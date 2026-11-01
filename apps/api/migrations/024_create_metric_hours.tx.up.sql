-- metric_minutes summed per hour across every server, with request_hours
-- (docs/api/observability.md#metrics-in-the-database). A gauge's hour has
-- the number of its minutes as count and their values' total as sum, so
-- sum / count is its average. Pruned by make prune (metric_hours), 90 days
-- by default.
CREATE TABLE metric_hours (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 hour timestamptz NOT NULL,
 metric text NOT NULL CHECK (metric ~ '^[a-z][a-z0-9_]{0,99}$'),
 labels text NOT NULL DEFAULT '' CHECK (length(labels) <= 1000),
 count bigint NOT NULL CHECK (count > 0),
 sum double precision NOT NULL DEFAULT 0,
 buckets bigint[],
 UNIQUE (hour, metric, labels)
);
