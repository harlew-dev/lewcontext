-- Grouped errors (docs/api/observability.md#grouped-errors): one row per
-- distinct failure, never one per occurrence. The server's collector
-- upserts each group every 10 seconds, only when something failed, adding
-- to count. The message and stack are scrubbed before they get here: no
-- email address, IP address, public or request ID, number, quoted value or
-- token-like string, and a stack holds only function names and lines.
-- Pruned by make prune (app_errors), 30 days after a group was last seen.
CREATE TABLE app_errors (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 -- The group's address on the admin Errors page (err_ and 14 base62).
 public_id text NOT NULL UNIQUE DEFAULT public_id('err') CHECK (public_id ~ '^err_[0-9A-Za-z]{14}$'),
 -- The hex SHA-256 of the source, route and scrubbed message.
 fingerprint text NOT NULL UNIQUE CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
 source text NOT NULL CHECK (source IN ('request', 'panic', 'worker', 'background', 'client')),
 -- The route pattern or page label, the listener's or job's name, or a
 -- browser page's route pattern: never a path.
 route text NOT NULL DEFAULT '' CHECK (length(route) <= 200),
 message text NOT NULL CHECK (length(message) BETWEEN 1 AND 1000),
 stack text NOT NULL DEFAULT '' CHECK (length(stack) <= 8000),
 count bigint NOT NULL DEFAULT 1 CHECK (count > 0),
 first_seen_at timestamptz NOT NULL,
 last_seen_at timestamptz NOT NULL,
 -- The latest occurrence's request, our own ID only (as events.request_id).
 last_request_id text NOT NULL DEFAULT '' CHECK (last_request_id = '' OR last_request_id ~ '^req_[0-9A-Za-z]{14}$'),
 last_version text NOT NULL DEFAULT '' CHECK (last_version ~ '^[A-Za-z0-9._-]{0,64}$'),
 -- Set by an admin (the admin actions plan); a new occurrence clears it.
 resolved_at timestamptz
);
CREATE INDEX app_errors_last_seen_idx ON app_errors(last_seen_at DESC);
