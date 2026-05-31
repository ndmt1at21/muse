-- +goose Up
ALTER TABLE games ADD COLUMN validator_config JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE games DROP COLUMN validator_config;
