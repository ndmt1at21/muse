-- +goose Up
-- MySQL variant: VARCHAR keys, JSON columns, DATETIME(6) for sub-second
-- precision, TINYINT(1) booleans. Same isolation columns as Postgres.

CREATE TABLE games (
    id             VARCHAR(64) PRIMARY KEY,
    tenant_id      VARCHAR(64) NOT NULL,
    merchant_id    VARCHAR(64) NOT NULL DEFAULT '',
    campaign_id    VARCHAR(64) NOT NULL DEFAULT '',
    name           VARCHAR(255) NOT NULL,
    type           VARCHAR(64) NOT NULL,
    seed_generator VARCHAR(64) NOT NULL DEFAULT 'none',
    reward_handler VARCHAR(64) NOT NULL,
    validator      VARCHAR(64) NOT NULL DEFAULT 'basic',
    status         VARCHAR(32) NOT NULL DEFAULT 'draft',
    rules          JSON NOT NULL,
    handler_config JSON NOT NULL,
    ui             JSON NOT NULL,
    created_at     DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at     DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_games_tenant (tenant_id, merchant_id, campaign_id)
);

CREATE TABLE prizes (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64) NOT NULL,
    merchant_id VARCHAR(64) NOT NULL DEFAULT '',
    game_id     VARCHAR(64) NOT NULL DEFAULT '',
    name        VARCHAR(255) NOT NULL,
    type        VARCHAR(64) NOT NULL,
    image       VARCHAR(1024) NOT NULL DEFAULT '',
    value       BIGINT NOT NULL DEFAULT 0,
    total       BIGINT NOT NULL DEFAULT 0,
    remaining   BIGINT NOT NULL DEFAULT 0,
    metadata    JSON NOT NULL,
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_prizes_game (tenant_id, game_id),
    CONSTRAINT chk_prizes_remaining CHECK (remaining >= 0)
);

CREATE TABLE sessions (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64) NOT NULL,
    merchant_id VARCHAR(64) NOT NULL DEFAULT '',
    game_id     VARCHAR(64) NOT NULL,
    player_id   VARCHAR(64) NOT NULL,
    seed_data   JSON,
    secret      VARCHAR(128) NOT NULL DEFAULT '',
    started_at  DATETIME(6) NOT NULL,
    expires_at  DATETIME(6) NOT NULL,
    consumed    TINYINT(1) NOT NULL DEFAULT 0,
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_sessions_player (tenant_id, game_id, player_id)
);

CREATE TABLE play_history (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64) NOT NULL,
    merchant_id VARCHAR(64) NOT NULL DEFAULT '',
    game_id     VARCHAR(64) NOT NULL,
    player_id   VARCHAR(64) NOT NULL,
    session_id  VARCHAR(64) NOT NULL,
    payload     JSON,
    rewards     JSON,
    metadata    JSON,
    trace_id    VARCHAR(64) NOT NULL DEFAULT '',
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_history_player (tenant_id, game_id, player_id, created_at)
);

-- +goose Down
DROP TABLE IF EXISTS play_history;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS prizes;
DROP TABLE IF EXISTS games;
