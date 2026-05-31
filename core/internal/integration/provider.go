// Package integration is Core's outbound integration hub (Phase 10): a Provider
// registry (one impl per integration type, the same pluggable pattern as the
// reward handlers and fulfillment channels) plus a Hub that fans domain events
// out to the integrations subscribed to them. It is hosting-layer (I/O) code, so
// it lives in Core, not in the pure gamekit SDK.
package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/muse/gamekit/ports"
	"github.com/muse/gamekit/types"
)

// Provider delivers one domain event to one configured integration. Impls must
// be safe for concurrent use; delivery is best-effort (the Hub logs failures and
// moves on — an integration is not allowed to fail the originating play/claim).
type Provider interface {
	Deliver(ctx context.Context, integ types.Integration, evt ports.Event) error
}

// Registry maps an integration type to its Provider.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry { return &Registry{providers: map[string]Provider{}} }

// Register adds (or replaces) the provider for a type.
func (r *Registry) Register(typ string, p Provider) { r.providers[typ] = p }

// Get returns the provider for a type.
func (r *Registry) Get(typ string) (Provider, bool) {
	p, ok := r.providers[typ]
	return p, ok
}

// RegisterBuiltins wires the built-in providers: a real HTTP webhook (also used
// for n8n, same POST+HMAC convention) and logging stubs for the channels whose
// real integrations are out of scope for the initial build (gsheet/sms/zns/crm).
func RegisterBuiltins(reg *Registry, client *http.Client, log *slog.Logger) {
	wh := NewWebhookProvider(client, log)
	reg.Register(types.IntegrationWebhook, wh)
	reg.Register(types.IntegrationN8N, wh) // n8n is a webhook receiver
	for _, t := range []string{types.IntegrationGSheet, types.IntegrationSMS, types.IntegrationZNS, types.IntegrationCRM} {
		reg.Register(t, NewStubProvider(t, log))
	}
}

var errNoURL = errors.New("webhook: no url configured")

// WebhookProvider POSTs the event as JSON to a configured URL, optionally
// HMAC-SHA256 signed (X-Muse-Signature) — the same scheme as the fulfillment
// external_workflow hand-off, so an n8n flow verifies events and tasks alike.
type WebhookProvider struct {
	client *http.Client
	log    *slog.Logger
}

// NewWebhookProvider builds the provider. client must be non-nil.
func NewWebhookProvider(client *http.Client, log *slog.Logger) *WebhookProvider {
	return &WebhookProvider{client: client, log: log}
}

type webhookConfig struct {
	URL        string `json:"url"`
	HMACSecret string `json:"hmac_secret"`
}

// eventPayload is the body POSTed to the integration endpoint.
type eventPayload struct {
	Event      string         `json:"event"`
	TenantID   string         `json:"tenant_id"`
	MerchantID string         `json:"merchant_id"`
	Payload    map[string]any `json:"payload"`
}

func (p *WebhookProvider) Deliver(ctx context.Context, integ types.Integration, evt ports.Event) error {
	var cfg webhookConfig
	if len(integ.Config) > 0 {
		_ = json.Unmarshal(integ.Config, &cfg)
	}
	if cfg.URL == "" {
		return errNoURL
	}
	body, err := json.Marshal(eventPayload{
		Event: evt.Type, TenantID: evt.Scope.TenantID, MerchantID: evt.Scope.MerchantID, Payload: evt.Payload,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Muse-Event", evt.Type)
	if cfg.HMACSecret != "" {
		req.Header.Set("X-Muse-Signature", signHMAC(cfg.HMACSecret, body))
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook POST %s: %w", cfg.URL, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook %s returned %d", cfg.URL, resp.StatusCode)
	}
	if p.log != nil {
		p.log.Info("integration webhook delivered", "type", integ.Type, "integration_id", integ.ID,
			"event", evt.Type, "url", cfg.URL, "status", resp.StatusCode)
	}
	return nil
}

// StubProvider logs the delivery and succeeds — a placeholder for channels whose
// real provider integration is out of scope for the initial build (pluggable:
// swap in a real impl by re-registering the type).
type StubProvider struct {
	typ string
	log *slog.Logger
}

// NewStubProvider builds a stub for an integration type.
func NewStubProvider(typ string, log *slog.Logger) *StubProvider {
	return &StubProvider{typ: typ, log: log}
}

func (p *StubProvider) Deliver(_ context.Context, integ types.Integration, evt ports.Event) error {
	if p.log != nil {
		p.log.Info("integration stub delivered (no-op)", "type", p.typ, "integration_id", integ.ID, "event", evt.Type)
	}
	return nil
}

func signHMAC(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
