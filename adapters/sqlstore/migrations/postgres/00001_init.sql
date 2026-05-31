-- +goose Up
-- Every row carries tenant_id (+ merchant_id for campaign-scoped rows): the
-- hard isolation boundary enforced by every query in the adapter.

CREATE TABLE games (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL,
    merchant_id    TEXT NOT NULL DEFAULT '',
    campaign_id    TEXT NOT NULL DEFAULT '',
    name           TEXT NOT NULL,
    type           TEXT NOT NULL,
    seed_generator TEXT NOT NULL DEFAULT 'none',
    reward_handler TEXT NOT NULL,
    validator      TEXT NOT NULL DEFAULT 'basic',
    status         TEXT NOT NULL DEFAULT 'draft',
    rules          JSONB NOT NULL DEFAULT '{}'::jsonb,
    handler_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    ui             JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_games_tenant ON games (tenant_id, merchant_id, campaign_id);

CREATE TABLE prizes (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    merchant_id TEXT NOT NULL DEFAULT '',
    game_id     TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL,
    type        TEXT NOT NULL,
    image       TEXT NOT NULL DEFAULT '',
    value       BIGINT NOT NULL DEFAULT 0,
    total       BIGINT NOT NULL DEFAULT 0,
    remaining   BIGINT NOT NULL DEFAULT 0,
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_prizes_remaining CHECK (remaining >= 0)
);
CREATE INDEX idx_prizes_game ON prizes (tenant_id, game_id);

CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    merchant_id TEXT NOT NULL DEFAULT '',
    game_id     TEXT NOT NULL,
    player_id   TEXT NOT NULL,
    seed_data   JSONB,
    secret      TEXT NOT NULL DEFAULT '',
    started_at  TIMESTAMPTZ NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_sessions_player ON sessions (tenant_id, game_id, player_id);

CREATE TABLE play_history (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    merchant_id TEXT NOT NULL DEFAULT '',
    game_id     TEXT NOT NULL,
    player_id   TEXT NOT NULL,
    session_id  TEXT NOT NULL,
    payload     JSONB,
    rewards     JSONB,
    metadata    JSONB,
    trace_id    TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_history_player ON play_history (tenant_id, game_id, player_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS play_history;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS prizes;
DROP TABLE IF EXISTS games;
