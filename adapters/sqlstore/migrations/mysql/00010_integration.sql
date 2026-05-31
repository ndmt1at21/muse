-- +goose Up
-- Phase 10: outbound integrations fired on domain events.
-- (See the postgres migration for design notes.)

CREATE TABLE integrations (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64) NOT NULL,
    merchant_id VARCHAR(64) NOT NULL,
    campaign_id VARCHAR(64) NOT NULL DEFAULT '',
    type        VARCHAR(32) NOT NULL,
    events      JSON NOT NULL,
    config      JSON NOT NULL,
    status      VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_integrations_scope (tenant_id, merchant_id, campaign_id, created_at)
);

-- +goose Down
DROP TABLE IF EXISTS integrations;
