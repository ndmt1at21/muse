//go:build integration

// Integration tests for the sqlstore adapter. They spin up REAL Postgres and
// MySQL via testcontainers-go and run one shared contract suite against both —
// proving the dialect abstraction, atomic stock deduction, single-use sessions,
// and scope isolation behave identically on each engine.
//
// Run with: go test -tags integration ./adapters/sqlstore/...
// (Requires a working Docker daemon. Skipped by the default `go test`.)
package sqlstore_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/muse/adapters/sqlstore"
	"github.com/muse/gamekit/defaults"
	"github.com/muse/gamekit/engine"
	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/identity"
	"github.com/muse/gamekit/ports"
	"github.com/muse/gamekit/std"
	"github.com/muse/gamekit/types"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestMain(m *testing.M) {
	// Disable the Ryuk reaper; tests terminate their own containers. Avoids
	// reaper-socket friction in restricted CI sandboxes.
	_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	os.Exit(m.Run())
}

var ids defaults.IDGen

func id(prefix string) string { return ids.NewID(prefix) }

// newDB starts a container for the given engine, opens + migrates the DB, and
// registers cleanup. Returns the connected *sqlstore.DB.
func newDB(t *testing.T, engineName string) *sqlstore.DB {
	t.Helper()
	ctx := context.Background()
	var dsn string

	switch engineName {
	case "postgres":
		c, err := postgres.Run(ctx, "postgres:16-alpine",
			postgres.WithDatabase("muse"),
			postgres.WithUsername("muse"),
			postgres.WithPassword("muse"),
			postgres.BasicWaitStrategies(),
		)
		if err != nil {
			t.Fatalf("start postgres: %v", err)
		}
		t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
		dsn, err = c.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			t.Fatalf("pg dsn: %v", err)
		}
	case "mysql":
		c, err := mysql.Run(ctx, "mysql:8.4",
			mysql.WithDatabase("muse"),
			mysql.WithUsername("muse"),
			mysql.WithPassword("muse"),
		)
		if err != nil {
			t.Fatalf("start mysql: %v", err)
		}
		t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
		dsn, err = c.ConnectionString(ctx, "parseTime=true", "multiStatements=true")
		if err != nil {
			t.Fatalf("mysql dsn: %v", err)
		}
	default:
		t.Fatalf("unknown engine %q", engineName)
	}

	db, err := sqlstore.Open(ctx, engineName, dsn)
	if err != nil {
		t.Fatalf("open %s: %v", engineName, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate %s: %v", engineName, err)
	}
	return db
}

var phoneSeq atomic.Int64

// uniquePhone returns a distinct, well-formed E.164-ish phone (digits only) for
// tests — id()-derived suffixes contain hex letters that fail normalization.
func uniquePhone() string { return fmt.Sprintf("+849%08d", phoneSeq.Add(1)) }

// TestSQLStoreContract runs the shared port-contract suite against both engines.
func TestSQLStoreContract(t *testing.T) {
	for _, eng := range []string{"postgres", "mysql"} {
		t.Run(eng, func(t *testing.T) {
			t.Parallel()
			db := newDB(t, eng)
			ctx := context.Background()
			scope := types.Scope{TenantID: id("tenant"), MerchantID: id("merchant")}

			t.Run("game roundtrip + scope isolation", func(t *testing.T) {
				g := &types.Game{
					ID: id("game"), Scope: scope, CampaignID: "camp_1", Name: "Spin",
					Type: "spin_wheel", SeedGenerator: "none", RewardHandler: "probability",
					Validator: "basic", Status: types.StatusActive,
					Rules:         types.Rules{MaxPlaysPerUser: 3},
					HandlerConfig: json.RawMessage(`{"prizes":[]}`),
				}
				if _, err := db.CreateGame(ctx, g); err != nil {
					t.Fatalf("CreateGame: %v", err)
				}
				got, err := db.GetGame(ctx, scope, g.ID)
				if err != nil {
					t.Fatalf("GetGame: %v", err)
				}
				if got.Name != "Spin" || got.RewardHandler != "probability" || got.Rules.MaxPlaysPerUser != 3 {
					t.Errorf("game roundtrip mismatch: %+v", got)
				}
				// A different tenant must NOT see it.
				other := types.Scope{TenantID: id("tenant"), MerchantID: scope.MerchantID}
				if _, err := db.GetGame(ctx, other, g.ID); gkerr.ReasonOf(err) != gkerr.ReasonNotFound {
					t.Errorf("cross-tenant read should be NOT_FOUND, got %v", err)
				}
			})

			t.Run("prize roundtrip + deduct atomic", func(t *testing.T) {
				p := &types.Prize{ID: id("prize"), Scope: scope, Name: "V", Type: "voucher", Total: 2, Remaining: 2}
				if _, err := db.CreatePrize(ctx, p, "game_x"); err != nil {
					t.Fatalf("CreatePrize: %v", err)
				}
				ok1, _ := db.Deduct(ctx, scope, p.ID)
				ok2, _ := db.Deduct(ctx, scope, p.ID)
				ok3, _ := db.Deduct(ctx, scope, p.ID)
				if !ok1 || !ok2 || ok3 {
					t.Errorf("expected deduct true,true,false; got %v,%v,%v", ok1, ok2, ok3)
				}
				got, _ := db.GetPrize(ctx, scope, p.ID)
				if got.Remaining != 0 {
					t.Errorf("expected remaining 0, got %d", got.Remaining)
				}
			})

			t.Run("concurrent deduct never over-issues", func(t *testing.T) {
				const stock, racers = 10, 50
				p := &types.Prize{ID: id("prize"), Scope: scope, Name: "Ltd", Type: "voucher", Total: stock, Remaining: stock}
				if _, err := db.CreatePrize(ctx, p, "game_x"); err != nil {
					t.Fatalf("CreatePrize: %v", err)
				}
				var wins int64
				var wg sync.WaitGroup
				for i := 0; i < racers; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						if ok, err := db.Deduct(ctx, scope, p.ID); err == nil && ok {
							atomic.AddInt64(&wins, 1)
						}
					}()
				}
				wg.Wait()
				if wins != stock {
					t.Errorf("expected exactly %d wins, got %d", stock, wins)
				}
			})

			t.Run("session single-use", func(t *testing.T) {
				now := time.Now().UTC()
				s := &types.Session{
					ID: id("sess"), Scope: scope, GameID: "game_x", PlayerID: "p1",
					StartedAt: now, ExpiresAt: now.Add(time.Minute),
				}
				if err := db.CreateSession(ctx, s); err != nil {
					t.Fatalf("CreateSession: %v", err)
				}
				if err := db.ConsumeSession(ctx, scope, s.ID); err != nil {
					t.Fatalf("first consume: %v", err)
				}
				if err := db.ConsumeSession(ctx, scope, s.ID); gkerr.ReasonOf(err) != gkerr.ReasonSessionConsumed {
					t.Errorf("second consume should be SESSION_CONSUMED, got %v", err)
				}
			})

			t.Run("history insert + count + list", func(t *testing.T) {
				player := id("player")
				for i := 0; i < 3; i++ {
					h := &types.PlayHistory{
						ID: id("play"), Scope: scope, GameID: "game_h", PlayerID: player,
						SessionID: id("sess"), Rewards: json.RawMessage(`[]`),
						Metadata: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
					}
					if err := db.InsertHistory(ctx, h); err != nil {
						t.Fatalf("InsertHistory: %v", err)
					}
				}
				total, today, err := db.CountPlays(ctx, scope, "game_h", player, time.Now().UTC().Add(-time.Hour))
				if err != nil {
					t.Fatalf("CountPlays: %v", err)
				}
				if total != 3 || today != 3 {
					t.Errorf("expected total=3 today=3, got total=%d today=%d", total, today)
				}
				entries, _, err := db.ListHistory(ctx, scope, "game_h", player, 10, "")
				if err != nil {
					t.Fatalf("ListHistory: %v", err)
				}
				if len(entries) != 3 {
					t.Errorf("expected 3 history entries, got %d", len(entries))
				}
			})

			t.Run("identity: one identity, isolated players across tenants", func(t *testing.T) {
				// Two distinct tenants; the same phone logs into both via the real
				// resolver over the SQL adapter.
				tA := types.Scope{TenantID: id("tenant")}
				tB := types.Scope{TenantID: id("tenant")}
				resolver := identity.New(db, db, defaults.IDGen{}, defaults.Clock{})
				phone := types.Contact{Type: types.ContactPhone, Value: uniquePhone()}

				a, err := resolver.ResolveLogin(ctx, identity.VerifiedLogin{Tenant: tA, Contact: phone})
				if err != nil {
					t.Fatalf("login A: %v", err)
				}
				b, err := resolver.ResolveLogin(ctx, identity.VerifiedLogin{Tenant: tB, Contact: phone})
				if err != nil {
					t.Fatalf("login B: %v", err)
				}
				if a.Identity.ID != b.Identity.ID {
					t.Errorf("same phone must yield one identity: %s vs %s", a.Identity.ID, b.Identity.ID)
				}
				if a.Player.ID == b.Player.ID {
					t.Errorf("players across tenants must be isolated, both = %s", a.Player.ID)
				}
				if !a.IdentityIsNew || b.IdentityIsNew {
					t.Errorf("identity new for A, reused for B; got A=%v B=%v", a.IdentityIsNew, b.IdentityIsNew)
				}
				// Re-login in tenant A reuses the same player (idempotent upsert).
				a2, _ := resolver.ResolveLogin(ctx, identity.VerifiedLogin{Tenant: tA, Contact: phone})
				if a2.Player.ID != a.Player.ID {
					t.Errorf("re-login in same tenant should reuse player: %s vs %s", a2.Player.ID, a.Player.ID)
				}
				// Cross-tenant player isolation: A cannot read B's player.
				if _, err := db.GetPlayer(ctx, tA.TenantID, b.Player.ID); gkerr.ReasonOf(err) != gkerr.ReasonNotFound {
					t.Errorf("cross-tenant player read should be NOT_FOUND, got %v", err)
				}
			})

			t.Run("identity: contact conflict + link merge", func(t *testing.T) {
				resolver := identity.New(db, db, defaults.IDGen{}, defaults.Clock{})
				tn := types.Scope{TenantID: id("tenant")}
				p1 := types.Contact{Type: types.ContactPhone, Value: uniquePhone()}
				p2 := types.Contact{Type: types.ContactPhone, Value: uniquePhone()}
				email := types.Contact{Type: types.ContactEmail, Value: id("") + "@example.com"}

				a, _ := resolver.ResolveLogin(ctx, identity.VerifiedLogin{Tenant: tn, Contact: p1})
				b, _ := resolver.ResolveLogin(ctx, identity.VerifiedLogin{Tenant: tn, Contact: p2})

				// Linking B's phone to A's identity must conflict.
				if _, err := resolver.LinkContact(ctx, a.Identity.ID, p2); gkerr.ReasonOf(err) != gkerr.ReasonContactConflict {
					t.Errorf("expected CONTACT_CONFLICT, got %v", err)
				}
				// Linking a fresh email to A merges; logging in by it hits A's identity.
				if _, err := resolver.LinkContact(ctx, a.Identity.ID, email); err != nil {
					t.Fatalf("link email: %v", err)
				}
				byEmail, err := resolver.ResolveLogin(ctx, identity.VerifiedLogin{Tenant: tn, Contact: email})
				if err != nil {
					t.Fatalf("login by email: %v", err)
				}
				if byEmail.Identity.ID != a.Identity.ID {
					t.Errorf("linked email should resolve to A's identity")
				}
				_ = b
			})

			t.Run("turn balances grant/consume atomic", func(t *testing.T) {
				player := id("player")
				if _, err := db.GrantTurns(ctx, scope.TenantID, player, "camp_t", 5); err != nil {
					t.Fatalf("GrantTurns: %v", err)
				}
				// Concurrent consumes never go below zero.
				const racers = 12
				var consumed int64
				var wg sync.WaitGroup
				for i := 0; i < racers; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						if ok, _, err := db.ConsumeTurn(ctx, scope.TenantID, player, "camp_t"); err == nil && ok {
							atomic.AddInt64(&consumed, 1)
						}
					}()
				}
				wg.Wait()
				if consumed != 5 {
					t.Errorf("expected exactly 5 consumed turns, got %d", consumed)
				}
				if bal, _ := db.GetTurnBalance(ctx, scope.TenantID, player, "camp_t"); bal != 0 {
					t.Errorf("expected balance 0, got %d", bal)
				}
			})

			t.Run("tenant + merchant roundtrip", func(t *testing.T) {
				tn, err := db.CreateTenant(ctx, &types.Tenant{
					Name: "Acme", Plan: "pro",
					Settings: types.TenantSettings{IdentityLinking: true, WalletScope: types.WalletScopeTenant},
				})
				if err != nil {
					t.Fatalf("CreateTenant: %v", err)
				}
				got, err := db.GetTenant(ctx, tn.ID)
				if err != nil || got.Settings.WalletScope != types.WalletScopeTenant {
					t.Fatalf("tenant roundtrip: %+v err=%v", got, err)
				}
				m, err := db.CreateMerchant(ctx, &types.Merchant{TenantID: tn.ID, Name: "Acme Coffee"})
				if err != nil {
					t.Fatalf("CreateMerchant: %v", err)
				}
				// Another tenant cannot read this merchant.
				if _, err := db.GetMerchant(ctx, id("tenant"), m.ID); gkerr.ReasonOf(err) != gkerr.ReasonNotFound {
					t.Errorf("cross-tenant merchant read should be NOT_FOUND, got %v", err)
				}
			})

			t.Run("campaign CRUD + scope isolation + public lookup + duplicate", func(t *testing.T) {
				start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
				c, err := db.CreateCampaign(ctx, &types.Campaign{
					TenantID: scope.TenantID, MerchantID: scope.MerchantID, Name: "Tết 2027",
					StartDate: &start, Channels: []string{"website_embed", "qr_code"},
					Games: []string{"game_x"}, Quests: []string{"quest_x"},
					Settings: types.CampaignSettings{RequireAuth: true, AuthMethods: []string{"phone_otp"}, MaxPlaysPerUser: 5},
				})
				if err != nil {
					t.Fatalf("CreateCampaign: %v", err)
				}
				if c.Status != types.CampaignDraft {
					t.Errorf("new campaign should default to draft, got %q", c.Status)
				}
				got, err := db.GetCampaign(ctx, scope.TenantID, scope.MerchantID, c.ID)
				if err != nil {
					t.Fatalf("GetCampaign: %v", err)
				}
				if got.Name != "Tết 2027" || len(got.Channels) != 2 || !got.Settings.RequireAuth ||
					got.StartDate == nil || !got.StartDate.Equal(start) {
					t.Errorf("campaign roundtrip mismatch: %+v", got)
				}
				// Public lookup by id alone works.
				if pub, err := db.GetCampaignByID(ctx, c.ID); err != nil || pub.ID != c.ID {
					t.Errorf("GetCampaignByID: %+v err=%v", pub, err)
				}
				// Cross-merchant read must be NOT_FOUND.
				if _, err := db.GetCampaign(ctx, scope.TenantID, id("merchant"), c.ID); gkerr.ReasonOf(err) != gkerr.ReasonNotFound {
					t.Errorf("cross-merchant campaign read should be NOT_FOUND, got %v", err)
				}
				// Update flips status; List sees it.
				got.Status = types.CampaignActive
				got.Name = "Tết 2027 (live)"
				if _, err := db.UpdateCampaign(ctx, got); err != nil {
					t.Fatalf("UpdateCampaign: %v", err)
				}
				list, _, err := db.ListCampaigns(ctx, scope.TenantID, scope.MerchantID, 50, "")
				if err != nil || len(list) == 0 {
					t.Fatalf("ListCampaigns: len=%d err=%v", len(list), err)
				}
				// Duplicate clones links but resets to draft with a fresh id.
				dup, err := db.DuplicateCampaign(ctx, scope.TenantID, scope.MerchantID, c.ID, "")
				if err != nil {
					t.Fatalf("DuplicateCampaign: %v", err)
				}
				if dup.ID == c.ID || dup.Status != types.CampaignDraft || len(dup.Games) != 1 {
					t.Errorf("duplicate mismatch: %+v", dup)
				}
			})

			t.Run("campaign analytics rollup", func(t *testing.T) {
				c, err := db.CreateCampaign(ctx, &types.Campaign{
					TenantID: scope.TenantID, MerchantID: scope.MerchantID, Name: "Analytics",
				})
				if err != nil {
					t.Fatalf("CreateCampaign: %v", err)
				}
				// A game under this campaign, then 3 plays by 2 players and 2 wins
				// (one revoked → not counted).
				g := &types.Game{
					ID: id("game"), Scope: scope, CampaignID: c.ID, Name: "Spin",
					Type: "spin_wheel", SeedGenerator: "none", RewardHandler: "probability",
					Validator: "basic", Status: types.StatusActive,
				}
				if _, err := db.CreateGame(ctx, g); err != nil {
					t.Fatalf("CreateGame: %v", err)
				}
				pA, pB := id("player"), id("player")
				for _, pl := range []string{pA, pA, pB} {
					if err := db.InsertHistory(ctx, &types.PlayHistory{
						ID: id("play"), Scope: scope, GameID: g.ID, PlayerID: pl, SessionID: id("sess"),
						Rewards: json.RawMessage(`[]`), Metadata: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
					}); err != nil {
						t.Fatalf("InsertHistory: %v", err)
					}
				}
				for _, st := range []types.RewardStatus{types.RewardWon, types.RewardRevoked} {
					if err := db.InsertReward(ctx, &types.RewardRecord{
						ID: id("rwd"), Scope: scope, GameID: g.ID, PlayerID: pA, PrizeID: id("prize"),
						PlayID: id("play"), Name: "Voucher", Type: "voucher", Status: st, CreatedAt: time.Now().UTC(),
					}); err != nil {
						t.Fatalf("InsertReward: %v", err)
					}
				}
				a, err := db.CampaignAnalytics(ctx, scope.TenantID, scope.MerchantID, c.ID)
				if err != nil {
					t.Fatalf("CampaignAnalytics: %v", err)
				}
				if a.TotalPlays != 3 || a.UniquePlayers != 2 || a.TotalWins != 1 {
					t.Errorf("analytics rollup: plays=%d players=%d wins=%d (want 3/2/1)", a.TotalPlays, a.UniquePlayers, a.TotalWins)
				}
				if a.Conversion < 0.32 || a.Conversion > 0.34 { // 1/3
					t.Errorf("conversion want ~0.333, got %v", a.Conversion)
				}
				// Analytics for a campaign outside this merchant is NOT_FOUND.
				if _, err := db.CampaignAnalytics(ctx, scope.TenantID, id("merchant"), c.ID); gkerr.ReasonOf(err) != gkerr.ReasonNotFound {
					t.Errorf("cross-merchant analytics should be NOT_FOUND, got %v", err)
				}
			})

			t.Run("quest CRUD + scope isolation + completion counts", func(t *testing.T) {
				campaignID := id("campaign")
				q, err := db.CreateQuest(ctx, &types.Quest{
					TenantID: scope.TenantID, MerchantID: scope.MerchantID, CampaignID: campaignID,
					Type: types.QuestShareSocial, Name: "Share us",
					Reward: types.QuestReward{Type: types.QuestRewardPlayTurn, Quantity: 2},
					Config: json.RawMessage(`{"platforms":["facebook"]}`),
				})
				if err != nil {
					t.Fatalf("CreateQuest: %v", err)
				}
				if q.Status != types.QuestActive {
					t.Errorf("new quest should default to active, got %q", q.Status)
				}
				got, err := db.GetQuest(ctx, scope.TenantID, scope.MerchantID, q.ID)
				if err != nil || got.Reward.Quantity != 2 || got.Type != types.QuestShareSocial {
					t.Fatalf("quest roundtrip mismatch: %+v err=%v", got, err)
				}
				// Cross-merchant read is NOT_FOUND.
				if _, err := db.GetQuest(ctx, scope.TenantID, id("merchant"), q.ID); gkerr.ReasonOf(err) != gkerr.ReasonNotFound {
					t.Errorf("cross-merchant quest read should be NOT_FOUND, got %v", err)
				}
				// List filtered by campaign sees it.
				list, _, err := db.ListQuests(ctx, scope.TenantID, scope.MerchantID, campaignID, 50, "")
				if err != nil || len(list) != 1 {
					t.Fatalf("ListQuests(campaign): len=%d err=%v", len(list), err)
				}

				// Completion counts: none, then one today.
				player := id("player")
				total, today, last, err := db.CountCompletions(ctx, scope.TenantID, q.ID, player, startOfUTCDayT(time.Now()))
				if err != nil || total != 0 || today != 0 || last != nil {
					t.Fatalf("empty completions: total=%d today=%d last=%v err=%v", total, today, last, err)
				}
				if err := db.RecordCompletion(ctx, &types.QuestCompletion{
					TenantID: scope.TenantID, QuestID: q.ID, PlayerID: player, CreatedAt: time.Now().UTC(),
				}); err != nil {
					t.Fatalf("RecordCompletion: %v", err)
				}
				total, today, last, err = db.CountCompletions(ctx, scope.TenantID, q.ID, player, startOfUTCDayT(time.Now()))
				if err != nil || total != 1 || today != 1 || last == nil {
					t.Errorf("after completion: total=%d today=%d last=%v err=%v", total, today, last, err)
				}

				// Update + delete.
				got.Status = types.QuestInactive
				if _, err := db.UpdateQuest(ctx, got); err != nil {
					t.Fatalf("UpdateQuest: %v", err)
				}
				if err := db.DeleteQuest(ctx, scope.TenantID, scope.MerchantID, q.ID); err != nil {
					t.Fatalf("DeleteQuest: %v", err)
				}
				if _, err := db.GetQuest(ctx, scope.TenantID, scope.MerchantID, q.ID); gkerr.ReasonOf(err) != gkerr.ReasonNotFound {
					t.Errorf("deleted quest should be NOT_FOUND, got %v", err)
				}
			})

			t.Run("leaderboard config + entries + ranking + admin actions", func(t *testing.T) {
				campaignID := id("campaign")
				lb, err := db.CreateLeaderboard(ctx, &types.Leaderboard{
					TenantID: scope.TenantID, MerchantID: scope.MerchantID, CampaignID: campaignID,
					Name: "Top Tuần", Metric: types.MetricTotalScore,
					Window:     types.TimeWindow{Type: types.WindowFixed},
					PrizeTiers: []types.PrizeTier{{FromRank: 1, ToRank: 1, PrizeID: "p_top"}},
					AntiCheat:  types.AntiCheat{ScoreCeiling: 1000, FlagOutliers: true},
				})
				if err != nil {
					t.Fatalf("CreateLeaderboard: %v", err)
				}
				if lb.Status != types.LeaderboardActive {
					t.Errorf("new leaderboard should be active, got %q", lb.Status)
				}
				// Active-for-campaign sees it.
				active, err := db.ActiveLeaderboardsForCampaign(ctx, scope.TenantID, scope.MerchantID, campaignID)
				if err != nil || len(active) != 1 {
					t.Fatalf("ActiveLeaderboardsForCampaign: len=%d err=%v", len(active), err)
				}
				// Cross-merchant get is NOT_FOUND.
				if _, err := db.GetLeaderboard(ctx, scope.TenantID, id("merchant"), lb.ID); gkerr.ReasonOf(err) != gkerr.ReasonNotFound {
					t.Errorf("cross-merchant leaderboard read should be NOT_FOUND, got %v", err)
				}

				const wk = "fixed"
				pA, pB, pC := id("player"), id("player"), id("player")
				// Accumulate scores: A=30, B=20, C=50.
				for player, n := range map[string]int{pA: 30, pB: 20, pC: 50} {
					if _, err := db.UpsertEntry(ctx, &types.LeaderboardEntry{
						TenantID: scope.TenantID, LeaderboardID: lb.ID, WindowKey: wk, PlayerID: player,
					}, int64(n), false); err != nil {
						t.Fatalf("UpsertEntry: %v", err)
					}
				}
				// Ranking: C(50) > A(30) > B(20).
				ranked, total, err := db.Rankings(ctx, scope.TenantID, lb.ID, wk, 10, 0)
				if err != nil || total != 3 || len(ranked) != 3 {
					t.Fatalf("Rankings: total=%d len=%d err=%v", total, len(ranked), err)
				}
				if ranked[0].PlayerID != pC || ranked[0].Rank != 1 || ranked[1].PlayerID != pA || ranked[2].PlayerID != pB {
					t.Errorf("ranking order wrong: %+v", ranked)
				}
				// my-rank for A is 2.
				if pr, err := db.PlayerRank(ctx, scope.TenantID, lb.ID, wk, pA); err != nil || pr.Rank != 2 {
					t.Errorf("PlayerRank A: rank=%v err=%v", pr, err)
				}

				// Atomic accumulate under concurrency: +1 x N for a new player.
				pRace := id("player")
				const racers = 25
				var wg sync.WaitGroup
				for i := 0; i < racers; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						_, _ = db.UpsertEntry(ctx, &types.LeaderboardEntry{
							TenantID: scope.TenantID, LeaderboardID: lb.ID, WindowKey: wk, PlayerID: pRace,
						}, 1, false)
					}()
				}
				wg.Wait()
				if e, err := db.PlayerRank(ctx, scope.TenantID, lb.ID, wk, pRace); err != nil || e.Score != racers || e.Plays != racers {
					t.Errorf("concurrent upsert: score=%d plays=%d (want %d) err=%v", e.Score, e.Plays, racers, err)
				}

				// high_score (max) semantics via a second board.
				lbMax, _ := db.CreateLeaderboard(ctx, &types.Leaderboard{
					TenantID: scope.TenantID, MerchantID: scope.MerchantID, CampaignID: campaignID,
					Name: "Best", Metric: types.MetricHighScore, Window: types.TimeWindow{Type: types.WindowFixed},
				})
				for _, s := range []int64{40, 90, 70} {
					if _, err := db.UpsertEntry(ctx, &types.LeaderboardEntry{
						TenantID: scope.TenantID, LeaderboardID: lbMax.ID, WindowKey: wk, PlayerID: pA,
					}, s, true); err != nil {
						t.Fatalf("UpsertEntry max: %v", err)
					}
				}
				if e, _ := db.PlayerRank(ctx, scope.TenantID, lbMax.ID, wk, pA); e.Score != 90 {
					t.Errorf("high_score should keep max 90, got %d", e.Score)
				}

				// Disqualify the leader → excluded from rankings; B's adjust raises it.
				if _, err := db.SetEntryState(ctx, scope.TenantID, lb.ID, wk, pC, types.EntryDisqualified); err != nil {
					t.Fatalf("SetEntryState: %v", err)
				}
				ranked, total, _ = db.Rankings(ctx, scope.TenantID, lb.ID, wk, 10, 0)
				for _, e := range ranked {
					if e.PlayerID == pC {
						t.Errorf("disqualified player still ranked")
					}
				}
				if total != 3 { // pA, pB, pRace remain (pC excluded)
					t.Errorf("rankable total after disqualify: got %d want 3", total)
				}
				if _, err := db.AdjustScore(ctx, scope.TenantID, lb.ID, wk, pB, 1000); err != nil {
					t.Fatalf("AdjustScore: %v", err)
				}
				if e, _ := db.PlayerRank(ctx, scope.TenantID, lb.ID, wk, pB); e.Rank != 1 || e.Score != 1020 {
					t.Errorf("after +1000 adjust B should lead: rank=%d score=%d", e.Rank, e.Score)
				}

				// SnapshotRanking returns active-only, ordered (for finalize).
				snap, err := db.SnapshotRanking(ctx, scope.TenantID, lb.ID, wk)
				if err != nil || len(snap) != 3 || snap[0].PlayerID != pB {
					t.Errorf("SnapshotRanking: len=%d leader=%v err=%v", len(snap), snap, err)
				}
				// Reset clears the window.
				if n, err := db.DeleteEntries(ctx, scope.TenantID, lb.ID, wk); err != nil || n == 0 {
					t.Errorf("DeleteEntries: n=%d err=%v", n, err)
				}
				if _, total, _ := db.Rankings(ctx, scope.TenantID, lb.ID, wk, 10, 0); total != 0 {
					t.Errorf("rankings after reset should be empty, total=%d", total)
				}
			})
		})
	}
}

// startOfUTCDayT mirrors the core layer's start-of-day for the completion "today"
// window in tests.
func startOfUTCDayT(t time.Time) time.Time {
	u := t.UTC()
	y, m, d := u.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// TestEngineIntegration wires the real gamekit engine against each SQL engine
// and runs a concurrent stock-race through the full Start/Play transaction —
// the strongest proof that atomic deduction holds under real DB transactions.
func TestEngineIntegration(t *testing.T) {
	for _, engName := range []string{"postgres", "mysql"} {
		t.Run(engName, func(t *testing.T) {
			t.Parallel()
			db := newDB(t, engName)
			ctx := context.Background()
			scope := types.Scope{TenantID: id("tenant"), MerchantID: id("merchant")}

			prizeID := id("prize")
			const stock = 8
			if _, err := db.CreatePrize(ctx, &types.Prize{
				ID: prizeID, Scope: scope, Name: "Voucher", Type: "voucher", Total: stock, Remaining: stock,
			}, "game_e"); err != nil {
				t.Fatalf("CreatePrize: %v", err)
			}
			gameID := id("game")
			if _, err := db.CreateGame(ctx, &types.Game{
				ID: gameID, Scope: scope, Name: "Spin", Type: "spin_wheel",
				SeedGenerator: "none", RewardHandler: "probability", Validator: "basic",
				Status:        types.StatusActive, // unlimited turns
				HandlerConfig: json.RawMessage(`{"prizes":[{"prize_id":"` + prizeID + `","probability":1.0,"slot_index":0}]}`),
			}); err != nil {
				t.Fatalf("CreateGame: %v", err)
			}

			eng := engine.New(engine.Deps{
				Registry: std.Registry(),
				Games:    db, Prizes: db, Sessions: db, History: db, Tx: db,
				Events: ports.EventSink(nil),
				Clock:  defaults.Clock{}, Rand: defaults.Rand{}, IDs: defaults.IDGen{},
			}, engine.Config{SessionTTL: time.Minute})

			const racers = 40
			var wins, oos int64
			var wg sync.WaitGroup
			for i := 0; i < racers; i++ {
				wg.Add(1)
				go func(n int) {
					defer wg.Done()
					player := id("player") // distinct player avoids per-user cap
					start, err := eng.Start(ctx, scope, gameID, player)
					if err != nil {
						return
					}
					_, err = eng.Play(ctx, scope, gameID, start.SessionID, player, json.RawMessage(`{}`), "trace")
					switch gkerr.ReasonOf(err) {
					case "":
						atomic.AddInt64(&wins, 1)
					case gkerr.ReasonPrizeOutOfStock:
						atomic.AddInt64(&oos, 1)
					}
				}(i)
			}
			wg.Wait()

			if wins != stock {
				t.Errorf("[%s] expected exactly %d wins, got %d (out_of_stock=%d)", engName, stock, wins, oos)
			}
			got, _ := db.GetPrize(ctx, scope, prizeID)
			if got.Remaining != 0 {
				t.Errorf("[%s] expected remaining 0, got %d", engName, got.Remaining)
			}
		})
	}
}

// TestTurnGatedPlay exercises the Phase-6 turn-gating wired into the engine: a
// game with Rules.UseTurns spends the player's quest-granted campaign turn
// balance, eligibility reflects it, and Play fails OUT_OF_TURNS at zero.
func TestTurnGatedPlay(t *testing.T) {
	for _, engName := range []string{"postgres", "mysql"} {
		t.Run(engName, func(t *testing.T) {
			t.Parallel()
			db := newDB(t, engName)
			ctx := context.Background()
			scope := types.Scope{TenantID: id("tenant"), MerchantID: id("merchant")}
			campaignID := id("campaign")
			player := id("player")

			// A turn-gated game: each play always wins the prize (probability 1.0)
			// and consumes one campaign turn.
			gid := id("game")
			prizeID := id("prize")
			if _, err := db.CreatePrize(ctx, &types.Prize{
				ID: prizeID, Scope: scope, Name: "Voucher", Type: "voucher", Total: 100, Remaining: 100,
			}, gid); err != nil {
				t.Fatalf("CreatePrize: %v", err)
			}
			if _, err := db.CreateGame(ctx, &types.Game{
				ID: gid, Scope: scope, CampaignID: campaignID, Name: "Spin",
				Type: "spin_wheel", SeedGenerator: "none", RewardHandler: "probability", Validator: "basic",
				Status: types.StatusActive, Rules: types.Rules{UseTurns: true},
				HandlerConfig: json.RawMessage(`{"prizes":[{"prize_id":"` + prizeID + `","probability":1.0,"slot_index":0}]}`),
			}); err != nil {
				t.Fatalf("CreateGame: %v", err)
			}

			// Engine wired WITH the turn gate (Turns: db).
			eng := engine.New(engine.Deps{
				Registry: std.Registry(),
				Games:    db, Prizes: db, Sessions: db, History: db, Tx: db, Turns: db,
				Clock: defaults.Clock{}, Rand: defaults.Rand{}, IDs: defaults.IDGen{},
			}, engine.Config{SessionTTL: time.Minute})

			// No turns yet → not eligible, and Play fails OUT_OF_TURNS.
			if el, err := eng.Eligibility(ctx, scope, gid, player); err != nil || el.CanPlay {
				t.Fatalf("expected not eligible with 0 turns: el=%+v err=%v", el, err)
			}

			// Grant 2 turns to the campaign balance (as a quest completion would).
			if _, err := db.GrantTurns(ctx, scope.TenantID, player, campaignID, 2); err != nil {
				t.Fatalf("GrantTurns: %v", err)
			}
			if el, err := eng.Eligibility(ctx, scope, gid, player); err != nil || !el.CanPlay || el.RemainingPlays != 2 {
				t.Fatalf("expected 2 remaining: el=%+v err=%v", el, err)
			}

			// Play twice → consumes both turns.
			for i := 0; i < 2; i++ {
				st, err := eng.Start(ctx, scope, gid, player)
				if err != nil {
					t.Fatalf("Start %d: %v", i, err)
				}
				if _, err := eng.Play(ctx, scope, gid, st.SessionID, player, json.RawMessage(`{}`), "trace"); err != nil {
					t.Fatalf("Play %d: %v", i, err)
				}
			}
			if bal, _ := db.GetTurnBalance(ctx, scope.TenantID, player, campaignID); bal != 0 {
				t.Errorf("expected 0 turns after 2 plays, got %d", bal)
			}

			// Third play is out of turns (the in-tx consume rejects it).
			st, err := eng.Start(ctx, scope, gid, player)
			if err != nil {
				t.Fatalf("Start 3: %v", err)
			}
			_, err = eng.Play(ctx, scope, gid, st.SessionID, player, json.RawMessage(`{}`), "trace")
			if gkerr.ReasonOf(err) != gkerr.ReasonOutOfTurns {
				t.Errorf("third play should be OUT_OF_TURNS, got %v", err)
			}
		})
	}
}
