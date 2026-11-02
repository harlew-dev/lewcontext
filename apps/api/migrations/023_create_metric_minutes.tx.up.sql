-- Each server's other metrics per minute
-- (docs/api/observability.md#metrics-in-the-database): a row per series
-- that changed during the minute, and one per gauge. Written in the same
-- statement as request_minutes, insert only, and kept 48 hours the same way.
CREATE TABLE metric_minutes (
 minute timestamptz NOT NULL,
 server text NOT NULL CHECK (server ~ '^srv_[0-9A-Za-z]{14}$'),
 metric text NOT NULL CHECK (metric ~ '^[a-z][a-z0-9_]{0,99}$'),
 -- The series' labels, name="value" pairs sorted by name, as /metrics
 -- writes them: values only from fixed sets (route patterns, limit,
 -- listener and event names), never from a request.
 labels text NOT NULL DEFAULT '' CHECK (length(labels) <= 1000),
 -- A counter's increase, a histogram's observations, 1 for a gauge.
 count bigint NOT NULL CHECK (count > 0),
 -- A histogram's sum in milliseconds, or a gauge's value.
 sum double precision NOT NULL DEFAULT 0,
 -- A histogram's counts per bound and one above; null otherwise.
 buckets bigint[],
 PRIMARY KEY (minute, metric, labels, server)
);
