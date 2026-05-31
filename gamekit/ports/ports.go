// Package ports declares the interfaces the engine depends on. Consumers
// implement them (their own DB/cache/clock) or import github.com/muse/adapters
// for batteries-included pg/mysql + Redis. Every method takes a types.Scope so
// implementations enforce tenant/merchant isolation on every query.
package ports

import (
	"context"
	"time"

	"github.com/muse/gamekit/types"
)

// Clock abstracts time so the engine is deterministic under test.
type Clock interface {
	Now() time.Time
}

// RandSource abstracts randomness for weighted draws (deterministic in tests).
type RandSource interface {
	// Float64 returns a pseudo-random number in [0.0, 1.0).
	Float64() float64
	// Intn returns a pseudo-random int in [0, n).
	Intn(n int) int
}

// IDGen mints prefixed opaque IDs (sess_..., play_...).
type IDGen interface {
	NewID(prefix string) string
}

// GameStore loads game configuration.
type GameStore interface {
	GetGame(ctx context.Context, scope types.Scope, gameID string) (*types.Game, error)
}

// SessionStore persists single-use play sessions (Redis in Mode B, with a
// durable audit row). TTL is honored by the implementation.
type SessionStore interface {
	CreateSession(ctx context.Context, s *types.Session) error
	GetSession(ctx context.Context, scope types.Scope, sessionID string) (*types.Session, error)
	// ConsumeSession atomically marks a session consumed; returns ErrAlreadyConsumed
	// (as a gkerr) if it was already consumed.
	ConsumeSession(ctx context.Context, scope types.Scope, sessionID string) error
}

// PrizeStore loads prizes and performs the authoritative atomic stock deduction.
type PrizeStore interface {
	GetPrize(ctx context.Context, scope types.Scope, prizeID string) (*types.Prize, error)
	ListPrizes(ctx context.Context, scope types.Scope, gameID string) ([]types.Prize, error)
	// Deduct atomically decrements remaining stock by 1 iff remaining > 0.
	// Returns (true, nil) on success, (false, nil) when out of stock.
	// MUST be safe under concurrency (conditional UPDATE ... WHERE remaining > 0).
	Deduct(ctx context.Context, scope types.Scope, prizeID string) (ok bool, err error)
}

// RewardStore persists durable reward records and supports the in-Play reward
// concerns: per-player award caps and voucher-code assignment. The reward
// lifecycle (claim/fulfill/revoke) lives in the hosting layer, not here — the
// engine only creates records and reads caps inside the Play transaction.
//
// Optional: if the engine is constructed without a RewardStore, it skips reward
// records, constraint checks, and code assignment (legacy/embed behavior).
type RewardStore interface {
	// InsertReward writes a reward record (inside the Play txn).
	InsertReward(ctx context.Context, r *types.RewardRecord) error
	// CountAwards returns how many non-revoked rewards of a prize a player has
	// (all time) and since `since` (start of today), for the per-user/day caps.
	CountAwards(ctx context.Context, scope types.Scope, prizeID, playerID string, since time.Time) (total, today int, err error)
	// PopCode atomically claims one available code from the prize's pool.
	// Returns (code, true, nil) on success, ("", false, nil) when the pool is empty.
	PopCode(ctx context.Context, scope types.Scope, prizeID string) (code string, ok bool, err error)
}

// --- Tenancy & identity (Phase 4) ---
//
// These let an embedder plug their own existing tenant/user/identity system
// while reusing the game logic. They are NOT consumed by the engine's Play path
// (gameplay is keyed by an opaque player_id); they back the hosting layer's
// auth + membership resolution. An embedder that brings its own auth can ignore
// them entirely.

// TenantStore persists platform tenants (top of the isolation hierarchy).
type TenantStore interface {
	CreateTenant(ctx context.Context, t *types.Tenant) (*types.Tenant, error)
	GetTenant(ctx context.Context, tenantID string) (*types.Tenant, error)
	UpdateTenant(ctx context.Context, t *types.Tenant) (*types.Tenant, error)
	ListTenants(ctx context.Context, limit int, cursor string) ([]types.Tenant, string, error)
}

// MerchantStore persists merchants under a tenant (tenant-scoped).
type MerchantStore interface {
	CreateMerchant(ctx context.Context, m *types.Merchant) (*types.Merchant, error)
	GetMerchant(ctx context.Context, tenantID, merchantID string) (*types.Merchant, error)
	UpdateMerchant(ctx context.Context, m *types.Merchant) (*types.Merchant, error)
	ListMerchants(ctx context.Context, tenantID string, limit int, cursor string) ([]types.Merchant, string, error)
}

// QuestStore persists quests under a campaign (campaign-scoped) and the
// immutable per-player completion records that gate re-completion. The engine
// never calls this; it backs the hosting layer's quest CRUD + completion flow.
type QuestStore interface {
	CreateQuest(ctx context.Context, q *types.Quest) (*types.Quest, error)
	GetQuest(ctx context.Context, tenantID, merchantID, questID string) (*types.Quest, error)
	UpdateQuest(ctx context.Context, q *types.Quest) (*types.Quest, error)
	DeleteQuest(ctx context.Context, tenantID, merchantID, questID string) error
	// ListQuests lists a campaign's quests (campaignID optional → all under the
	// merchant), newest-last with a created_at cursor.
	ListQuests(ctx context.Context, tenantID, merchantID, campaignID string, limit int, cursor string) ([]types.Quest, string, error)

	// RecordCompletion writes one completion record (inside the grant txn).
	RecordCompletion(ctx context.Context, c *types.QuestCompletion) error
	// CountCompletions returns a player's completions of a quest (all time) and
	// since `since` (start of today), for the once / once-per-day gate.
	CountCompletions(ctx context.Context, tenantID, questID, playerID string, since time.Time) (total, today int, lastAt *time.Time, err error)
}

// WalletRouter is the engine's OPTIONAL in-Play wallet routing (Phase 8). Inside
// the Play transaction the engine hands it the awarded rewards; it credits the
// wallet ledger for `points` / `lucky_item` rewards and processes auto-grant
// milestones, returning any milestone-granted rewards to append to the result.
// A nil router (pure embed) leaves wallet rewards uncredited.
type WalletRouter interface {
	Credit(ctx context.Context, scope types.Scope, game *types.Game, playerID, playID string, rewards []types.Reward) (granted []types.Reward, err error)
}

// WalletStore persists the per-player wallet: an append-only ledger, the derived
// per-currency balances, and the once-only milestone grant guard. Credits/spends
// are atomic; the engine never calls this directly (it goes through WalletRouter).
type WalletStore interface {
	// Credit appends a (positive) ledger entry and returns the new balance.
	Credit(ctx context.Context, tenantID, scopeKey, playerID, currency string, amount int64, reason, refID string) (newBalance int64, err error)
	// Spend conditionally debits `amount` iff balance >= amount (ok=false when
	// insufficient), appending a negative ledger entry. For spend_exchange redeem.
	Spend(ctx context.Context, tenantID, scopeKey, playerID, currency string, amount int64, reason, refID string) (ok bool, newBalance int64, err error)
	// Balance returns one currency balance (0 if none).
	Balance(ctx context.Context, tenantID, scopeKey, playerID, currency string) (int64, error)
	// Balances returns all currency balances for a player in a scope.
	Balances(ctx context.Context, tenantID, scopeKey, playerID string) (map[string]int64, error)
	// Ledger lists movements newest-first (paginated).
	Ledger(ctx context.Context, tenantID, scopeKey, playerID string, limit int, cursor string) ([]types.LedgerEntry, string, error)
	// GrantMilestoneOnce inserts a grant record iff absent; granted=true when this
	// call won the race (idempotent auto-grant / manual claim).
	GrantMilestoneOnce(ctx context.Context, g *types.MilestoneGrant) (granted bool, err error)
	// GrantedMilestones returns the set of milestone ids already granted to a player.
	GrantedMilestones(ctx context.Context, tenantID, scopeKey, playerID string) (map[string]bool, error)
}

// LeaderboardHook is the engine's OPTIONAL post-Play standings update (Phase 7).
// After a play commits, the engine calls OnPlay best-effort so the host can fold
// the play into every active leaderboard of the game's campaign. The host may
// write back into metadata (e.g. metadata["rankings"]) so the Play response can
// surface the player's new rank. A nil hook (pure embed) skips leaderboards.
type LeaderboardHook interface {
	OnPlay(ctx context.Context, scope types.Scope, game *types.Game, playerID, playID string, rewards []types.Reward, metadata map[string]any) error
}

// LeaderboardStore persists leaderboard config and durable per-player entries
// (the source of truth; the Redis RankBoard is a derived real-time mirror).
// Entries are keyed by (leaderboard_id, window_key, player_id). The engine never
// calls this; it backs the hosting layer's leaderboard service.
type LeaderboardStore interface {
	CreateLeaderboard(ctx context.Context, lb *types.Leaderboard) (*types.Leaderboard, error)
	GetLeaderboard(ctx context.Context, tenantID, merchantID, lbID string) (*types.Leaderboard, error)
	UpdateLeaderboard(ctx context.Context, lb *types.Leaderboard) (*types.Leaderboard, error)
	ListLeaderboards(ctx context.Context, tenantID, merchantID, campaignID string, limit int, cursor string) ([]types.Leaderboard, string, error)
	// ActiveLeaderboardsForCampaign returns the campaign's active boards (the hook
	// fans a play out to each).
	ActiveLeaderboardsForCampaign(ctx context.Context, tenantID, merchantID, campaignID string) ([]types.Leaderboard, error)

	// UpsertEntry folds one play into a player's entry: plays += 1 and, when max,
	// score = GREATEST(score, contribution), else score += contribution. Atomic
	// under concurrency. Returns the updated entry.
	UpsertEntry(ctx context.Context, e *types.LeaderboardEntry, contribution int64, max bool) (*types.LeaderboardEntry, error)
	// SetEntryState flags/disqualifies/reactivates an entry.
	SetEntryState(ctx context.Context, tenantID, lbID, windowKey, playerID, state string) (*types.LeaderboardEntry, error)
	// AdjustScore applies a signed delta (admin correction).
	AdjustScore(ctx context.Context, tenantID, lbID, windowKey, playerID string, delta int64) (*types.LeaderboardEntry, error)
	// Rankings returns a page of ranked entries (rank desc by score), excluding
	// disqualified, plus the total rankable count.
	Rankings(ctx context.Context, tenantID, lbID, windowKey string, limit, offset int) ([]types.RankedEntry, int64, error)
	// PlayerRank returns the caller's ranked entry (NOT_FOUND if none/disqualified).
	PlayerRank(ctx context.Context, tenantID, lbID, windowKey, playerID string) (*types.RankedEntry, error)
	// SnapshotRanking returns ALL award-eligible (state=active) entries ranked,
	// for finalize (no pagination clamp).
	SnapshotRanking(ctx context.Context, tenantID, lbID, windowKey string) ([]types.RankedEntry, error)
	// DeleteEntries clears a window's entries (reset). Returns rows removed.
	DeleteEntries(ctx context.Context, tenantID, lbID, windowKey string) (int64, error)
}

// RankBoard is the OPTIONAL Redis sorted-set mirror for real-time rank reads
// (around-me / my-rank / top-N). The hosting layer keeps it in sync on each play
// and prefers it for reads when present; without it, reads fall back to the
// durable LeaderboardStore. Scores are descending (higher = rank 1).
type RankBoard interface {
	Update(ctx context.Context, key, member string, score int64) error
	Remove(ctx context.Context, key, member string) error
	Reset(ctx context.Context, key string) error
	// Rank returns the member's 1-based rank (ok=false if absent).
	Rank(ctx context.Context, key, member string) (rank int64, score int64, ok bool, err error)
	// Top returns members ranked [offset, offset+limit), highest first.
	Top(ctx context.Context, key string, offset, limit int) ([]RankMember, int64, error)
	// Around returns members within radius of the member (for around-me).
	Around(ctx context.Context, key, member string, radius int) ([]RankMember, error)
}

// RankMember is one Redis sorted-set member with its rank+score.
type RankMember struct {
	Member string
	Score  int64
	Rank   int64
}

// Locker is the OPTIONAL distributed lock used to serialize finalize. Acquire
// returns ok=false when the lock is held; release frees it. A nil Locker means
// finalize runs without cross-process mutual exclusion (single-instance dev).
type Locker interface {
	Acquire(ctx context.Context, key string, ttl time.Duration) (release func(context.Context) error, ok bool, err error)
}

// TurnGate is the engine's OPTIONAL view of quest-granted play turns. When a
// game opts into turn-gating (Rules.UseTurns) and the engine is built with a
// TurnGate, eligibility reads the balance and Play consumes one turn inside the
// play transaction. PlayerStore satisfies this interface, so the hosted Core
// wires the same store in; a pure embed leaves it nil and turn-gating is a no-op.
type TurnGate interface {
	GetTurnBalance(ctx context.Context, tenantID, playerID, scopeKey string) (int64, error)
	ConsumeTurn(ctx context.Context, tenantID, playerID, scopeKey string) (ok bool, newBalance int64, err error)
}

// CampaignStore persists campaigns under a merchant (campaign-scoped: every
// method takes tenant+merchant for isolation). GetCampaignByID resolves a
// campaign from its globally-unique id alone — used by the public widget config
// endpoint, where the caller knows only the campaign id. The engine never calls
// this; it backs the hosting layer's admin CRUD + widget rendering.
type CampaignStore interface {
	CreateCampaign(ctx context.Context, c *types.Campaign) (*types.Campaign, error)
	GetCampaign(ctx context.Context, tenantID, merchantID, campaignID string) (*types.Campaign, error)
	// GetCampaignByID resolves a campaign by its id alone (NOT_FOUND if missing).
	GetCampaignByID(ctx context.Context, campaignID string) (*types.Campaign, error)
	UpdateCampaign(ctx context.Context, c *types.Campaign) (*types.Campaign, error)
	ListCampaigns(ctx context.Context, tenantID, merchantID string, limit int, cursor string) ([]types.Campaign, string, error)
}

// IdentityStore persists the global person + their verified contacts. Each
// (contact_type, value) is globally unique; FindByContact resolves it. The
// engine never calls this — identity is platform infra used by the hosting
// auth layer. Contact writes MUST enforce the global-uniqueness invariant
// (return a CONTACT_CONFLICT gkerr when a contact maps to a different identity).
type IdentityStore interface {
	CreateIdentity(ctx context.Context, idn *types.Identity) (*types.Identity, error)
	GetIdentity(ctx context.Context, identityID string) (*types.Identity, error)
	// FindByContact returns the identity owning a normalized contact, or a
	// NOT_FOUND gkerr if none.
	FindByContact(ctx context.Context, t types.ContactType, value string) (*types.Identity, error)
	// AddContact links a verified contact to an identity. Returns CONTACT_CONFLICT
	// if the contact already belongs to a different identity (idempotent no-op if
	// it already belongs to this one).
	AddContact(ctx context.Context, identityID string, c types.Contact) error
}

// PlayerStore persists the tenant-scoped membership (UNIQUE(tenant_id,
// identity_id)) plus per-scope turn balances.
type PlayerStore interface {
	// UpsertPlayer returns the existing player for (tenant, identity) or creates
	// one. created reports whether a new row was inserted.
	UpsertPlayer(ctx context.Context, p *types.Player) (player *types.Player, created bool, err error)
	GetPlayer(ctx context.Context, tenantID, playerID string) (*types.Player, error)
	GetPlayerByIdentity(ctx context.Context, tenantID, identityID string) (*types.Player, error)
	UpdatePlayerProfile(ctx context.Context, tenantID, playerID string, profile map[string]any) (*types.Player, error)

	// GetTurnBalance returns the player's balance for a scope key (0 if none).
	GetTurnBalance(ctx context.Context, tenantID, playerID, scopeKey string) (int64, error)
	// GrantTurns atomically adds delta (may be negative) and returns the new
	// balance. Implementations MUST be atomic (upsert + conditional decrement)
	// and never let the balance go below zero on a consume.
	GrantTurns(ctx context.Context, tenantID, playerID, scopeKey string, delta int64) (int64, error)
	// ConsumeTurn atomically decrements by 1 iff balance > 0. ok=false means the
	// player was out of turns for that scope.
	ConsumeTurn(ctx context.Context, tenantID, playerID, scopeKey string) (ok bool, newBalance int64, err error)
}

// FulfillmentStore persists the transactional-outbox delivery tasks. The engine
// enqueues a task inside the Play transaction when a won unit needs out-of-band
// delivery (PrizeFulfillment.NeedsAsyncDelivery), so the task is committed
// atomically with the reward + stock deduction. The dispatcher (a hosting-layer
// worker, not the engine) drains and delivers them — that lifecycle lives in the
// host, not here.
//
// Optional: an engine built without a FulfillmentStore simply skips enqueueing
// (in-app/voucher_code-only behavior, e.g. the embed example).
type FulfillmentStore interface {
	// EnqueueTask writes one pending outbox task (inside the Play txn).
	EnqueueTask(ctx context.Context, t *types.FulfillmentTask) error
}

// HistoryStore persists immutable play history and answers eligibility counts.
type HistoryStore interface {
	InsertHistory(ctx context.Context, h *types.PlayHistory) error
	ListHistory(ctx context.Context, scope types.Scope, gameID, playerID string, limit int, cursor string) ([]types.PlayHistory, string, error)
	// CountPlays returns total plays by a player for a game (all time) and today.
	CountPlays(ctx context.Context, scope types.Scope, gameID, playerID string, since time.Time) (total, today int, err error)
}

// TxRunner runs fn inside a transaction. Implementations put a transaction
// handle into the context; the *Store ports detect and use it. The in-memory
// fake just calls fn directly.
type TxRunner interface {
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// EventSink publishes domain events (play_completed, prize_won, ...). Best-effort;
// failures must not fail a play (the durable record already exists).
type EventSink interface {
	Emit(ctx context.Context, evt Event)
}

// Event is a domain event.
type Event struct {
	Type    string         `json:"type"`
	Scope   types.Scope    `json:"scope"`
	Payload map[string]any `json:"payload"`
}
