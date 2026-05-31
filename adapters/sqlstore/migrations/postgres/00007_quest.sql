-- +goose Up
-- Phase 6: quests under a campaign + per-player completion records. Quests are
-- campaign-scoped (tenant_id + merchant_id); reward/config are opaque JSON.

CREATE TABLE quests (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    merchant_id TEXT NOT NULL,
    campaign_id TEXT NOT NULL DEFAULT '',
    type        TEXT NOT NULL, -- daily_checkin | share_social | invite_friend | scan_qr | view_page | answer_question | external_event
    name        TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'active', -- active | inactive
    reward      JSONB NOT NULL DEFAULT '{}'::jsonb, -- {type, quantity}
    config      JSONB NOT NULL DEFAULT '{}'::jsonb, -- verifier-specific
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_quests_campaign ON quests (tenant_id, merchant_id, campaign_id, created_at);

-- Immutable completion records gate re-completion (daily_checkin: one per day;
-- other types: one per player).
CREATE TABLE quest_completions (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL,
    quest_id   TEXT NOT NULL,
    player_id  TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_quest_completions ON quest_completions (tenant_id, quest_id, player_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS quest_completions;
DROP TABLE IF EXISTS quests;
