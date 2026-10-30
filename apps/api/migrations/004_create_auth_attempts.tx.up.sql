CREATE TABLE auth_attempts (
 key text PRIMARY KEY,
 attempts integer NOT NULL DEFAULT 1,
 resets_at timestamptz NOT NULL
);
