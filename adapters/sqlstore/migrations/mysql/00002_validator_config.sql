-- +goose Up
ALTER TABLE games ADD COLUMN validator_config JSON NOT NULL DEFAULT (JSON_OBJECT());

-- +goose Down
ALTER TABLE games DROP COLUMN validator_config;
