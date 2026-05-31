-- +goose Up
ALTER TABLE prizes ADD COLUMN award_constraints JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE prizes ADD COLUMN fulfillment JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE rewards (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL,
    merchant_id  TEXT NOT NULL DEFAULT '',
    game_id      TEXT NOT NULL DEFAULT '',
    player_id    TEXT NOT NULL,
    prize_id     TEXT NOT NULL,
    play_id      TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL DEFAULT '',
    type         TEXT NOT NULL DEFAULT '',
    value        BIGINT NOT NULL DEFAULT 0,
    code         TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'won',
    metadata     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at   TIMESTAMPTZ,
    fulfilled_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX idx_rewards_player ON rewards (tenant_id, player_id, created_at DESC);
CREATE INDEX idx_rewards_prize_player ON rewards (tenant_id, prize_id, player_id);

CREATE TABLE prize_codes (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    merchant_id TEXT NOT NULL DEFAULT '',
    prize_id    TEXT NOT NULL,
    code        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'available', -- available | assigned
    reward_id   TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    assigned_at TIMESTAMPTZ
);
-- Partial index makes "next available code" lookups cheap and SKIP LOCKED-friendly.
CREATE INDEX idx_prize_codes_avail ON prize_codes (tenant_id, prize_id) WHERE status = 'available';
CREATE UNIQUE INDEX uq_prize_codes_code ON prize_codes (tenant_id, prize_id, code);

-- +goose Down
DROP TABLE IF EXISTS prize_codes;
DROP TABLE IF EXISTS rewards;
ALTER TABLE prizes DROP COLUMN fulfillment;
ALTER TABLE prizes DROP COLUMN award_constraints;
