-- +goose Up
-- Phase 10: outbound integrations fired on domain events. Tenant/merchant-scoped
-- (optionally narrowed to one campaign); `events` is the subscribed event-type
-- list, `config` an opaque adapter-specific JSON blob (webhook url + secret, …).

CREATE TABLE integrations (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    merchant_id TEXT NOT NULL,
    campaign_id TEXT NOT NULL DEFAULT '', -- '' = all campaigns in scope
    type        TEXT NOT NULL,            -- webhook | gsheet | sms | zns | crm | n8n
    events      JSONB NOT NULL DEFAULT '[]'::jsonb, -- subscribed event types
    config      JSONB NOT NULL DEFAULT '{}'::jsonb, -- adapter-specific
    status      TEXT NOT NULL DEFAULT 'active',     -- active | paused
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_integrations_scope ON integrations (tenant_id, merchant_id, campaign_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS integrations;
