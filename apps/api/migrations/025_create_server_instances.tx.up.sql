-- Every running server (docs/api/observability.md#servers-and-versions):
-- a row per process, made at startup with a new ID, its last_seen_at moved
-- by the heartbeat in each minute's metrics write. A server silent for 2
-- minutes is gone; the rollup deletes rows silent for a day.
CREATE TABLE server_instances (
 -- srv_ and 14 base62, new at every start (made by the server, which may
 -- have to run before it can write the row).
 id text PRIMARY KEY CHECK (id ~ '^srv_[0-9A-Za-z]{14}$'),
 hostname text NOT NULL CHECK (length(hostname) <= 255),
 -- observe.Version(): the VCS revision, or dev.
 version text NOT NULL CHECK (version ~ '^[A-Za-z0-9._-]{1,64}$'),
 started_at timestamptz NOT NULL,
 last_seen_at timestamptz NOT NULL,
 -- The configuration's summary (config.Summary): which settings are set
 -- and their non-secret values, never a secret's value or digest.
 config jsonb NOT NULL
);
CREATE INDEX server_instances_last_seen_idx ON server_instances(last_seen_at);
