-- +goose Up
CREATE TABLE workspaces (
 user_id TEXT PRIMARY KEY CHECK(length(user_id)>0),
 revision BIGINT NOT NULL DEFAULT 0 CHECK(revision BETWEEN 0 AND 9007199254740991),
 project_revision BIGINT NOT NULL DEFAULT 0 CHECK(project_revision BETWEEN 0 AND 9007199254740991),
 category_revision BIGINT NOT NULL DEFAULT 0 CHECK(category_revision BETWEEN 0 AND 9007199254740991),
 personal_label TEXT NOT NULL DEFAULT '个人工作台'
);
CREATE TABLE projects (
 user_id TEXT NOT NULL REFERENCES workspaces(user_id) ON DELETE CASCADE,
 id TEXT NOT NULL CHECK(length(id)>0 AND id<>'__all__'),
 name TEXT NOT NULL CHECK(length(trim(name))>0), name_key TEXT NOT NULL,
 position INTEGER NOT NULL CHECK(position>=0),
 PRIMARY KEY(user_id,id), UNIQUE(user_id,name_key), UNIQUE(user_id,position)
);
CREATE TABLE categories (
 user_id TEXT NOT NULL REFERENCES workspaces(user_id) ON DELETE CASCADE,
 name TEXT NOT NULL CHECK(length(trim(name))>0), name_key TEXT NOT NULL,
 position INTEGER NOT NULL CHECK(position>=0),
 PRIMARY KEY(user_id,name), UNIQUE(user_id,name_key), UNIQUE(user_id,position)
);
CREATE TABLE tasks (
 user_id TEXT NOT NULL REFERENCES workspaces(user_id) ON DELETE CASCADE,
 id TEXT NOT NULL CHECK(length(id)>0), id_json JSON NOT NULL,
 title TEXT NOT NULL CHECK(length(trim(title))>0), description TEXT NOT NULL,
 project_id TEXT,
 status TEXT NOT NULL CHECK(status IN ('planned','progress','done')),
 tag TEXT NOT NULL,
 priority TEXT NOT NULL CHECK(priority IN ('低','中','高','极高')),
 start_date TEXT NOT NULL CHECK(start_date='' OR (start_date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' AND to_char(start_date::date,'YYYY-MM-DD')=start_date)),
 due_date TEXT NOT NULL CHECK(due_date='' OR (due_date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' AND to_char(due_date::date,'YYYY-MM-DD')=due_date)),
 color TEXT NOT NULL CHECK(color IN ('white','red','orange','blue','green')),
 version BIGINT NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 position INTEGER NOT NULL CHECK(position>=0),
 PRIMARY KEY(user_id,id), UNIQUE(user_id,position),
 FOREIGN KEY(user_id,project_id) REFERENCES projects(user_id,id),
 FOREIGN KEY(user_id,tag) REFERENCES categories(user_id,name),
 CHECK(start_date='' OR due_date='' OR start_date<=due_date)
);
CREATE INDEX tasks_project_status ON tasks(user_id,project_id,status);
CREATE INDEX tasks_dates ON tasks(user_id,due_date,start_date);
CREATE TABLE todos (
 user_id TEXT NOT NULL REFERENCES workspaces(user_id) ON DELETE CASCADE,
 id TEXT NOT NULL CHECK(length(id)>0), id_json JSON NOT NULL,
 title TEXT NOT NULL CHECK(length(trim(title))>0),
 done BOOLEAN NOT NULL,
 version BIGINT NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 position INTEGER NOT NULL CHECK(position>=0),
 PRIMARY KEY(user_id,id), UNIQUE(user_id,position)
);
CREATE TABLE worklogs (
 user_id TEXT NOT NULL REFERENCES workspaces(user_id) ON DELETE CASCADE,
 date TEXT NOT NULL CHECK(date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' AND to_char(date::date,'YYYY-MM-DD')=date),
 content JSON NOT NULL CHECK(COALESCE(content->>'type'='doc',FALSE)),
 has_content BOOLEAN NOT NULL,
 version BIGINT NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 updated_at TEXT NOT NULL,
 PRIMARY KEY(user_id,date)
);
CREATE INDEX worklogs_content_date ON worklogs(user_id,has_content,date);
CREATE TABLE idempotency (
 user_id TEXT NOT NULL REFERENCES workspaces(user_id) ON DELETE CASCADE,
 key TEXT NOT NULL CHECK(length(key) BETWEEN 1 AND 200),
 fingerprint TEXT NOT NULL, response JSON NOT NULL,
 PRIMARY KEY(user_id,key)
);
-- +goose Down
DROP TABLE idempotency;
DROP TABLE worklogs;
DROP TABLE todos;
DROP TABLE tasks;
DROP TABLE categories;
DROP TABLE projects;
DROP TABLE workspaces;
