-- +goose Up
-- Phase 7: leaderboards (config) + durable entries.
-- (See the postgres migration for design notes.)

CREATE TABLE leaderboards (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64) NOT NULL,
    merchant_id VARCHAR(64) NOT NULL,
    campaign_id VARCHAR(64) NOT NULL DEFAULT '',
    name        VARCHAR(255) NOT NULL DEFAULT '',
    metric      VARCHAR(32) NOT NULL,
    time_window JSON NOT NULL,
    prize_tiers JSON NOT NULL,
    anti_cheat  JSON NOT NULL,
    status      VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_leaderboards_campaign (tenant_id, merchant_id, campaign_id, created_at)
);

CREATE TABLE leaderboard_entries (
    tenant_id      VARCHAR(64) NOT NULL,
    leaderboard_id VARCHAR(64) NOT NULL,
    window_key     VARCHAR(64) NOT NULL,
    player_id      VARCHAR(64) NOT NULL,
    score          BIGINT NOT NULL DEFAULT 0,
    plays          BIGINT NOT NULL DEFAULT 0,
    state          VARCHAR(32) NOT NULL DEFAULT 'active',
    updated_at     DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (leaderboard_id, window_key, player_id),
    INDEX idx_lb_entries_rank (tenant_id, leaderboard_id, window_key, score)
);

-- +goose Down
DROP TABLE IF EXISTS leaderboard_entries;
DROP TABLE IF EXISTS leaderboards;
