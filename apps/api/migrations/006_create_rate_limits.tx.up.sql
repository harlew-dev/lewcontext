CREATE TABLE rate_limits (
 key text PRIMARY KEY,
 hits integer NOT NULL DEFAULT 1,
 resets_at timestamptz NOT NULL
);
-- The cleanup's delete of ended windows, and the Integrity page's check
-- that it runs (no window ended over 30 minutes ago).
CREATE INDEX rate_limits_resets_at_idx ON rate_limits(resets_at);
