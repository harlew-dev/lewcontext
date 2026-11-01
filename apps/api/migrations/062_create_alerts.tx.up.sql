-- Alerts (docs/api/monitoring.md#alerts): a monitoring signal that stayed at its warn
-- or critical level for its rule's time, opened by the one server holding
-- the alerts' advisory lock, and resolved after 10 minutes back at ok. At
-- most one open alert per signal (alerts_open_signal_idx), which is also
-- the backstop if two servers ever evaluated at once. Resolved alerts are
-- kept a year (make prune target=alerts): when we detected something and
-- how fast we answered is evidence an auditor asks for.
CREATE TABLE alerts (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 public_id text NOT NULL UNIQUE DEFAULT public_id('alt') CHECK (public_id ~ '^alt_[0-9A-Za-z]{14}$'),
 -- The monitoring signal's stable name, e.g. auth.stuffing_successes.
 signal text NOT NULL CHECK (signal ~ '^[a-z][a-z0-9_]*(\.[a-z0-9_]+){1,2}$'),
 -- Raised in place from warn to critical, never lowered while open.
 level text NOT NULL CHECK (level IN ('warn', 'critical')),
 opened_at timestamptz NOT NULL DEFAULT now(),
 last_value double precision NOT NULL,
 peak_value double precision NOT NULL,
 -- The signal's drill-down filter, and an integrity check's sample public
 -- IDs (at most 10): shown on the alert's page, never emailed or posted.
 details jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(details) = 'object'),
 -- How its opening was told: mail (emailed on its own), held (waiting
 -- for a digest), digest (in a digest), quiet (silenced, or its signal
 -- opened another alert within the hour: no email, no webhook).
 notified text NOT NULL DEFAULT 'quiet' CHECK (notified IN ('mail', 'held', 'digest', 'quiet')),
 -- The last email about it (opening, escalation, a 4-hourly reminder).
 last_notified_at timestamptz,
 -- Who acknowledged it (stopping its reminders) and silenced it (no
 -- email or webhook until silenced_until); null once that account is
 -- deleted, as ip_blocks.created_by: the events still name them.
 acknowledged_at timestamptz,
 acknowledged_by bigint REFERENCES users(id) ON DELETE SET NULL,
 silenced_until timestamptz,
 silenced_by bigint REFERENCES users(id) ON DELETE SET NULL,
 resolved_at timestamptz,
 CHECK (resolved_at IS NULL OR resolved_at >= opened_at)
);
-- One open alert per signal.
CREATE UNIQUE INDEX alerts_open_signal_idx ON alerts(signal) WHERE resolved_at IS NULL;
-- The Alerts page, newest first, and its cursor.
CREATE INDEX alerts_opened_idx ON alerts(opened_at DESC, id DESC);
-- A signal's recent alerts: the hour's cooldown, a silence carried over.
CREATE INDEX alerts_signal_opened_idx ON alerts(signal, opened_at DESC);
-- Held openings waiting for their digest, and when the last digest went.
CREATE INDEX alerts_digest_idx ON alerts(last_notified_at) WHERE notified IN ('held', 'digest');
-- Pruning resolved alerts a year on.
CREATE INDEX alerts_resolved_idx ON alerts(resolved_at) WHERE resolved_at IS NOT NULL;
