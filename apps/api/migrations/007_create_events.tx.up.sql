CREATE TABLE events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 public_id text NOT NULL UNIQUE DEFAULT public_id('evt') CHECK (public_id ~ '^evt_[0-9A-Za-z]{14}$'),
 name text NOT NULL CHECK (name ~ '^[a-z][a-z_]*(\.[a-z][a-z_]*){1,2}$'),
 actor_id bigint REFERENCES users(id) ON DELETE SET NULL,
 user_id bigint REFERENCES users(id) ON DELETE CASCADE,
 workspace_id bigint REFERENCES workspaces(id) ON DELETE SET NULL,
 payload jsonb NOT NULL,
 user_agent text NOT NULL DEFAULT '' CHECK (length(user_agent) <= 256),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX events_user_created_idx ON events(user_id, created_at DESC);
CREATE INDEX events_workspace_created_idx ON events(workspace_id, created_at DESC);
CREATE INDEX events_name_created_idx ON events(name, created_at DESC);
CREATE INDEX events_created_idx ON events(created_at);
