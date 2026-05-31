// Command bff-admin is the internal/VPN BFF: dashboard management endpoints +
// machine callbacks. No public exposure, no embed CORS. The management surface
// is role-guarded (admin/designer/reward_manager); the HMAC-signed n8n
// fulfillment callback stays outside the role gate (machine caller).
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/muse/adapters/redisstore"
	admincampaign "github.com/muse/bff-admin/internal/campaign"
	adminfulfillment "github.com/muse/bff-admin/internal/fulfillment"
	admingame "github.com/muse/bff-admin/internal/game"
	adminintegration "github.com/muse/bff-admin/internal/integration"
	adminleaderboard "github.com/muse/bff-admin/internal/leaderboard"
	adminquest "github.com/muse/bff-admin/internal/quest"
	admintenancy "github.com/muse/bff-admin/internal/tenancy"
	"github.com/muse/bffkit/auth"
	"github.com/muse/bffkit/cache"
	"github.com/muse/bffkit/coreclient"
	"github.com/muse/bffkit/middleware"
	"github.com/muse/bffkit/obs"
)

func main() {
	addr := env("BFF_ADMIN_ADDR", ":8081")
	coreAddr := env("CORE_GRPC_ADDR", "localhost:9090")
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	core, err := coreclient.Dial(coreAddr)
	if err != nil {
		log.Error("dial core failed", "addr", coreAddr, "err", err)
		os.Exit(1)
	}
	defer core.Close()

	// Shared read-model cache (same "muse:bff" Redis namespace the consumer BFF
	// reads), used here only to invalidate the public campaign config on update.
	// Optional: without Redis, invalidation is a no-op.
	var rmcache *cache.Cache
	if redisAddr := env("REDIS_ADDR", ""); redisAddr != "" {
		if rc, rErr := redisstore.Open(context.Background(), redisAddr, "muse:bff"); rErr != nil {
			log.Warn("redis unavailable; read-model cache invalidation disabled", "err", rErr)
		} else {
			defer rc.Close()
			rmcache = cache.New(rc, 0)
		}
	}

	metrics := obs.New() // Prometheus RED metrics (Phase 11)

	r := chi.NewRouter()
	r.Use(middleware.TraceID)
	r.Use(middleware.Recover(log))
	r.Use(middleware.Logger(log))
	r.Use(metrics.Middleware)
	// Verify admin JWTs when present (claims feed the scope + roles seam); header
	// fallback still works for dev/e2e. No CORS — admin is not embedded.
	r.Use(auth.NewVerifier(env("JWT_SECRET", "")).Bearer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	r.Handle("/metrics", metrics.Handler())

	gh := admingame.New(core)
	fh := adminfulfillment.New(core, env("FULFILLMENT_CALLBACK_SECRET", ""), log)
	th := admintenancy.New(core)
	ch := admincampaign.New(core, rmcache)
	qh := adminquest.New(core)
	lh := adminleaderboard.New(core)
	ih := adminintegration.New(core)
	r.Route("/api/v1", func(r chi.Router) {
		// Machine + callback surface (HMAC-verified n8n callback + admin task ops)
		// stays outside the role gate; the callback has no logged-in admin.
		fh.Routes(r)
		// Staff-only management surface: requires an admin/designer/reward_manager
		// role (from the admin JWT, or the X-Roles dev header).
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole("admin", "designer", "reward_manager"))
			gh.Routes(r)
			th.Routes(r)
			ch.Routes(r)
			qh.Routes(r)
			lh.Routes(r)
			ih.Routes(r)
		})
	})

	srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Info("bff-admin listening", "addr", addr, "core", coreAddr)
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
