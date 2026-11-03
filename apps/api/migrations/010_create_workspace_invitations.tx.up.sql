CREATE TABLE workspace_invitations (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 public_id text NOT NULL UNIQUE DEFAULT public_id('inv') CHECK (public_id ~ '^inv_[0-9A-Za-z]{14}$'),
 workspace_id bigint NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 email text NOT NULL CHECK (email = lower(email) AND length(email) BETWEEN 3 AND 254),
 role text NOT NULL CHECK (role IN ('admin', 'member', 'viewer')),
 invited_by bigint REFERENCES users(id) ON DELETE SET NULL,
 token_hash text UNIQUE,
 sent_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 UNIQUE (workspace_id, email)
);
CREATE INDEX workspace_invitations_expires_at_idx ON workspace_invitations(expires_at);
