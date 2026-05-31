-- +goose Up
-- Phase 6: quests + per-player completion records.
-- (See the postgres migration for design notes.)

CREATE TABLE quests (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64) NOT NULL,
    merchant_id VARCHAR(64) NOT NULL,
    campaign_id VARCHAR(64) NOT NULL DEFAULT '',
    type        VARCHAR(32) NOT NULL,
    name        VARCHAR(255) NOT NULL DEFAULT '',
    status      VARCHAR(32) NOT NULL DEFAULT 'active',
    reward      JSON NOT NULL,
    config      JSON NOT NULL,
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_quests_campaign (tenant_id, merchant_id, campaign_id, created_at)
);

CREATE TABLE quest_completions (
    id         VARCHAR(64) PRIMARY KEY,
    tenant_id  VARCHAR(64) NOT NULL,
    quest_id   VARCHAR(64) NOT NULL,
    player_id  VARCHAR(64) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_quest_completions (tenant_id, quest_id, player_id, created_at)
);

-- +goose Down
DROP TABLE IF EXISTS quest_completions;
DROP TABLE IF EXISTS quests;
