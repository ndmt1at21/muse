-- +goose Up
-- Phase 4: tenancy hierarchy + global identity + tenant-scoped players.

CREATE TABLE tenants (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    plan       TEXT NOT NULL DEFAULT '',
    settings   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE merchants (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL,
    name       TEXT NOT NULL,
    logo       TEXT NOT NULL DEFAULT '',
    settings   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_merchants_tenant ON merchants (tenant_id, created_at);

-- Global person. Internal infra (dedup/anti-fraud); never tenant-scoped.
CREATE TABLE identities (
    id         TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Verified contacts. Each (type, value) is GLOBALLY unique → resolves to one
-- identity. This unique index is the hard dedup invariant.
CREATE TABLE identity_contacts (
    identity_id   TEXT NOT NULL REFERENCES identities(id) ON DELETE CASCADE,
    contact_type  TEXT NOT NULL,           -- phone | email
    contact_value TEXT NOT NULL,           -- normalized (E.164-ish / lowercased)
    verified      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (contact_type, contact_value)
);
CREATE INDEX idx_contacts_identity ON identity_contacts (identity_id);

-- Tenant-scoped membership: one identity participating in one tenant.
CREATE TABLE players (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    merchant_id TEXT NOT NULL DEFAULT '',
    identity_id TEXT NOT NULL,
    profile     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_players_tenant_identity UNIQUE (tenant_id, identity_id)
);
CREATE INDEX idx_players_tenant ON players (tenant_id, created_at);

-- Per-scope turn balances (campaign by default). Phase 6 reads these in
-- eligibility; Phase 4 lands the storage + grant/consume primitives.
CREATE TABLE turn_balances (
    tenant_id  TEXT NOT NULL,
    player_id  TEXT NOT NULL,
    scope_key  TEXT NOT NULL,             -- campaign_id | merchant_id | tenant_id
    balance    BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, player_id, scope_key),
    CONSTRAINT chk_turns_nonneg CHECK (balance >= 0)
);

-- Pending auth challenges (code/otp/magic_link/social). Short-lived, single-use.
-- Durable here (Redis is optional in this stack); the dispatcher of stale rows
-- is just the expiry check at verify time.
CREATE TABLE auth_challenges (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    merchant_id   TEXT NOT NULL DEFAULT '',
    contact_type  TEXT NOT NULL,
    contact_value TEXT NOT NULL,   -- normalized
    method        TEXT NOT NULL,   -- code | otp | magic_link | social
    secret        TEXT NOT NULL,   -- expected code/token (server-issued)
    campaign_id   TEXT NOT NULL DEFAULT '',
    consumed      BOOLEAN NOT NULL DEFAULT FALSE,
    attempts      INT NOT NULL DEFAULT 0,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_challenges_expiry ON auth_challenges (expires_at);

-- +goose Down
DROP TABLE IF EXISTS auth_challenges;
DROP TABLE IF EXISTS turn_balances;
DROP TABLE IF EXISTS players;
DROP TABLE IF EXISTS identity_contacts;
DROP TABLE IF EXISTS identities;
DROP TABLE IF EXISTS merchants;
DROP TABLE IF EXISTS tenants;
