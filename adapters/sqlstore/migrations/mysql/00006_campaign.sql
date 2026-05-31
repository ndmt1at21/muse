-- +goose Up
-- Phase 5: campaigns — container aggregate under a merchant.
-- (See the postgres migration for design notes.)

CREATE TABLE campaigns (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64) NOT NULL,
    merchant_id VARCHAR(64) NOT NULL,
    name        VARCHAR(255) NOT NULL,
    status      VARCHAR(32) NOT NULL DEFAULT 'draft',
    start_date  DATETIME(6) NULL,
    end_date    DATETIME(6) NULL,
    channels    JSON NOT NULL,
    games       JSON NOT NULL,
    quests      JSON NOT NULL,
    settings    JSON NOT NULL,
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_campaigns_merchant (tenant_id, merchant_id, created_at)
);

-- +goose Down
DROP TABLE IF EXISTS campaigns;
