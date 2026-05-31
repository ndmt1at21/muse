-- +goose Up
-- Phase 5: campaigns — the container aggregate under a merchant. Campaign-scoped
-- (tenant_id + merchant_id). channels/games/quests are JSON arrays of ids;
-- settings holds the widget/eligibility knobs.

CREATE TABLE campaigns (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    merchant_id TEXT NOT NULL,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'draft', -- draft | active | paused | ended
    start_date  TIMESTAMPTZ,
    end_date    TIMESTAMPTZ,
    channels    JSONB NOT NULL DEFAULT '[]'::jsonb,
    games       JSONB NOT NULL DEFAULT '[]'::jsonb,
    quests      JSONB NOT NULL DEFAULT '[]'::jsonb,
    settings    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_campaigns_merchant ON campaigns (tenant_id, merchant_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS campaigns;
