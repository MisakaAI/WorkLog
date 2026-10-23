-- +goose Up
-- Keep tombstones to prevent delete/recreate ABA version resets.
CREATE TABLE resource_versions (
 user_id TEXT NOT NULL REFERENCES workspaces(user_id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('task','todo')),
 id TEXT NOT NULL CHECK(length(id)>0),
 version BIGINT NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 PRIMARY KEY(user_id,kind,id)
);
INSERT INTO resource_versions SELECT user_id,'task',id,version FROM tasks;
INSERT INTO resource_versions SELECT user_id,'todo',id,version FROM todos;
-- +goose Down
DROP TABLE resource_versions;
