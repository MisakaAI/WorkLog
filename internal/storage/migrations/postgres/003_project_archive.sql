-- +goose Up
ALTER TABLE projects ADD COLUMN archived BOOLEAN NOT NULL DEFAULT FALSE;
-- +goose Down
ALTER TABLE projects DROP COLUMN archived;
