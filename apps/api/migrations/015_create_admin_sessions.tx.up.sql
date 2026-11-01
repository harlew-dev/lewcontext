-- Admin sessions, separate from the app's so an app token can never be
-- looked up as an admin one. Only the token's SHA-256 digest is stored.
-- key_hash is the SHA-256 of the ADMIN_ROUTE_KEY the session was created
-- under: rotating the key ends every admin session, apart from those made
-- under ADMIN_ROUTE_KEY_PREVIOUS while it is set. Revoking admin deletes
-- them (cascade). ip and user_agent are where the session was signed in
-- from (shown on the Admins page); a session isn't bound to them, but the
-- first request from another network sets moved_at and records
-- admin.session.moved, once.
CREATE TABLE admin_sessions (
 token_hash text PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES admins(user_id) ON DELETE CASCADE,
 key_hash text NOT NULL,
 csrf_token text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 last_seen_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 ip inet NOT NULL,
 user_agent text NOT NULL DEFAULT '' CHECK (length(user_agent) <= 256),
 moved_at timestamptz
);
CREATE INDEX admin_sessions_user_id_idx ON admin_sessions(user_id);
CREATE INDEX admin_sessions_expires_at_idx ON admin_sessions(expires_at);
