CREATE TABLE password_resets (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 token_hash text NOT NULL UNIQUE,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL
);
CREATE INDEX password_resets_expires_at_idx ON password_resets(expires_at);
