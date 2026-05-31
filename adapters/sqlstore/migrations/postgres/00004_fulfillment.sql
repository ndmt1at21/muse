-- +goose Up
-- Transactional outbox for prize delivery. A task is written in the SAME txn as
-- the reward + stock deduction (instant) or at claim time (on_claim), so a win
-- is never lost or double-delivered. A dispatcher worker drains pending tasks
-- and invokes the channel provider with retry/backoff and a dead-letter state.
CREATE TABLE fulfillment_tasks (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    merchant_id     TEXT NOT NULL DEFAULT '',
    reward_id       TEXT NOT NULL DEFAULT '',
    prize_id        TEXT NOT NULL DEFAULT '',
    player_id       TEXT NOT NULL DEFAULT '',
    game_id         TEXT NOT NULL DEFAULT '',
    campaign_id     TEXT NOT NULL DEFAULT '',
    channel         TEXT NOT NULL,
    channel_config  JSONB NOT NULL DEFAULT '{}'::jsonb,
    status          TEXT NOT NULL DEFAULT 'pending', -- pending|processing|fulfilled|failed|dead
    attempts        INT NOT NULL DEFAULT 0,
    max_attempts    INT NOT NULL DEFAULT 0,           -- 0 = dispatcher default
    last_error      TEXT NOT NULL DEFAULT '',
    receipt         JSONB NOT NULL DEFAULT '{}'::jsonb,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- The dispatcher's hot path: "due, runnable tasks". Partial index keeps it tiny
-- as fulfilled/dead rows accumulate.
CREATE INDEX idx_ftasks_due ON fulfillment_tasks (next_attempt_at)
    WHERE status IN ('pending', 'processing');
-- Admin filtering by status / campaign / prize.
CREATE INDEX idx_ftasks_admin ON fulfillment_tasks (tenant_id, status, created_at DESC);
CREATE INDEX idx_ftasks_campaign ON fulfillment_tasks (tenant_id, campaign_id);
CREATE INDEX idx_ftasks_prize ON fulfillment_tasks (tenant_id, prize_id);

-- +goose Down
DROP TABLE IF EXISTS fulfillment_tasks;
