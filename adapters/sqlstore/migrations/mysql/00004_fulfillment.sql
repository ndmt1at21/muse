-- +goose Up
-- Transactional outbox for prize delivery (see the postgres migration for the
-- design notes). MySQL has no partial indexes, so the dispatcher's "due" query
-- filters on (status, next_attempt_at) with a composite index.
CREATE TABLE fulfillment_tasks (
    id              VARCHAR(64) PRIMARY KEY,
    tenant_id       VARCHAR(64) NOT NULL,
    merchant_id     VARCHAR(64) NOT NULL DEFAULT '',
    reward_id       VARCHAR(64) NOT NULL DEFAULT '',
    prize_id        VARCHAR(64) NOT NULL DEFAULT '',
    player_id       VARCHAR(64) NOT NULL DEFAULT '',
    game_id         VARCHAR(64) NOT NULL DEFAULT '',
    campaign_id     VARCHAR(64) NOT NULL DEFAULT '',
    channel         VARCHAR(64) NOT NULL,
    channel_config  JSON NOT NULL,
    status          VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempts        INT NOT NULL DEFAULT 0,
    max_attempts    INT NOT NULL DEFAULT 0,
    last_error      TEXT NULL,
    receipt         JSON NOT NULL,
    next_attempt_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    created_at      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_ftasks_due (status, next_attempt_at),
    INDEX idx_ftasks_admin (tenant_id, status, created_at),
    INDEX idx_ftasks_campaign (tenant_id, campaign_id),
    INDEX idx_ftasks_prize (tenant_id, prize_id)
);

-- +goose Down
DROP TABLE IF EXISTS fulfillment_tasks;
