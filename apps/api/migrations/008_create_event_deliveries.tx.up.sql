CREATE TABLE event_deliveries (
 event_id bigint NOT NULL REFERENCES events(id) ON DELETE CASCADE,
 listener text NOT NULL CHECK (length(listener) BETWEEN 1 AND 100),
 attempts integer NOT NULL DEFAULT 0,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 delivered_at timestamptz,
 failed_at timestamptz,
 last_error text NOT NULL DEFAULT '' CHECK (length(last_error) <= 500),
 PRIMARY KEY (event_id, listener)
);
CREATE INDEX event_deliveries_pending_idx ON event_deliveries(next_attempt_at, event_id)
 WHERE delivered_at IS NULL AND failed_at IS NULL;
-- The admin dashboard's failed deliveries, few among many delivered.
CREATE INDEX event_deliveries_failed_idx ON event_deliveries(failed_at DESC) WHERE failed_at IS NOT NULL;
