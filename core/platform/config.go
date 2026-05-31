// Package platform holds Core's process-level concerns: configuration, logging,
// and health. Business logic lives in gamekit; wiring lives in cmd/core.
package platform

import (
	"os"
	"time"
)

// Config is the Core service configuration, loaded from the environment.
type Config struct {
	GRPCAddr   string // host:port for the gRPC server
	RESTAddr   string // host:port for the HTTP/REST grpc-gateway (empty disables REST)
	HealthAddr string // host:port for the HTTP health/metrics server
	DBEngine   string // postgres | mysql
	DSN        string // database connection string
	RedisAddr  string // host:port; empty disables Redis (idempotency degrades)
	LogLevel   string // debug | info | warn | error

	// Fulfillment dispatcher / delivery (Phase 3.5).
	FulfillmentEnabled  bool          // run the outbox dispatcher (default true)
	FulfillmentInterval time.Duration // dispatcher poll period (default 2s)
	N8NWebhookURL       string        // default external_workflow webhook (per-prize config overrides)
	N8NHMACSecret       string        // default HMAC secret for outbound n8n POSTs
	CallbackBaseURL     string        // public base url n8n calls back to (e.g. the admin BFF)

	// Player auth / identity (Phase 4).
	JWTSecret   string // HMAC secret for player JWTs (shared with the BFFs)
	AuthDevMode bool   // reveal dev OTP codes in StartAuth responses (local/e2e only)
}

// LoadConfig reads configuration from the environment with sensible defaults.
func LoadConfig() Config {
	return Config{
		GRPCAddr:   env("CORE_GRPC_ADDR", ":9090"),
		RESTAddr:   env("CORE_REST_ADDR", ":8090"),
		HealthAddr: env("CORE_HEALTH_ADDR", ":9091"),
		DBEngine:   env("DB_ENGINE", "postgres"),
		DSN:        env("DB_DSN", "postgres://muse:muse@localhost:5432/muse?sslmode=disable"),
		RedisAddr:  env("REDIS_ADDR", "localhost:6379"),
		LogLevel:   env("LOG_LEVEL", "info"),

		FulfillmentEnabled:  envBool("FULFILLMENT_ENABLED", true),
		FulfillmentInterval: envDuration("FULFILLMENT_INTERVAL", 2*time.Second),
		N8NWebhookURL:       env("N8N_WEBHOOK_URL", ""),
		N8NHMACSecret:       env("N8N_HMAC_SECRET", ""),
		CallbackBaseURL:     env("FULFILLMENT_CALLBACK_BASE", ""),

		JWTSecret:   env("JWT_SECRET", ""),
		AuthDevMode: envBool("AUTH_DEV_MODE", false),
	}
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	switch os.Getenv(key) {
	case "1", "true", "TRUE", "yes":
		return true
	case "0", "false", "FALSE", "no":
		return false
	default:
		return def
	}
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
