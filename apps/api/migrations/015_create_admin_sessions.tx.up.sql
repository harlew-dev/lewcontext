-- Admin sessions, separate from the app's so an app token can never be
-- looked up as an admin one. Only the token's SHA-256 digest is stored.
-- key_hash is the SHA-256 of the ADMIN_ROUTE_KEY the session was created
-- under: rotating the key ends every admin session. Revoking admin deletes
-- them (cascade).
CREATE TABLE admin_sessions (
 token_hash text PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES admins(user_id) ON DELETE CASCADE,
 key_hash text NOT NULL,
 csrf_token text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 last_seen_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL
);
CREATE INDEX admin_sessions_user_id_idx ON admin_sessions(user_id);
CREATE INDEX admin_sessions_expires_at_idx ON admin_sessions(expires_at);
