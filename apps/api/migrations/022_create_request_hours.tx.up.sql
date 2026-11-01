-- request_minutes summed per hour across every server, by the server
-- holding the rollup lock (docs/api/observability.md#metrics-in-the-database).
-- One row per route per hour; an hour is written once (its unique key makes
-- a second rollup of it do nothing). Pruned by make prune (request_hours),
-- 90 days by default.
CREATE TABLE request_hours (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 -- The hour's start.
 hour timestamptz NOT NULL,
 route text NOT NULL CHECK (length(route) BETWEEN 1 AND 200),
 requests bigint NOT NULL CHECK (requests > 0),
 status_4xx bigint NOT NULL DEFAULT 0 CHECK (status_4xx >= 0),
 status_429 bigint NOT NULL DEFAULT 0 CHECK (status_429 >= 0),
 status_5xx bigint NOT NULL DEFAULT 0 CHECK (status_5xx >= 0),
 timeouts bigint NOT NULL DEFAULT 0 CHECK (timeouts >= 0),
 aborted bigint NOT NULL DEFAULT 0 CHECK (aborted >= 0),
 duration_sum_ms double precision NOT NULL CHECK (duration_sum_ms >= 0),
 duration_buckets bigint[] NOT NULL CHECK (cardinality(duration_buckets) = 12),
 UNIQUE (hour, route)
);
