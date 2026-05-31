-- +goose Up
-- Phase 4: tenancy hierarchy + global identity + tenant-scoped players.
-- (See the postgres migration for design notes.)

CREATE TABLE tenants (
    id         VARCHAR(64) PRIMARY KEY,
    name       VARCHAR(255) NOT NULL,
    plan       VARCHAR(64) NOT NULL DEFAULT '',
    settings   JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);

CREATE TABLE merchants (
    id         VARCHAR(64) PRIMARY KEY,
    tenant_id  VARCHAR(64) NOT NULL,
    name       VARCHAR(255) NOT NULL,
    logo       VARCHAR(1024) NOT NULL DEFAULT '',
    settings   JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_merchants_tenant (tenant_id, created_at)
);

CREATE TABLE identities (
    id         VARCHAR(64) PRIMARY KEY,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);

CREATE TABLE identity_contacts (
    identity_id   VARCHAR(64) NOT NULL,
    contact_type  VARCHAR(16) NOT NULL,
    contact_value VARCHAR(320) NOT NULL,
    verified      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (contact_type, contact_value),
    INDEX idx_contacts_identity (identity_id),
    CONSTRAINT fk_contacts_identity FOREIGN KEY (identity_id) REFERENCES identities(id) ON DELETE CASCADE
);

CREATE TABLE players (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   VARCHAR(64) NOT NULL,
    merchant_id VARCHAR(64) NOT NULL DEFAULT '',
    identity_id VARCHAR(64) NOT NULL,
    profile     JSON NOT NULL,
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT uq_players_tenant_identity UNIQUE (tenant_id, identity_id),
    INDEX idx_players_tenant (tenant_id, created_at)
);

CREATE TABLE turn_balances (
    tenant_id  VARCHAR(64) NOT NULL,
    player_id  VARCHAR(64) NOT NULL,
    scope_key  VARCHAR(64) NOT NULL,
    balance    BIGINT NOT NULL DEFAULT 0,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (tenant_id, player_id, scope_key),
    CONSTRAINT chk_turns_nonneg CHECK (balance >= 0)
);

CREATE TABLE auth_challenges (
    id            VARCHAR(64) PRIMARY KEY,
    tenant_id     VARCHAR(64) NOT NULL,
    merchant_id   VARCHAR(64) NOT NULL DEFAULT '',
    contact_type  VARCHAR(16) NOT NULL,
    contact_value VARCHAR(320) NOT NULL,
    method        VARCHAR(32) NOT NULL,
    secret        VARCHAR(255) NOT NULL,
    campaign_id   VARCHAR(64) NOT NULL DEFAULT '',
    consumed      BOOLEAN NOT NULL DEFAULT FALSE,
    attempts      INT NOT NULL DEFAULT 0,
    expires_at    DATETIME(6) NOT NULL,
    created_at    DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_challenges_expiry (expires_at)
);

-- +goose Down
DROP TABLE IF EXISTS auth_challenges;
DROP TABLE IF EXISTS turn_balances;
DROP TABLE IF EXISTS players;
DROP TABLE IF EXISTS identity_contacts;
DROP TABLE IF EXISTS identities;
DROP TABLE IF EXISTS merchants;
DROP TABLE IF EXISTS tenants;
