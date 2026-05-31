// Command bff-consumer is the public-internet BFF: it serves the widget +
// player REST surface over Core gRPC, applies the uniform envelope, CORS for
// embed, and trace-id propagation. Phase 9 hardening: soft player-JWT auth, a
// distributed (Redis) per-player/IP rate limit on /start + /play, and a
// read-model cache for the public widget config + top-N leaderboard.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/muse/adapters/redisstore"
	"github.com/muse/bffkit/auth"
	"github.com/muse/bffkit/cache"
	"github.com/muse/bffkit/coreclient"
	"github.com/muse/bffkit/middleware"
	"github.com/muse/bffkit/obs"
	"github.com/muse/bffkit/ratelimit"
	consumercampaign "github.com/muse/examples/bff-consumer/internal/campaign"
	"github.com/muse/examples/bff-consumer/internal/game"
	consumerleaderboard "github.com/muse/examples/bff-consumer/internal/leaderboard"
	consumerplayer "github.com/muse/examples/bff-consumer/internal/player"
	consumerquest "github.com/muse/examples/bff-consumer/internal/quest"
	consumerwallet "github.com/muse/examples/bff-consumer/internal/wallet"
)

func main() {
	addr := env("BFF_CONSUMER_ADDR", ":8080")
	coreAddr := env("CORE_GRPC_ADDR", "localhost:9090")
	jwtSecret := env("JWT_SECRET", "")
	redisAddr := env("REDIS_ADDR", "")
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	core, err := coreclient.Dial(coreAddr)
	if err != nil {
		log.Error("dial core failed", "addr", coreAddr, "err", err)
		os.Exit(1)
	}
	defer core.Close()

	// Redis backs the distributed rate limit + read-model cache. Both BFFs share
	// the "muse:bff" namespace so the admin BFF can invalidate what the consumer
	// caches. Redis is optional: without it, limiting and caching degrade to
	// no-ops (gameplay still works).
	metrics := obs.New() // Prometheus RED + cache metrics (Phase 11)

	var rl ratelimit.Limiter
	var configCache, rankCache *cache.Cache
	if redisAddr != "" {
		if rc, rErr := redisstore.Open(context.Background(), redisAddr, "muse:bff"); rErr != nil {
			log.Warn("redis unavailable; rate limiting + read-model cache disabled", "err", rErr)
		} else {
			defer rc.Close()
			rl = rc
			configCache = cache.New(rc, 60*time.Second).Observe(metrics.IncCache) // widget config: longer TTL + explicit bust
			rankCache = cache.New(rc, 5*time.Second).Observe(metrics.IncCache)    // live rankings: short TTL, no bust
		}
	}
	playLimit := ratelimit.Middleware(rl, ratelimit.Config{Name: "play", Limit: rateLimit(), Window: time.Minute})

	r := chi.NewRouter()
	r.Use(middleware.TraceID)
	r.Use(middleware.Recover(log))
	r.Use(middleware.Logger(log))
	r.Use(metrics.Middleware) // RED metrics per route
	r.Use(middleware.CORS)
	r.Handle("/metrics", metrics.Handler())
	// Soft player-JWT verification: attaches claims when a Bearer token is
	// present, else passes through (public/widget + legacy header callers).
	r.Use(auth.NewVerifier(jwtSecret).Bearer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })

	gh := game.New(core, playLimit)
	ph := consumerplayer.New(core)
	ch := consumercampaign.New(core, configCache)
	qh := consumerquest.New(core)
	lh := consumerleaderboard.New(core, rankCache)
	wh := consumerwallet.New(core)
	r.Route("/api/v1", func(r chi.Router) {
		gh.Routes(r)
		gh.RewardRoutes(r)
		ph.Routes(r)
		ch.Routes(r)
		qh.Routes(r)
		lh.Routes(r)
		wh.Routes(r)
	})

	srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Info("bff-consumer listening", "addr", addr, "core", coreAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("serve error", "err", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// rateLimit reads the per-player/per-IP gameplay limit per minute for /start +
// /play (PLAY_RATE_LIMIT, default 60).
func rateLimit() int {
	if v := os.Getenv("PLAY_RATE_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 60
}
