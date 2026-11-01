-- Each server's requests per route per minute
-- (docs/api/observability.md#metrics-in-the-database): never a row per
-- request. Every server writes the minute just ended once, in one
-- statement, a row per route that had traffic, so what is stored depends
-- on how many routes there are, never on the traffic. Insert only: a
-- minute is never updated, and a write that fails is dropped, not retried.
-- The server holding the rollup lock sums each hour into request_hours and
-- deletes minutes older than 48 hours, so the table stays bounded without
-- make prune.
CREATE TABLE request_minutes (
 -- The minute's start.
 minute timestamptz NOT NULL,
 -- The server that counted them: server_instances.id (not a foreign key:
 -- a server's row goes a day after it stops, its minutes after 48 hours).
 server text NOT NULL CHECK (server ~ '^srv_[0-9A-Za-z]{14}$'),
 -- The route pattern or page label, never a path ("unmatched" for none).
 route text NOT NULL CHECK (length(route) BETWEEN 1 AND 200),
 requests integer NOT NULL CHECK (requests > 0),
 -- 4xx includes the 429s, which status_429 counts again.
 status_4xx integer NOT NULL DEFAULT 0 CHECK (status_4xx >= 0),
 status_429 integer NOT NULL DEFAULT 0 CHECK (status_429 >= 0),
 status_5xx integer NOT NULL DEFAULT 0 CHECK (status_5xx >= 0),
 -- Requests the 10 s deadline ended (also counted in their status).
 timeouts integer NOT NULL DEFAULT 0 CHECK (timeouts >= 0),
 -- Requests that got no answer: the client went away, or the connection
 -- was cut. Counted in requests, never in a status class.
 aborted integer NOT NULL DEFAULT 0 CHECK (aborted >= 0),
 duration_sum_ms double precision NOT NULL CHECK (duration_sum_ms >= 0),
 -- Requests per bound: 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000,
 -- 10000 ms, and above (not cumulative).
 duration_buckets integer[] NOT NULL CHECK (cardinality(duration_buckets) = 12),
 PRIMARY KEY (minute, route, server)
);
