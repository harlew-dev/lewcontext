CREATE TABLE workspaces (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 public_id text NOT NULL UNIQUE DEFAULT public_id('wsp') CHECK (public_id ~ '^wsp_[0-9A-Za-z]{14}$'),
 name text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE workspace_user (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 workspace_id bigint NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 role text NOT NULL CHECK (role IN ('owner','admin','member')),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (workspace_id, user_id)
);
CREATE INDEX workspace_user_user_id_idx ON workspace_user(user_id);
