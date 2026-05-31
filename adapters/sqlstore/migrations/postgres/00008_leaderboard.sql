-- +goose Up
-- Phase 7: leaderboards (config) + durable per-window per-player entries. The
-- Redis sorted set is a derived real-time mirror; this table is the truth.

CREATE TABLE leaderboards (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    merchant_id TEXT NOT NULL,
    campaign_id TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL DEFAULT '',
    metric      TEXT NOT NULL, -- high_score | total_score | total_wins | total_plays | quest_points | collection_count
    time_window JSONB NOT NULL DEFAULT '{}'::jsonb, -- {type, period, start, end}
    prize_tiers JSONB NOT NULL DEFAULT '[]'::jsonb, -- [{from_rank, to_rank, prize_id}]
    anti_cheat  JSONB NOT NULL DEFAULT '{}'::jsonb, -- {score_ceiling, flag_outliers}
    status      TEXT NOT NULL DEFAULT 'active',     -- active | finalized
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_leaderboards_campaign ON leaderboards (tenant_id, merchant_id, campaign_id, created_at);

CREATE TABLE leaderboard_entries (
    tenant_id      TEXT NOT NULL,
    leaderboard_id TEXT NOT NULL,
    window_key     TEXT NOT NULL,
    player_id      TEXT NOT NULL,
    score          BIGINT NOT NULL DEFAULT 0,
    plays          BIGINT NOT NULL DEFAULT 0,
    state          TEXT NOT NULL DEFAULT 'active', -- active | flagged | disqualified
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (leaderboard_id, window_key, player_id)
);
-- Ranking scan: a window ordered by score desc.
CREATE INDEX idx_lb_entries_rank ON leaderboard_entries (tenant_id, leaderboard_id, window_key, score DESC);

-- +goose Down
DROP TABLE IF EXISTS leaderboard_entries;
DROP TABLE IF EXISTS leaderboards;
