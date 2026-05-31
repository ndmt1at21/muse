package fulfillment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/muse/gamekit/types"
)

var errNoWebhook = errors.New("external_workflow: no webhook_url configured")

// ExternalWorkflowProvider hands a task off to an external orchestrator (n8n)
// by POSTing it to a per-campaign webhook, HMAC-signed. n8n runs the no-code
// workflow and later calls the signed callback to finalize the task, so this
// provider returns AwaitingCallback on a successful hand-off rather than
// Delivered. The task id is the idempotency key across retries.
type ExternalWorkflowProvider struct {
	client        *http.Client
	defaultURL    string // fallback webhook when channel_config has none
	defaultSecret string // fallback signing secret
	callbackBase  string // public base url n8n should call back to (optional)
	log           *slog.Logger
}

// NewExternalWorkflowProvider builds the provider. client must be non-nil.
func NewExternalWorkflowProvider(client *http.Client, defaultURL, defaultSecret, callbackBase string, log *slog.Logger) *ExternalWorkflowProvider {
	return &ExternalWorkflowProvider{
		client: client, defaultURL: defaultURL, defaultSecret: defaultSecret,
		callbackBase: callbackBase, log: log,
	}
}

type externalConfig struct {
	WebhookURL    string `json:"webhook_url"`
	HMACSecret    string `json:"hmac_secret"`
	HMACSecretRef string `json:"hmac_secret_ref"` // opaque; resolution is a deploy concern
}

// webhookPayload is the body POSTed to n8n.
type webhookPayload struct {
	TaskID        string          `json:"task_id"`
	TenantID      string          `json:"tenant_id"`
	MerchantID    string          `json:"merchant_id"`
	RewardID      string          `json:"reward_id"`
	PrizeID       string          `json:"prize_id"`
	PlayerID      string          `json:"player_id"`
	GameID        string          `json:"game_id"`
	CampaignID    string          `json:"campaign_id"`
	Channel       string          `json:"channel"`
	ChannelConfig json.RawMessage `json:"channel_config,omitempty"`
	CallbackURL   string          `json:"callback_url,omitempty"`
}

func (p *ExternalWorkflowProvider) Deliver(ctx context.Context, task *types.FulfillmentTask) Result {
	var cfg externalConfig
	if len(task.ChannelConfig) > 0 {
		_ = json.Unmarshal(task.ChannelConfig, &cfg)
	}
	url := cfg.WebhookURL
	if url == "" {
		url = p.defaultURL
	}
	if url == "" {
		return Permanent(errNoWebhook)
	}
	secret := cfg.HMACSecret
	if secret == "" {
		secret = p.defaultSecret
	}

	payload := webhookPayload{
		TaskID: task.ID, TenantID: task.Scope.TenantID, MerchantID: task.Scope.MerchantID,
		RewardID: task.RewardID, PrizeID: task.PrizeID, PlayerID: task.PlayerID,
		GameID: task.GameID, CampaignID: task.CampaignID, Channel: task.Channel,
		ChannelConfig: task.ChannelConfig,
	}
	if p.callbackBase != "" {
		payload.CallbackURL = strings.TrimRight(p.callbackBase, "/") + "/api/v1/fulfillment/tasks/" + task.ID + "/callback"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Permanent(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Permanent(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Muse-Task-Id", task.ID)
	if secret != "" {
		req.Header.Set("X-Muse-Signature", SignHMAC(secret, body))
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return Retry(fmt.Errorf("external_workflow POST %s: %w", url, err))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if p.log != nil {
			p.log.Info("external_workflow handed off", "task_id", task.ID, "url", url, "status", resp.StatusCode)
		}
		return Awaiting(mustJSON(map[string]any{"dispatched_to": url, "http_status": resp.StatusCode}))
	case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return Retry(fmt.Errorf("external_workflow %s returned %d", url, resp.StatusCode))
	default:
		// 4xx (other than 408/429): a client/config error retrying won't fix.
		return Permanent(fmt.Errorf("external_workflow %s returned %d", url, resp.StatusCode))
	}
}
