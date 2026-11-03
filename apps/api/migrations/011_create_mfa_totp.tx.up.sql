-- One authenticator per user. secret is sealed with APP_KEY
-- (AES-256-GCM, bound to user_id), never stored in plain. confirmed_at is
-- null while setup is pending; a code works once, after last_used_step.
CREATE TABLE mfa_totp (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 secret bytea NOT NULL,
 last_used_step bigint NOT NULL DEFAULT 0,
 confirmed_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
