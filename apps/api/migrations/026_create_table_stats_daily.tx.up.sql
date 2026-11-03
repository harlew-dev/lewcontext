-- Each table's size once a day (docs/api/monitoring.md#data), for the
-- admin Monitoring Data page's growth: written by one server (the one
-- holding the table-stats advisory lock) after the hourly rollup, at most
-- once a day, one row per known table (store.DataTables), never updated.
-- Pruned by make prune (table_stats_daily), 400 days by default.
CREATE TABLE table_stats_daily (
 -- A bigint id for make prune, which deletes by id.
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 -- The day it was taken, on the database's clock (UTC).
 day date NOT NULL,
 table_name text NOT NULL CHECK (table_name ~ '^[a-z][a-z0-9_]{0,62}$'),
 -- PostgreSQL's running count of live rows (pg_stat_user_tables.n_live_tup):
 -- close, not exact.
 live_rows bigint NOT NULL CHECK (live_rows >= 0),
 -- The table with its indexes and TOAST (pg_total_relation_size).
 total_bytes bigint NOT NULL CHECK (total_bytes >= 0),
 UNIQUE (day, table_name)
);
