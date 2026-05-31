//go:build integration

// End-to-end leaderboard test: a real engine wired with the leaderboard Service
// over a real Postgres (testcontainers). It proves the Play hook folds plays
// into standings, the Play response carries the new rank, the service serves
// rankings from the durable store (no Redis here), and finalize batch-awards the
// configured prize tier (minting a reward + deducting tier stock).
//
// Run with: go test -tags integration ./core/internal/leaderboard/...
package leaderboard_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/muse/adapters/sqlstore"
	corelb "github.com/muse/core/internal/leaderboard"
	"github.com/muse/gamekit/defaults"
	"github.com/muse/gamekit/engine"
	"github.com/muse/gamekit/std"
	"github.com/muse/gamekit/types"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	os.Exit(m.Run())
}

var idgen defaults.IDGen

func id(p string) string { return idgen.NewID(p) }

func newPG(t *testing.T) *sqlstore.DB {
	t.Helper()
	ctx := context.Background()
	c, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("muse"), postgres.WithUsername("muse"), postgres.WithPassword("muse"),
		postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("dsn: %v", err)
	}
	db, err := sqlstore.Open(ctx, "postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestLeaderboardThroughPlayAndFinalize(t *testing.T) {
	db := newPG(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	scope := types.Scope{TenantID: id("tenant"), MerchantID: id("merchant")}
	campaignID := id("campaign")

	// Leaderboard: rank by total_plays, fixed window, top-1 wins a tier prize.
	tierPrize, err := db.CreatePrize(ctx, &types.Prize{
		ID: id("prize"), Scope: scope, Name: "Grand Prize", Type: "voucher", Total: 5, Remaining: 5,
	}, "")
	if err != nil {
		t.Fatalf("CreatePrize tier: %v", err)
	}
	svc := corelb.New(db, nil, nil, defaults.IDGen{}, defaults.Clock{}, log)
	lb, err := svc.Create(ctx, &types.Leaderboard{
		TenantID: scope.TenantID, MerchantID: scope.MerchantID, CampaignID: campaignID,
		Name: "Đua Top", Metric: types.MetricTotalPlays, Window: types.TimeWindow{Type: types.WindowFixed},
		PrizeTiers: []types.PrizeTier{{FromRank: 1, ToRank: 1, PrizeID: tierPrize.ID}},
	})
	if err != nil {
		t.Fatalf("Create leaderboard: %v", err)
	}

	// A playable game under the campaign that always awards a small prize.
	winPrize, _ := db.CreatePrize(ctx, &types.Prize{
		ID: id("prize"), Scope: scope, Name: "Win", Type: "points", Total: 1000, Remaining: 1000,
	}, "g")
	gameID := id("game")
	if _, err := db.CreateGame(ctx, &types.Game{
		ID: gameID, Scope: scope, CampaignID: campaignID, Name: "Spin", Type: "spin_wheel",
		SeedGenerator: "none", RewardHandler: "probability", Validator: "basic", Status: types.StatusActive,
		HandlerConfig: json.RawMessage(`{"prizes":[{"prize_id":"` + winPrize.ID + `","probability":1.0,"slot_index":0}]}`),
	}); err != nil {
		t.Fatalf("CreateGame: %v", err)
	}

	eng := engine.New(engine.Deps{
		Registry: std.Registry(),
		Games:    db, Prizes: db, Sessions: db, History: db, Rewards: db, Fulfill: db, Tx: db,
		Leaderboard: svc,
		Clock:       defaults.Clock{}, Rand: defaults.Rand{}, IDs: defaults.IDGen{},
	}, engine.Config{SessionTTL: time.Minute})

	play := func(player string) *types.PlayResult {
		st, err := eng.Start(ctx, scope, gameID, player)
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		res, err := eng.Play(ctx, scope, gameID, st.SessionID, player, json.RawMessage(`{}`), "trace")
		if err != nil {
			t.Fatalf("Play: %v", err)
		}
		return res
	}

	pA, pB, pC := id("player"), id("player"), id("player")
	play(pA) // A leads with 2 plays
	res := play(pA)
	play(pB)
	play(pC)

	// The Play response metadata carries the new rank(s).
	if res.Metadata == nil || res.Metadata["rankings"] == nil {
		t.Errorf("expected rankings in play metadata, got %+v", res.Metadata)
	}

	// Rankings served from the durable store: A(2) ahead of B(1), C(1).
	ranked, total, _, err := svc.Rankings(ctx, scope, lb.ID, 10, 0)
	if err != nil || total != 3 {
		t.Fatalf("Rankings: total=%d err=%v", total, err)
	}
	if ranked[0].PlayerID != pA || ranked[0].Rank != 1 || ranked[0].Score != 2 {
		t.Errorf("expected A rank1 score2, got %+v", ranked[0])
	}

	// my-rank for B reports its position and distance to the top tier.
	entry, nextFrom, toGo, err := svc.MyRank(ctx, scope, lb.ID, pB)
	if err != nil || entry.Rank < 2 {
		t.Errorf("MyRank B: entry=%+v err=%v", entry, err)
	}
	if nextFrom != 1 || toGo < 1 {
		t.Errorf("MyRank B distance: nextFrom=%d toGo=%d", nextFrom, toGo)
	}

	// Finalize: top-1 (A) wins the tier prize → one award, stock deducted, board finalized.
	awards, err := svc.Finalize(ctx, scope, lb.ID)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if len(awards) != 1 || awards[0].PlayerID != pA || awards[0].PrizeID != tierPrize.ID {
		t.Fatalf("expected 1 award to A, got %+v", awards)
	}
	if got, _ := db.GetPrize(ctx, scope, tierPrize.ID); got.Remaining != 4 {
		t.Errorf("tier prize stock should drop to 4, got %d", got.Remaining)
	}
	rewards, _, _ := db.ListRewards(ctx, scope, pA, 10, "")
	foundTier := false
	for _, r := range rewards {
		if r.PrizeID == tierPrize.ID && r.PlayID == "lb:"+lb.ID {
			foundTier = true
		}
	}
	if !foundTier {
		t.Errorf("expected a minted tier reward for A, got %+v", rewards)
	}
	if got, _ := svc.Get(ctx, scope.TenantID, scope.MerchantID, lb.ID); got.Status != types.LeaderboardFinalized {
		t.Errorf("leaderboard should be finalized, got %q", got.Status)
	}
	// A second finalize is rejected.
	if _, err := svc.Finalize(ctx, scope, lb.ID); err == nil {
		t.Errorf("re-finalize should fail")
	}
}
