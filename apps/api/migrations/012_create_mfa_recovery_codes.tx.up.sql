-- Single-use recovery codes, stored as HMAC-SHA256 digests keyed from
-- APP_KEY, so they can't be guessed offline from a dump.
CREATE TABLE mfa_recovery_codes (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 code_hash text NOT NULL,
 used_at timestamptz,
 UNIQUE (user_id, code_hash)
);
