// Package engine is the transport-free orchestrator for Start/Play/Eligibility.
// It depends only on ports and the registry — never on a DB, Redis, or gRPC.
package engine

import (
	"context"
	"encoding/json"
	"time"

	"github.com/muse/gamekit"
	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/ports"
	"github.com/muse/gamekit/types"
	"github.com/muse/gamekit/wallet"
)

// Config tunes engine behavior.
type Config struct {
	// SessionTTL is how long a session is valid after Start.
	SessionTTL time.Duration
}

// Engine orchestrates the three core operations. Construct with New.
type Engine struct {
	reg     *gamekit.Registry
	games   ports.GameStore
	prizes  ports.PrizeStore
	sess    ports.SessionStore
	hist    ports.HistoryStore
	rewards ports.RewardStore      // optional; nil → no reward records / caps / codes
	fulfill ports.FulfillmentStore // optional; nil → no outbox delivery tasks
	turns   ports.TurnGate         // optional; nil → Rules.UseTurns is a no-op
	board   ports.LeaderboardHook  // optional; nil → no leaderboard updates
	wallet  ports.WalletRouter     // optional; nil → points/lucky_item uncredited
	tx      ports.TxRunner
	events  ports.EventSink
	clock   ports.Clock
	rand    ports.RandSource
	ids     ports.IDGen
	cfg     Config
}

// Deps bundles the ports the engine needs.
type Deps struct {
	Registry    *gamekit.Registry
	Games       ports.GameStore
	Prizes      ports.PrizeStore
	Sessions    ports.SessionStore
	History     ports.HistoryStore
	Rewards     ports.RewardStore      // optional
	Fulfill     ports.FulfillmentStore // optional
	Turns       ports.TurnGate         // optional; enables Rules.UseTurns gating
	Leaderboard ports.LeaderboardHook  // optional; post-Play standings update
	Wallet      ports.WalletRouter     // optional; in-Play points/lucky_item credit + milestones
	Tx          ports.TxRunner
	Events      ports.EventSink
	Clock       ports.Clock
	Rand        ports.RandSource
	IDs         ports.IDGen
}

// New builds an engine. SessionTTL defaults to 10m if unset.
func New(d Deps, cfg Config) *Engine {
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 10 * time.Minute
	}
	return &Engine{
		reg: d.Registry, games: d.Games, prizes: d.Prizes, sess: d.Sessions,
		hist: d.History, rewards: d.Rewards, fulfill: d.Fulfill, turns: d.Turns,
		board: d.Leaderboard, wallet: d.Wallet, tx: d.Tx, events: d.Events,
		clock: d.Clock, rand: d.Rand, ids: d.IDs, cfg: cfg,
	}
}

func (e *Engine) deps() gamekit.HandlerDeps {
	return gamekit.HandlerDeps{Prizes: e.prizes, Rand: e.rand, Clock: e.clock}
}

// Start creates a session, runs the configured seed generator, and returns the
// session id + seed for the FE to render.
func (e *Engine) Start(ctx context.Context, scope types.Scope, gameID, playerID string) (*types.StartResult, error) {
	game, err := e.games.GetGame(ctx, scope, gameID)
	if err != nil {
		return nil, err
	}
	if err := e.checkActive(game); err != nil {
		return nil, err
	}

	now := e.clock.Now()
	session := &types.Session{
		ID:        e.ids.NewID("sess"),
		Scope:     scope,
		GameID:    gameID,
		PlayerID:  playerID,
		Secret:    e.ids.NewID("sk"),
		StartedAt: now,
		ExpiresAt: now.Add(e.cfg.SessionTTL),
	}

	seedGen, err := e.reg.Seed(game.SeedGenerator)
	if err != nil {
		return nil, err
	}
	seed, err := seedGen.Generate(ctx, e.deps(), game, session)
	if err != nil {
		return nil, err
	}
	session.SeedData = seed

	if err := e.sess.CreateSession(ctx, session); err != nil {
		return nil, err
	}
	return &types.StartResult{
		SessionID: session.ID,
		SeedData:  seed,
		ExpiresAt: session.ExpiresAt,
	}, nil
}

// Play validates the session + payload, runs the reward handler, then inside a
// single transaction: deducts stock atomically, consumes the session, and
// writes immutable history. Best-effort events fire after commit.
func (e *Engine) Play(ctx context.Context, scope types.Scope, gameID, sessionID, playerID string, payload types.Payload, traceID string) (*types.PlayResult, error) {
	game, err := e.games.GetGame(ctx, scope, gameID)
	if err != nil {
		return nil, err
	}
	if err := e.checkActive(game); err != nil {
		return nil, err
	}

	session, err := e.sess.GetSession(ctx, scope, sessionID)
	if err != nil {
		return nil, err
	}
	if session.GameID != gameID || session.PlayerID != playerID {
		return nil, gkerr.New(gkerr.ReasonSessionInvalid, "session does not match game/player")
	}
	if session.Consumed {
		return nil, gkerr.New(gkerr.ReasonSessionConsumed, "session already used")
	}
	if e.clock.Now().After(session.ExpiresAt) {
		return nil, gkerr.New(gkerr.ReasonSessionExpired, "session has expired").
			WithMeta("session_id", sessionID)
	}

	// Anti-cheat validator runs before the handler decides anything.
	validator, err := e.reg.Validator(game.Validator)
	if err != nil {
		return nil, err
	}
	if err := validator.Validate(ctx, game, session, payload); err != nil {
		return nil, err
	}

	// Eligibility: enforce per-user / per-day caps.
	if err := e.checkTurns(ctx, scope, game, playerID); err != nil {
		return nil, err
	}

	// The handler decides candidate rewards (server-authoritative outcome).
	handler, err := e.reg.Handler(game.RewardHandler)
	if err != nil {
		return nil, err
	}
	result, err := handler.Evaluate(ctx, e.deps(), game, session.SeedData, payload)
	if err != nil {
		return nil, err
	}

	playID := e.ids.NewID("play")

	// Everything that must be consistent happens in one transaction:
	// consume session → for each stocked reward: enforce per-player caps, deduct
	// stock atomically, assign a code, write the durable reward record → write history.
	awarded := make([]types.Reward, 0, len(result.Rewards))
	now := e.clock.Now()
	dayStart := startOfDay(now)
	txErr := e.tx.WithTx(ctx, func(ctx context.Context) error {
		// Turn-gated games spend one quest-granted turn, atomically with the rest
		// of the play. The pre-check in checkTurns is advisory; this is authoritative.
		if game.Rules.UseTurns && e.turns != nil {
			ok, _, err := e.turns.ConsumeTurn(ctx, scope.TenantID, playerID, engineTurnKey(scope, game))
			if err != nil {
				return err
			}
			if !ok {
				return gkerr.New(gkerr.ReasonOutOfTurns, "no remaining turns")
			}
		}
		if err := e.sess.ConsumeSession(ctx, scope, sessionID); err != nil {
			return err
		}
		// Per-prize award counters accumulated within this single play so caps
		// hold across multiple units of the same prize (gift-catcher).
		awardedThisPlay := map[string]int{}
		for _, r := range result.Rewards {
			// Wallet currencies (points/lucky_item) are credited to the ledger by
			// the WalletRouter after the loop — never stock-deducted or fulfilled.
			if wallet.IsWalletReward(r.Type) {
				awarded = append(awarded, r)
				continue
			}
			// PrizeID == "" means a non-stocked reward (e.g. "better luck").
			if r.PrizeID == "" {
				awarded = append(awarded, r)
				continue
			}

			// Load the prize for its caps + fulfillment policy.
			prize, err := e.prizes.GetPrize(ctx, scope, r.PrizeID)
			if err != nil {
				return err
			}

			// Per-user / per-day caps: drop the reward if the player is at the
			// limit (the wheel "landed" on it but the cap converts it to no-win).
			if e.rewards != nil && (prize.Constraints.MaxPerUser > 0 || prize.Constraints.MaxPerDay > 0) {
				total, today, err := e.rewards.CountAwards(ctx, scope, r.PrizeID, playerID, dayStart)
				if err != nil {
					return err
				}
				total += awardedThisPlay[r.PrizeID]
				today += awardedThisPlay[r.PrizeID]
				if prize.Constraints.MaxPerUser > 0 && total >= prize.Constraints.MaxPerUser {
					continue
				}
				if prize.Constraints.MaxPerDay > 0 && today >= prize.Constraints.MaxPerDay {
					continue
				}
			}

			ok, err := e.prizes.Deduct(ctx, scope, r.PrizeID)
			if err != nil {
				return err
			}
			if !ok {
				return gkerr.New(gkerr.ReasonPrizeOutOfStock, "prize stock exhausted").
					WithMeta("prize_id", r.PrizeID).WithMeta("game_id", gameID)
			}
			awardedThisPlay[r.PrizeID]++

			// Persist the durable reward record + assign a code, if a store is wired.
			if e.rewards != nil {
				rec, err := e.createRewardRecord(ctx, scope, game, playerID, playID, prize, now)
				if err != nil {
					return err
				}
				r.RewardID = rec.ID
				r.Code = rec.Code
			}
			awarded = append(awarded, r)
		}
		// Wallet routing (Phase 8): credit points/lucky_item to the ledger and
		// auto-grant milestones, atomically with the play. Milestone-granted
		// prizes are appended so they appear in the result + history.
		if e.wallet != nil {
			granted, wErr := e.wallet.Credit(ctx, scope, game, playerID, playID, awarded)
			if wErr != nil {
				return wErr
			}
			awarded = append(awarded, granted...)
		}
		rewardsJSON, _ := json.Marshal(awarded)
		metaJSON, _ := json.Marshal(result.Metadata)
		return e.hist.InsertHistory(ctx, &types.PlayHistory{
			ID:        playID,
			Scope:     scope,
			GameID:    gameID,
			PlayerID:  playerID,
			SessionID: sessionID,
			Payload:   payload,
			Rewards:   rewardsJSON,
			Metadata:  metaJSON,
			TraceID:   traceID,
			CreatedAt: now,
		})
	})
	if txErr != nil {
		return nil, txErr
	}

	// Leaderboard standings update (best-effort, post-commit): folds the play
	// into the campaign's active boards and may annotate metadata with the new
	// rank(s). A failure here never fails the play (the durable record exists).
	if e.board != nil {
		if err := e.board.OnPlay(ctx, scope, game, playerID, playID, awarded, result.Metadata); err != nil {
			if e.events != nil {
				e.events.Emit(ctx, ports.Event{
					Type: "leaderboard_update_failed", Scope: scope,
					Payload: map[string]any{"game_id": gameID, "player_id": playerID, "play_id": playID, "error": err.Error()},
				})
			}
		}
	}

	// Best-effort domain events after commit.
	if e.events != nil {
		e.events.Emit(ctx, ports.Event{
			Type: "play_completed", Scope: scope,
			Payload: map[string]any{"game_id": gameID, "player_id": playerID, "play_id": playID},
		})
		for _, r := range awarded {
			if r.PrizeID != "" {
				e.events.Emit(ctx, ports.Event{
					Type: "prize_won", Scope: scope,
					Payload: map[string]any{"game_id": gameID, "player_id": playerID, "prize_id": r.PrizeID},
				})
			}
		}
	}

	return &types.PlayResult{PlayID: playID, Rewards: awarded, Metadata: result.Metadata}, nil
}

// createRewardRecord builds and persists a reward record for one awarded unit,
// then enqueues a transactional-outbox delivery task when the prize's channel
// needs out-of-band delivery and redemption is automatic (instant). Reward
// status follows the redemption mode and channel:
//
//   - in-app channels (voucher_code/none) + instant -> fulfilled immediately
//     (the assigned code is the delivery; no task);
//   - async-delivery channels (sms/zns/.../external_workflow) -> stays won until
//     the dispatcher delivers it; an instant prize enqueues the task now, an
//     on_claim/manual prize enqueues later (at claim / staff approval).
//
// A voucher code is popped from the prize pool when the channel is voucher_code.
// All of this runs inside the Play transaction, so the reward, the stock
// deduction, and the outbox task commit atomically.
func (e *Engine) createRewardRecord(ctx context.Context, scope types.Scope, game *types.Game, playerID, playID string, prize *types.Prize, now time.Time) (*types.RewardRecord, error) {
	ful := prize.Fulfillment
	instant := ful.RedemptionMode == types.RedemptionInstant
	async := ful.NeedsAsyncDelivery()

	status := types.RewardWon
	var fulfilledAt *time.Time
	if instant && !async {
		// In-app delivery (voucher_code/none) is done the moment it is won.
		status = types.RewardFulfilled
		fulfilledAt = &now
	}

	code := ""
	if ful.ResolvedChannel() == types.ChannelVoucherCode {
		c, ok, err := e.rewards.PopCode(ctx, scope, prize.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			code = c
		}
	}

	rec := &types.RewardRecord{
		ID:          e.ids.NewID("rwd"),
		Scope:       scope,
		GameID:      game.ID,
		PlayerID:    playerID,
		PrizeID:     prize.ID,
		PlayID:      playID,
		Name:        prize.Name,
		Type:        prize.Type,
		Value:       prize.Value,
		Code:        code,
		Status:      status,
		CreatedAt:   now,
		FulfilledAt: fulfilledAt,
	}
	if err := e.rewards.InsertReward(ctx, rec); err != nil {
		return nil, err
	}

	// Outbox: an instant prize on an async-delivery channel is queued for the
	// dispatcher right now, in this same transaction. on_claim/manual prizes are
	// enqueued later by the hosting layer (claim / staff approval).
	if e.fulfill != nil && async && instant {
		task := &types.FulfillmentTask{
			ID:            e.ids.NewID("task"),
			Scope:         scope,
			RewardID:      rec.ID,
			PrizeID:       prize.ID,
			PlayerID:      playerID,
			GameID:        game.ID,
			CampaignID:    game.CampaignID,
			Channel:       ful.ResolvedChannel(),
			ChannelConfig: ful.ChannelConfig,
			Status:        types.TaskPending,
			MaxAttempts:   ful.Retry.MaxAttempts,
			NextAttemptAt: now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := e.fulfill.EnqueueTask(ctx, task); err != nil {
			return nil, err
		}
	}
	return rec, nil
}

// Eligibility reports whether the player can play and how many turns remain.
func (e *Engine) Eligibility(ctx context.Context, scope types.Scope, gameID, playerID string) (*types.Eligibility, error) {
	game, err := e.games.GetGame(ctx, scope, gameID)
	if err != nil {
		return nil, err
	}
	now := e.clock.Now()
	if game.Status != types.StatusActive {
		return &types.Eligibility{CanPlay: false, Reason: "ENDED"}, nil
	}
	if game.Rules.StartDate != nil && now.Before(*game.Rules.StartDate) {
		return &types.Eligibility{CanPlay: false, Reason: "NOT_STARTED", NextResetAt: game.Rules.StartDate}, nil
	}
	if game.Rules.EndDate != nil && now.After(*game.Rules.EndDate) {
		return &types.Eligibility{CanPlay: false, Reason: "ENDED"}, nil
	}

	remaining, reason, err := e.remainingPlays(ctx, scope, game, playerID, now)
	if err != nil {
		return nil, err
	}
	el := &types.Eligibility{CanPlay: remaining > 0, RemainingPlays: remaining}
	if remaining <= 0 {
		el.Reason = reason
		reset := startOfNextDay(now)
		el.NextResetAt = &reset
	}
	return el, nil
}

// History returns the caller's play history (paginated).
func (e *Engine) History(ctx context.Context, scope types.Scope, gameID, playerID string, limit int, cursor string) ([]types.PlayHistory, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return e.hist.ListHistory(ctx, scope, gameID, playerID, limit, cursor)
}

func (e *Engine) checkActive(game *types.Game) error {
	if game.Status != types.StatusActive {
		return gkerr.Newf(gkerr.ReasonGameNotActive, "game is %s", game.Status).
			WithMeta("game_id", game.ID)
	}
	now := e.clock.Now()
	if game.Rules.StartDate != nil && now.Before(*game.Rules.StartDate) {
		return gkerr.New(gkerr.ReasonGameNotActive, "game has not started")
	}
	if game.Rules.EndDate != nil && now.After(*game.Rules.EndDate) {
		return gkerr.New(gkerr.ReasonGameNotActive, "game has ended")
	}
	return nil
}

func (e *Engine) checkTurns(ctx context.Context, scope types.Scope, game *types.Game, playerID string) error {
	remaining, reason, err := e.remainingPlays(ctx, scope, game, playerID, e.clock.Now())
	if err != nil {
		return err
	}
	if remaining <= 0 {
		return gkerr.New(gkerr.ReasonOutOfTurns, "no remaining plays").WithMeta("reason", reason)
	}
	return nil
}

// remainingPlays computes the min of the per-user-total and per-day budgets and,
// when the game opts into turn-gating (Rules.UseTurns) and a TurnGate is wired,
// the player's quest-granted turn balance. A non-positive rule means "unlimited"
// for that dimension.
func (e *Engine) remainingPlays(ctx context.Context, scope types.Scope, game *types.Game, playerID string, now time.Time) (int, string, error) {
	remaining := 1<<31 - 1
	reason := ""
	rMax := game.Rules.MaxPlaysPerUser
	rDay := game.Rules.MaxPlaysPerDay
	if rMax > 0 || rDay > 0 {
		total, today, err := e.hist.CountPlays(ctx, scope, game.ID, playerID, startOfDay(now))
		if err != nil {
			return 0, "", err
		}
		if rMax > 0 {
			if v := rMax - total; v < remaining {
				remaining, reason = v, "OUT_OF_TURNS"
			}
		}
		if rDay > 0 {
			if v := rDay - today; v < remaining {
				remaining, reason = v, "OUT_OF_TURNS"
			}
		}
	}
	if game.Rules.UseTurns && e.turns != nil {
		bal, err := e.turns.GetTurnBalance(ctx, scope.TenantID, playerID, engineTurnKey(scope, game))
		if err != nil {
			return 0, "", err
		}
		if int(bal) < remaining {
			remaining, reason = int(bal), "OUT_OF_TURNS"
		}
	}
	if remaining < 0 {
		remaining = 0
	}
	return remaining, reason, nil
}

// engineTurnKey is the turn-balance scope key for a turn-gated game: the
// campaign (the default wallet scope), falling back to merchant then tenant so a
// balance always has a deterministic home. Mirrors the hosting layer's
// turnScopeKey so quest grants and Play consumption hit the same balance.
func engineTurnKey(scope types.Scope, game *types.Game) string {
	if game.CampaignID != "" {
		return game.CampaignID
	}
	if scope.MerchantID != "" {
		return scope.MerchantID
	}
	return scope.TenantID
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func startOfNextDay(t time.Time) time.Time {
	return startOfDay(t).Add(24 * time.Hour)
}
