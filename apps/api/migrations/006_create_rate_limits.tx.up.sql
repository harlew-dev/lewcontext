CREATE TABLE rate_limits (
 key text PRIMARY KEY,
 hits integer NOT NULL DEFAULT 1,
 resets_at timestamptz NOT NULL
);
