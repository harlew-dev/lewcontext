-- A sign-in whose password was right, waiting for its second factor. The
-- token is in an HttpOnly cookie; only its digest is stored. purpose keeps a
-- challenge from completing another flow's sign-in.
CREATE TABLE mfa_challenges (
 token_hash text PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 email_version bigint NOT NULL DEFAULT 0 CHECK (email_version >= 0),
 purpose text NOT NULL CHECK (purpose IN ('login', 'admin', 'admin_setup')),
 attempts int NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL
);
CREATE INDEX mfa_challenges_user_id_idx ON mfa_challenges(user_id);
CREATE INDEX mfa_challenges_expires_at_idx ON mfa_challenges(expires_at);
