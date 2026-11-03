-- A signed-in account's code for verifying the email it already has: one
-- per account, so a new code replaces the old. code_hash is an HMAC of the
-- code under a key from APP_KEY.
CREATE TABLE email_verifications (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 code_hash text NOT NULL,
 attempts smallint NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL
);
CREATE INDEX email_verifications_expires_at_idx ON email_verifications(expires_at);
