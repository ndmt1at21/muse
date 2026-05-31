-- +goose Up
-- Phase 8: wallet ledger + balances + milestone grants, and the per-game wallet
-- config (scope + milestones). Balances are keyed by (tenant, scope_key,
-- currency) where scope_key is the campaign/merchant/tenant per wallet_scope.

ALTER TABLE games ADD COLUMN wallet_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE games ADD COLUMN milestones JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE wallet_balances (
    tenant_id  TEXT NOT NULL,
    scope_key  TEXT NOT NULL,
    player_id  TEXT NOT NULL,
    currency   TEXT NOT NULL,
    balance    BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, scope_key, player_id, currency),
    CONSTRAINT chk_wallet_nonneg CHECK (balance >= 0)
);

CREATE TABLE wallet_ledger (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL,
    scope_key  TEXT NOT NULL,
    player_id  TEXT NOT NULL,
    currency   TEXT NOT NULL,
    amount     BIGINT NOT NULL,           -- signed: + earn, - spend
    reason     TEXT NOT NULL DEFAULT '',  -- play | redeem | adjust
    ref_id     TEXT NOT NULL DEFAULT '',  -- play_id | milestone_id
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_wallet_ledger_player ON wallet_ledger (tenant_id, scope_key, player_id, created_at DESC);

CREATE TABLE milestone_grants (
    tenant_id    TEXT NOT NULL,
    scope_key    TEXT NOT NULL,
    player_id    TEXT NOT NULL,
    milestone_id TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, scope_key, player_id, milestone_id)
);

-- +goose Down
DROP TABLE IF EXISTS milestone_grants;
DROP TABLE IF EXISTS wallet_ledger;
DROP TABLE IF EXISTS wallet_balances;
ALTER TABLE games DROP COLUMN milestones;
ALTER TABLE games DROP COLUMN wallet_scope;
