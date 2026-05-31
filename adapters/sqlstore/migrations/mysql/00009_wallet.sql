-- +goose Up
-- Phase 8: wallet ledger + balances + milestone grants + per-game wallet config.
-- (See the postgres migration for design notes.)

ALTER TABLE games ADD COLUMN wallet_scope VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE games ADD COLUMN milestones JSON NOT NULL;

CREATE TABLE wallet_balances (
    tenant_id  VARCHAR(64) NOT NULL,
    scope_key  VARCHAR(64) NOT NULL,
    player_id  VARCHAR(64) NOT NULL,
    currency   VARCHAR(64) NOT NULL,
    balance    BIGINT NOT NULL DEFAULT 0,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (tenant_id, scope_key, player_id, currency)
);

CREATE TABLE wallet_ledger (
    id         VARCHAR(64) PRIMARY KEY,
    tenant_id  VARCHAR(64) NOT NULL,
    scope_key  VARCHAR(64) NOT NULL,
    player_id  VARCHAR(64) NOT NULL,
    currency   VARCHAR(64) NOT NULL,
    amount     BIGINT NOT NULL,
    reason     VARCHAR(32) NOT NULL DEFAULT '',
    ref_id     VARCHAR(64) NOT NULL DEFAULT '',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_wallet_ledger_player (tenant_id, scope_key, player_id, created_at)
);

CREATE TABLE milestone_grants (
    tenant_id    VARCHAR(64) NOT NULL,
    scope_key    VARCHAR(64) NOT NULL,
    player_id    VARCHAR(64) NOT NULL,
    milestone_id VARCHAR(64) NOT NULL,
    created_at   DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (tenant_id, scope_key, player_id, milestone_id)
);

-- +goose Down
DROP TABLE IF EXISTS milestone_grants;
DROP TABLE IF EXISTS wallet_ledger;
DROP TABLE IF EXISTS wallet_balances;
ALTER TABLE games DROP COLUMN milestones;
ALTER TABLE games DROP COLUMN wallet_scope;
