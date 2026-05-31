// Package leaderboard is the hosting-layer leaderboard orchestrator: it folds
// each Play into the campaign's active boards (the engine's LeaderboardHook),
// serves real-time rankings (Redis when wired, else the durable DB), and runs
// the admin operations — finalize (lock + snapshot + batch tier award), reset,
// disqualify, and score adjust. The pure metric/window math lives in
// gamekit/leaderboard; storage + the sorted-set mirror behind the ports.
package leaderboard

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/muse/gamekit/gkerr"
	lbkit "github.com/muse/gamekit/leaderboard"
	"github.com/muse/gamekit/ports"
	"github.com/muse/gamekit/types"
)

// Store is the durable persistence the service needs (satisfied by *sqlstore.DB):
// leaderboard config + entries, plus the reward/prize/fulfillment ports finalize
// uses to mint tier awards, and a TxRunner for atomic finalize.
type Store interface {
	ports.LeaderboardStore
	ports.PrizeStore
	ports.RewardStore
	ports.FulfillmentStore
	ports.TxRunner
}

// Service orchestrates leaderboards. board (Redis) and locker are optional.
type Service struct {
	store  Store
	board  ports.RankBoard // optional real-time mirror; nil → DB-only
	locker ports.Locker    // optional finalize mutex; nil → no cross-process lock
	ids    ports.IDGen
	clock  ports.Clock
	log    *slog.Logger
}

// New builds the service. board/locker may be nil.
func New(store Store, board ports.RankBoard, locker ports.Locker, ids ports.IDGen, clock ports.Clock, log *slog.Logger) *Service {
	return &Service{store: store, board: board, locker: locker, ids: ids, clock: clock, log: log}
}

func boardKey(lbID, windowKey string) string { return lbID + ":" + windowKey }

// OnPlay implements ports.LeaderboardHook: fold the play into each active board
// of the game's campaign and annotate metadata with the player's new rank(s).
func (s *Service) OnPlay(ctx context.Context, scope types.Scope, game *types.Game, playerID, playID string, rewards []types.Reward, metadata map[string]any) error {
	if game.CampaignID == "" {
		return nil
	}
	boards, err := s.store.ActiveLeaderboardsForCampaign(ctx, scope.TenantID, scope.MerchantID, game.CampaignID)
	if err != nil || len(boards) == 0 {
		return err
	}
	now := s.clock.Now()
	var rankings []map[string]any
	for i := range boards {
		lb := &boards[i]
		if !lbkit.IsOpen(lb.Window, now) {
			continue
		}
		wk := lbkit.WindowKey(lb.Window, now)
		value, isMax := lbkit.Contribution(lb.Metric, rewards, metadata)
		entry, uErr := s.store.UpsertEntry(ctx, &types.LeaderboardEntry{
			TenantID: scope.TenantID, LeaderboardID: lb.ID, WindowKey: wk, PlayerID: playerID,
		}, value, isMax)
		if uErr != nil {
			s.log.Warn("leaderboard upsert failed", "lb", lb.ID, "err", uErr)
			continue
		}
		// Anti-cheat: a single play whose contribution exceeds the ceiling flags
		// the entry (excluded from awards pending review).
		flagged := false
		if lb.AntiCheat.FlagOutliers && lb.AntiCheat.ScoreCeiling > 0 && value > lb.AntiCheat.ScoreCeiling {
			if fe, fErr := s.store.SetEntryState(ctx, scope.TenantID, lb.ID, wk, playerID, types.EntryFlagged); fErr == nil {
				entry = fe
				flagged = true
			}
		}
		// Keep the Redis mirror in sync: active entries are ranked, flagged ones removed.
		if s.board != nil {
			if flagged {
				_ = s.board.Remove(ctx, boardKey(lb.ID, wk), playerID)
			} else {
				_ = s.board.Update(ctx, boardKey(lb.ID, wk), playerID, entry.Score)
			}
		}
		row := map[string]any{"leaderboard_id": lb.ID, "score": entry.Score, "flagged": flagged}
		if !flagged {
			if pr, rErr := s.store.PlayerRank(ctx, scope.TenantID, lb.ID, wk, playerID); rErr == nil {
				row["rank"] = pr.Rank
			}
		}
		rankings = append(rankings, row)
	}
	if len(rankings) > 0 && metadata != nil {
		metadata["rankings"] = rankings
	}
	return nil
}

// --- config CRUD ---

func (s *Service) Create(ctx context.Context, lb *types.Leaderboard) (*types.Leaderboard, error) {
	return s.store.CreateLeaderboard(ctx, lb)
}
func (s *Service) Get(ctx context.Context, tenantID, merchantID, lbID string) (*types.Leaderboard, error) {
	return s.store.GetLeaderboard(ctx, tenantID, merchantID, lbID)
}
func (s *Service) Update(ctx context.Context, lb *types.Leaderboard) (*types.Leaderboard, error) {
	return s.store.UpdateLeaderboard(ctx, lb)
}
func (s *Service) List(ctx context.Context, tenantID, merchantID, campaignID string, limit int, cursor string) ([]types.Leaderboard, string, error) {
	return s.store.ListLeaderboards(ctx, tenantID, merchantID, campaignID, limit, cursor)
}

// currentWindow resolves a board + its current window key (scoped read).
func (s *Service) currentWindow(ctx context.Context, scope types.Scope, lbID string) (*types.Leaderboard, string, error) {
	lb, err := s.store.GetLeaderboard(ctx, scope.TenantID, scope.MerchantID, lbID)
	if err != nil {
		return nil, "", err
	}
	return lb, lbkit.WindowKey(lb.Window, s.clock.Now()), nil
}

// --- real-time reads (prefer Redis, fall back to DB) ---

func (s *Service) Rankings(ctx context.Context, scope types.Scope, lbID string, limit, offset int) ([]types.RankedEntry, int64, string, error) {
	lb, wk, err := s.currentWindow(ctx, scope, lbID)
	if err != nil {
		return nil, 0, "", err
	}
	if s.board != nil {
		members, total, bErr := s.board.Top(ctx, boardKey(lb.ID, wk), offset, limit)
		if bErr == nil {
			return fromMembers(members), total, wk, nil
		}
		s.log.Warn("rankboard top failed; falling back to db", "err", bErr)
	}
	entries, total, dErr := s.store.Rankings(ctx, scope.TenantID, lb.ID, wk, limit, offset)
	return entries, total, wk, dErr
}

func (s *Service) AroundMe(ctx context.Context, scope types.Scope, lbID, playerID string, radius int) ([]types.RankedEntry, error) {
	lb, wk, err := s.currentWindow(ctx, scope, lbID)
	if err != nil {
		return nil, err
	}
	if radius <= 0 {
		radius = 3
	}
	if s.board != nil {
		members, bErr := s.board.Around(ctx, boardKey(lb.ID, wk), playerID, radius)
		if bErr == nil {
			return fromMembers(members), nil
		}
		s.log.Warn("rankboard around failed; falling back to db", "err", bErr)
	}
	pr, err := s.store.PlayerRank(ctx, scope.TenantID, lb.ID, wk, playerID)
	if err != nil {
		return nil, err
	}
	offset := max(int(pr.Rank)-1-radius, 0)
	entries, _, err := s.store.Rankings(ctx, scope.TenantID, lb.ID, wk, 2*radius+1, offset)
	return entries, err
}

// MyRank returns the caller's standing plus how far to the next (better) prize tier.
func (s *Service) MyRank(ctx context.Context, scope types.Scope, lbID, playerID string) (*types.RankedEntry, int, int64, error) {
	lb, wk, err := s.currentWindow(ctx, scope, lbID)
	if err != nil {
		return nil, 0, 0, err
	}
	var entry *types.RankedEntry
	if s.board != nil {
		if rank, score, ok, bErr := s.board.Rank(ctx, boardKey(lb.ID, wk), playerID); bErr == nil && ok {
			entry = &types.RankedEntry{LeaderboardEntry: types.LeaderboardEntry{PlayerID: playerID, Score: score, State: types.EntryActive}, Rank: rank}
		}
	}
	if entry == nil {
		entry, err = s.store.PlayerRank(ctx, scope.TenantID, lb.ID, wk, playerID)
		if err != nil {
			return nil, 0, 0, err
		}
	}
	nextFrom, toGo := nextTier(lb.PrizeTiers, entry.Rank)
	return entry, nextFrom, toGo, nil
}

// nextTier finds the best prize tier strictly above the player's rank and how
// many ranks they must climb to enter it. Returns (0,0) when already in/above
// the top tier or there are no tiers.
func nextTier(tiers []types.PrizeTier, rank int64) (fromRank int, ranksToGo int64) {
	sorted := append([]types.PrizeTier(nil), tiers...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ToRank < sorted[j].ToRank })
	for _, t := range sorted {
		if int64(t.ToRank) < rank { // a better-paying tier the player hasn't reached
			fromRank, ranksToGo = t.FromRank, rank-int64(t.ToRank)
		}
	}
	return fromRank, ranksToGo
}

func fromMembers(ms []ports.RankMember) []types.RankedEntry {
	out := make([]types.RankedEntry, 0, len(ms))
	for _, m := range ms {
		out = append(out, types.RankedEntry{
			LeaderboardEntry: types.LeaderboardEntry{PlayerID: m.Member, Score: m.Score, State: types.EntryActive},
			Rank:             m.Rank,
		})
	}
	return out
}

// --- admin actions ---

func (s *Service) Disqualify(ctx context.Context, scope types.Scope, lbID, playerID string) (*types.LeaderboardEntry, error) {
	lb, wk, err := s.currentWindow(ctx, scope, lbID)
	if err != nil {
		return nil, err
	}
	e, err := s.store.SetEntryState(ctx, scope.TenantID, lb.ID, wk, playerID, types.EntryDisqualified)
	if err != nil {
		return nil, err
	}
	if s.board != nil {
		_ = s.board.Remove(ctx, boardKey(lb.ID, wk), playerID)
	}
	return e, nil
}

func (s *Service) Adjust(ctx context.Context, scope types.Scope, lbID, playerID string, delta int64) (*types.LeaderboardEntry, error) {
	lb, wk, err := s.currentWindow(ctx, scope, lbID)
	if err != nil {
		return nil, err
	}
	e, err := s.store.AdjustScore(ctx, scope.TenantID, lb.ID, wk, playerID, delta)
	if err != nil {
		return nil, err
	}
	if s.board != nil && e.State == types.EntryActive {
		_ = s.board.Update(ctx, boardKey(lb.ID, wk), playerID, e.Score)
	}
	return e, nil
}

// Reset clears the current window's entries (durable + Redis mirror).
func (s *Service) Reset(ctx context.Context, scope types.Scope, lbID string) (int64, error) {
	lb, wk, err := s.currentWindow(ctx, scope, lbID)
	if err != nil {
		return 0, err
	}
	n, err := s.store.DeleteEntries(ctx, scope.TenantID, lb.ID, wk)
	if err != nil {
		return 0, err
	}
	if s.board != nil {
		_ = s.board.Reset(ctx, boardKey(lb.ID, wk))
	}
	return n, nil
}

// TierAward records one finalize award for the response.
type TierAward struct {
	Rank     int    `json:"rank"`
	PlayerID string `json:"player_id"`
	PrizeID  string `json:"prize_id"`
	RewardID string `json:"reward_id"`
}

// Finalize locks the board, snapshots the current window's active ranking, and
// batch-awards the configured prize tiers (minting reward records + fulfillment
// tasks atomically), then marks the board finalized.
func (s *Service) Finalize(ctx context.Context, scope types.Scope, lbID string) ([]TierAward, error) {
	if s.locker != nil {
		release, ok, err := s.locker.Acquire(ctx, "finalize:"+lbID, 30*time.Second)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, gkerr.New(gkerr.ReasonTaskBadState, "finalize already in progress")
		}
		defer func() { _ = release(ctx) }()
	}

	lb, err := s.store.GetLeaderboard(ctx, scope.TenantID, scope.MerchantID, lbID)
	if err != nil {
		return nil, err
	}
	if lb.Status == types.LeaderboardFinalized {
		return nil, gkerr.New(gkerr.ReasonAlreadyExists, "leaderboard already finalized")
	}
	wk := lbkit.WindowKey(lb.Window, s.clock.Now())
	ranked, err := s.store.SnapshotRanking(ctx, scope.TenantID, lb.ID, wk)
	if err != nil {
		return nil, err
	}
	byRank := make(map[int64]types.RankedEntry, len(ranked))
	for _, e := range ranked {
		byRank[e.Rank] = e
	}

	var awards []TierAward
	now := s.clock.Now()
	txErr := s.store.WithTx(ctx, func(ctx context.Context) error {
		for _, tier := range lb.PrizeTiers {
			prize, pErr := s.store.GetPrize(ctx, scope, tier.PrizeID)
			if pErr != nil {
				return pErr
			}
			for rank := tier.FromRank; rank <= tier.ToRank; rank++ {
				entry, ok := byRank[int64(rank)]
				if !ok {
					continue // fewer players than the tier spans
				}
				// Respect stock when the prize tracks it.
				if prize.Total > 0 {
					okStock, dErr := s.store.Deduct(ctx, scope, prize.ID)
					if dErr != nil {
						return dErr
					}
					if !okStock {
						continue
					}
				}
				rewardID, aErr := s.mintAward(ctx, scope, lb, prize, entry.PlayerID, now)
				if aErr != nil {
					return aErr
				}
				awards = append(awards, TierAward{Rank: rank, PlayerID: entry.PlayerID, PrizeID: prize.ID, RewardID: rewardID})
			}
		}
		lb.Status = types.LeaderboardFinalized
		_, uErr := s.store.UpdateLeaderboard(ctx, lb)
		return uErr
	})
	if txErr != nil {
		return nil, txErr
	}
	return awards, nil
}

// mintAward writes a durable reward record for a tier win and enqueues a
// fulfillment task when the prize needs out-of-band delivery — mirroring the
// engine's in-Play award path.
func (s *Service) mintAward(ctx context.Context, scope types.Scope, lb *types.Leaderboard, prize *types.Prize, playerID string, now time.Time) (string, error) {
	ful := prize.Fulfillment
	instant := ful.RedemptionMode == types.RedemptionInstant
	async := ful.NeedsAsyncDelivery()

	status := types.RewardWon
	var fulfilledAt *time.Time
	if instant && !async {
		status, fulfilledAt = types.RewardFulfilled, &now
	}
	code := ""
	if ful.ResolvedChannel() == types.ChannelVoucherCode {
		if c, ok, err := s.store.PopCode(ctx, scope, prize.ID); err != nil {
			return "", err
		} else if ok {
			code = c
		}
	}
	rec := &types.RewardRecord{
		ID:          s.ids.NewID("rwd"),
		Scope:       scope,
		PlayerID:    playerID,
		PrizeID:     prize.ID,
		PlayID:      "lb:" + lb.ID,
		Name:        prize.Name,
		Type:        prize.Type,
		Value:       prize.Value,
		Code:        code,
		Status:      status,
		CreatedAt:   now,
		FulfilledAt: fulfilledAt,
	}
	if err := s.store.InsertReward(ctx, rec); err != nil {
		return "", err
	}
	if async && instant {
		if err := s.store.EnqueueTask(ctx, &types.FulfillmentTask{
			ID:            s.ids.NewID("task"),
			Scope:         scope,
			RewardID:      rec.ID,
			PrizeID:       prize.ID,
			PlayerID:      playerID,
			CampaignID:    lb.CampaignID,
			Channel:       ful.ResolvedChannel(),
			ChannelConfig: ful.ChannelConfig,
			Status:        types.TaskPending,
			MaxAttempts:   ful.Retry.MaxAttempts,
			NextAttemptAt: now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}); err != nil {
			return "", err
		}
	}
	return rec.ID, nil
}
