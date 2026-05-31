// Command core is the hosted API service (Mode B): it wires the concrete
// adapters (sqlstore + redisstore) into the pure gamekit engine and serves the
// proto gRPC contracts. Run with no args to serve; run "core migrate" to apply
// migrations and exit.
package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/muse/adapters/events"
	"github.com/muse/adapters/redisstore"
	"github.com/muse/adapters/sqlstore"
	"github.com/muse/core/internal/fulfillment"
	"github.com/muse/core/internal/grpcsvc"
	"github.com/muse/core/internal/integration"
	"github.com/muse/core/internal/leaderboard"
	"github.com/muse/core/internal/player"
	"github.com/muse/core/internal/wallet"
	"github.com/muse/core/platform"
	"github.com/muse/gamekit/defaults"
	"github.com/muse/gamekit/engine"
	"github.com/muse/gamekit/identity"
	"github.com/muse/gamekit/ports"
	"github.com/muse/gamekit/std"
	"github.com/muse/gamekit/types"
	gamev1 "github.com/muse/pkg/gen/game/v1"
	"google.golang.org/grpc"
)

func main() {
	cfg := platform.LoadConfig()
	log := platform.NewLogger(cfg.LogLevel)

	ctx := context.Background()

	// Open the database (dual-engine).
	store, err := sqlstore.Open(ctx, cfg.DBEngine, cfg.DSN)
	if err != nil {
		log.Error("db open failed", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	// Subcommand: migrate-and-exit.
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := store.Migrate(); err != nil {
			log.Error("migrate failed", "err", err)
			os.Exit(1)
		}
		log.Info("migrations applied", "engine", cfg.DBEngine)
		return
	}

	// Always ensure schema exists on boot (idempotent).
	if err := store.Migrate(); err != nil {
		log.Error("migrate failed", "err", err)
		os.Exit(1)
	}

	// Redis is optional — without it, idempotency degrades but gameplay works.
	var cache *redisstore.Cache
	if cfg.RedisAddr != "" {
		c, rErr := redisstore.Open(ctx, cfg.RedisAddr, "muse:core")
		if rErr != nil {
			log.Warn("redis unavailable; idempotency disabled", "err", rErr)
		} else {
			cache = c
			defer cache.Close()
		}
	}

	// Leaderboard service: durable entries in SQL, with an optional Redis sorted-
	// set mirror + finalize lock when a cache is available. It is the engine's
	// post-Play standings hook and backs the LeaderboardService gRPC surface.
	var rankBoard ports.RankBoard
	var locker ports.Locker
	if cache != nil {
		rankBoard, locker = cache, cache
	}
	lbService := leaderboard.New(store, rankBoard, locker, defaults.IDGen{}, defaults.Clock{}, log)

	// Wallet service (Phase 8): the engine's in-Play WalletRouter (credit
	// points/lucky_item + auto-grant cumulative milestones) and the player-facing
	// redeem / milestone-progress surface. Pure rules live in gamekit/wallet.
	walletService := wallet.New(store, defaults.IDGen{}, defaults.Clock{}, log)

	// Integration hub (Phase 10): outbound adapters fired on domain events. It is
	// the engine's EventSink and backs the IntegrationService CRUD + EmitEvent.
	// The optional Redis pub/sub bus fans events out to other processes (realtime
	// gateway, extra replicas); without it, in-process dispatch still runs.
	var bus integration.Bus
	if cfg.RedisAddr != "" {
		if b, bErr := events.Open(ctx, cfg.RedisAddr, "muse"); bErr != nil {
			log.Warn("event bus unavailable; cross-process fan-out disabled", "err", bErr)
		} else {
			defer b.Close()
			bus = b
		}
	}
	intgReg := integration.NewRegistry()
	integration.RegisterBuiltins(intgReg, &http.Client{Timeout: 10 * time.Second}, log)
	hub := integration.NewHub(store, intgReg, bus, log)

	// Prometheus metrics (Phase 11): RED for the gRPC surface + business counters
	// fed from domain events (via metricsSink in the engine's EventSink chain) and
	// the fulfillment dispatcher. Exposed at /metrics on the health server.
	metrics := platform.NewMetrics()

	// Build the engine from the pure SDK + concrete adapters. The registry is
	// shared with the gRPC server so quest verifiers resolve the same way.
	registry := std.Registry()
	eng := engine.New(engine.Deps{
		Registry:    registry,
		Games:       store,
		Prizes:      store,
		Sessions:    store,
		History:     store,
		Rewards:     store,
		Fulfill:     store,
		Turns:       store, // enables Rules.UseTurns gating (quest-granted turns)
		Leaderboard: lbService,
		Wallet:      walletService, // in-Play points/lucky_item credit + milestone auto-grant
		Tx:          store,
		Events:      multiSink{&slogSink{log: log}, hub, metricsSink{metrics}}, // log + integrations + metrics
		Clock:       defaults.Clock{},
		Rand:        defaults.Rand{},
		IDs:         defaults.IDGen{},
	}, engine.Config{SessionTTL: 10 * time.Minute})

	// Phase 4: identity resolver + player authenticator (phone/email login,
	// resolve-or-create identity, upsert tenant player, issue player JWT).
	clock := defaults.Clock{}
	resolver := identity.New(store, store, defaults.IDGen{}, clock)
	auth := player.New(
		player.NewSQLChallengeStore(store), resolver, defaults.IDGen{}, defaults.Rand{}, clock,
		player.Config{JWTSecret: cfg.JWTSecret, DevMode: cfg.AuthDevMode},
	)
	if cfg.JWTSecret == "" {
		log.Warn("JWT_SECRET is empty; player tokens cannot be issued — set JWT_SECRET")
	}

	svc := grpcsvc.NewServer(eng, store, cache, grpcsvc.Deps{
		Auth: auth, Resolver: resolver, Registry: registry, Leaderboard: lbService, Wallet: walletService,
		Integration: hub, Clock: clock,
	}, log)

	// Fulfillment dispatcher: drains the transactional outbox and delivers tasks
	// through the channel providers (voucher_code + stubs + n8n external_workflow).
	reg := fulfillment.NewRegistry()
	fulfillment.RegisterBuiltins(reg, store, log)
	reg.Register(types.ChannelExternalWorkflow, fulfillment.NewExternalWorkflowProvider(
		&http.Client{Timeout: 10 * time.Second}, cfg.N8NWebhookURL, cfg.N8NHMACSecret, cfg.CallbackBaseURL, log))
	dispatcher := fulfillment.NewDispatcher(store, reg, fulfillment.Config{
		Interval: cfg.FulfillmentInterval, OnOutcome: metrics.IncFulfillment,
	}, log)
	dispCtx, dispCancel := context.WithCancel(context.Background())
	if cfg.FulfillmentEnabled {
		go dispatcher.Run(dispCtx)
	}

	// gRPC server (trace-id propagation + RED metrics interceptors).
	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
		grpcsvc.TraceIDInterceptor, grpcsvc.MetricsInterceptor(metrics)))
	gamev1.RegisterEngineServiceServer(grpcServer, svc)
	gamev1.RegisterGameAdminServiceServer(grpcServer, svc)
	gamev1.RegisterRewardServiceServer(grpcServer, svc)
	gamev1.RegisterFulfillmentServiceServer(grpcServer, svc)
	gamev1.RegisterTenantServiceServer(grpcServer, svc)
	gamev1.RegisterMerchantServiceServer(grpcServer, svc)
	gamev1.RegisterIdentityServiceServer(grpcServer, svc)
	gamev1.RegisterPlayerServiceServer(grpcServer, svc)
	gamev1.RegisterCampaignServiceServer(grpcServer, svc)
	gamev1.RegisterQuestServiceServer(grpcServer, svc)
	gamev1.RegisterLeaderboardServiceServer(grpcServer, svc)
	gamev1.RegisterWalletServiceServer(grpcServer, svc)
	gamev1.RegisterIntegrationServiceServer(grpcServer, svc)

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		log.Error("listen failed", "addr", cfg.GRPCAddr, "err", err)
		os.Exit(1)
	}

	// Health/metrics server.
	health := platform.NewHealthServer(map[string]platform.Checker{
		"db": store.Ping,
		"redis": func(ctx context.Context) error {
			if cache == nil {
				return nil
			}
			return cache.Ping(ctx)
		},
	})
	health.WithMetrics(metrics.Handler())
	healthSrv := &http.Server{Addr: cfg.HealthAddr, Handler: health.Handler()}
	go func() {
		log.Info("health server listening", "addr", cfg.HealthAddr)
		if err := healthSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("health server error", "err", err)
		}
	}()

	go func() {
		log.Info("core gRPC listening", "addr", cfg.GRPCAddr, "engine", cfg.DBEngine)
		if err := grpcServer.Serve(lis); err != nil {
			log.Error("grpc serve error", "err", err)
		}
	}()

	// Graceful shutdown.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Info("shutting down")
	dispCancel()
	grpcServer.GracefulStop()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = healthSrv.Shutdown(shutCtx)
}

// slogSink is a minimal EventSink that logs domain events.
type slogSink struct{ log *slog.Logger }

func (s *slogSink) Emit(ctx context.Context, evt ports.Event) {
	s.log.Info("event", "type", evt.Type, "tenant", evt.Scope.TenantID, "payload", evt.Payload)
}

// multiSink fans a domain event out to several EventSinks (the structured log +
// the integration hub + metrics). Each is best-effort; the engine calls Emit
// post-commit.
type multiSink []ports.EventSink

func (m multiSink) Emit(ctx context.Context, evt ports.Event) {
	for _, s := range m {
		s.Emit(ctx, evt)
	}
}

// metricsSink turns domain events into Prometheus business counters.
type metricsSink struct{ m *platform.Metrics }

func (s metricsSink) Emit(_ context.Context, evt ports.Event) { s.m.IncEvent(evt.Type) }
