-- +goose Up
ALTER TABLE prizes ADD COLUMN award_constraints JSON NOT NULL DEFAULT (JSON_OBJECT());
ALTER TABLE prizes ADD COLUMN fulfillment JSON NOT NULL DEFAULT (JSON_OBJECT());

CREATE TABLE rewards (
    id           VARCHAR(64) PRIMARY KEY,
    tenant_id    VARCHAR(64) NOT NULL,
    merchant_id  VARCHAR(64) NOT NULL DEFAULT '',
    game_id      VARCHAR(64) NOT NULL DEFAULT '',
    player_id    VARCHAR(64) NOT NULL,
    prize_id     VARCHAR(64) NOT NULL,
    play_id      VARCHAR(64) NOT NULL DEFAULT '',
    name         VARCHAR(255) NOT NULL DEFAULT '',
    type         VARCHAR(64) NOT NULL DEFAULT '',
    value        BIGINT NOT NULL DEFAULT 0,
    code         VARCHAR(255) NOT NULL DEFAULT '',
    status       VARCHAR(32) NOT NULL DEFAULT 'won',
    metadata     JSON NOT NULL,
    created_at   DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    claimed_at   DATETIME(6) NULL,
    fulfilled_at DATETIME(6) NULL,
    revoked_at   DATETIME(6) NULL,
    INDEX idx_rewards_player (tenant_id, player_id, created_at),
    INDEX idx_rewards_prize_player (tenant_id, prize_id, player_id)
);

CREATE TABLE prize_codes (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64) NOT NULL,
    merchant_id VARCHAR(64) NOT NULL DEFAULT '',
    prize_id    VARCHAR(64) NOT NULL,
    code        VARCHAR(255) NOT NULL,
    status      VARCHAR(32) NOT NULL DEFAULT 'available',
    reward_id   VARCHAR(64) NOT NULL DEFAULT '',
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    assigned_at DATETIME(6) NULL,
    INDEX idx_prize_codes_avail (tenant_id, prize_id, status),
    UNIQUE KEY uq_prize_codes_code (tenant_id, prize_id, code)
);

-- +goose Down
DROP TABLE IF EXISTS prize_codes;
DROP TABLE IF EXISTS rewards;
ALTER TABLE prizes DROP COLUMN fulfillment;
ALTER TABLE prizes DROP COLUMN award_constraints;
