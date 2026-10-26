-- +goose Up
ALTER TABLE projects ADD COLUMN archived BOOLEAN NOT NULL DEFAULT FALSE CHECK (archived IN (0,1));
-- +goose Down
ALTER TABLE projects DROP COLUMN archived;
