package observe

import (
	"database/sql"
	"runtime"
	"runtime/metrics"
	"time"
)

// Metrics is the process's registry: every metric below, and the request
// table httpapi counts into. /metrics serves it and the minute's flush
// stores it.
var Metrics = NewRegistry()

// The metrics the app counts (docs/api/observability.md#metrics), each
// label from a fixed set.
var (
	// ErrorsTotal counts every Problem reported (Collector.Report), per
	// occurrence: source and the route, listener or job.
	ErrorsTotal = Metrics.Counter("errors_total", "Failures reported to the error collector, by source and route, listener or job.", "source", "route")
	// RateLimitRefused counts every request a rate limit refused (not only
	// the first in a window, which is what rate_limit.reached records).
	RateLimitRefused = Metrics.Counter("rate_limit_refused_total", "Requests refused by a rate limit, by limit and level.", "limit")
	// QuotaRefused counts creates a quota refused.
	QuotaRefused = Metrics.Counter("quota_refused_total", "Creates refused by a quota, by quota.", "quota")
	// PasswordHashBusy counts sign-ins and password checks refused because
	// every hashing slot was taken (503).
	PasswordHashBusy = Metrics.Counter("password_hash_busy_total", "Password checks refused because every hashing slot was busy.")
	// DBQueryDuration times every query (database.QueryHook), by
	// operation: select, insert, update, delete or other.
	DBQueryDuration = Metrics.Histogram("db_query_duration_ms", "How long database queries took, by operation, in milliseconds.", DurationBounds, "operation")
	// DBSlowQueries counts queries over SLOW_QUERY_MS, by table.
	DBSlowQueries = Metrics.Counter("db_slow_queries_total", "Queries slower than SLOW_QUERY_MS, by table.", "table")
	// EventsRecorded counts events written, by name (in their change's
	// transaction, which may still roll back).
	EventsRecorded = Metrics.Counter("events_recorded_total", "Events written to the audit trail, by name.", "name")
	// Deliveries counts the event worker's outcomes, by listener.
	Deliveries = Metrics.Counter("deliveries_total", "Event deliveries by listener and outcome: delivered, retried or failed.", "listener", "outcome")
	// DeliveryLag is how long an event waited for its listener's first
	// attempt.
	DeliveryLag = Metrics.Histogram("delivery_lag_ms", "Time from an event being recorded to its listener's first attempt, in milliseconds.", DurationBounds, "listener")
	// DeliveriesPending is how many deliveries wait to run, read once a
	// minute.
	DeliveriesPending = Metrics.Gauge("deliveries_pending", "Event deliveries waiting to run, read once a minute.")
	// IPBlockRefused counts requests refused because their client address
	// is inside a live IP block (docs/api/admin.md#ip-blocks). No labels:
	// the address is never one.
	IPBlockRefused = Metrics.Counter("ip_block_refused_total", "Requests refused because their client address is blocked.")
	// IPBlocksLoaded is when this server last loaded the live IP blocks, in
	// Unix seconds, or 0 before its first load (when nothing is blocked on
	// it): an alert can fire on a server whose list has gone stale.
	IPBlocksLoaded = Metrics.Gauge("ip_blocks_loaded_timestamp_seconds", "When this server last loaded the live IP blocks, in Unix seconds; 0 before its first load.")
	// BuildInfo is 1, labelled with this build's version and Go's.
	BuildInfo = Metrics.Gauge("build_info", "This server's build: 1, labelled with its version and Go's.", "version", "go")
)

// Delivery outcomes, deliveries_total's outcome label.
const (
	DeliveryDelivered = "delivered"
	DeliveryRetried   = "retried"
	DeliveryFailed    = "failed"
)

// The database pool's and the runtime's gauges, set as they are collected.
var (
	dbOpen          = Metrics.Gauge("db_pool_open", "Database connections open.")
	dbInUse         = Metrics.Gauge("db_pool_in_use", "Database connections in use.")
	dbIdle          = Metrics.Gauge("db_pool_idle", "Database connections idle.")
	dbWaitCount     = Metrics.Gauge("db_pool_wait_count", "Times a query waited for a database connection, since the server started.")
	dbWaitMs        = Metrics.Gauge("db_pool_wait_ms", "Time queries spent waiting for a database connection since the server started, in milliseconds.")
	goGoroutines    = Metrics.Gauge("go_goroutines", "Goroutines running.")
	goHeapBytes     = Metrics.Gauge("go_heap_bytes", "Bytes of heap memory in use by live and unswept objects.")
	goMemoryBytes   = Metrics.Gauge("go_memory_bytes", "Bytes of memory the Go runtime holds from the system.")
	goGCCycles      = Metrics.Gauge("go_gc_cycles", "Garbage collections since the server started.")
	goGCPauseMs     = Metrics.Gauge("go_gc_pause_ms", "Time the garbage collector stopped the program since the server started, in milliseconds (from the runtime's pause histogram, so approximate).")
	runtimeReadings = []metrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/memory/classes/total:bytes"},
		{Name: "/gc/cycles/total:gc-cycles"},
		{Name: "/sched/pauses/total/gc:seconds"},
	}
)

func init() {
	BuildInfo.Set(1, Version(), runtime.Version())
	Metrics.OnCollect(collectRuntime)
}

// WatchDB sets the pool gauges from db's statistics at every collection.
func WatchDB(db *sql.DB) {
	Metrics.OnCollect(func() {
		stats := db.Stats()
		dbOpen.Set(float64(stats.OpenConnections))
		dbInUse.Set(float64(stats.InUse))
		dbIdle.Set(float64(stats.Idle))
		dbWaitCount.Set(float64(stats.WaitCount))
		dbWaitMs.Set(float64(stats.WaitDuration) / float64(time.Millisecond))
	})
}

// collectRuntime sets the runtime's gauges.
func collectRuntime() {
	goGoroutines.Set(float64(runtime.NumGoroutine()))
	samples := make([]metrics.Sample, len(runtimeReadings))
	copy(samples, runtimeReadings)
	metrics.Read(samples)
	goHeapBytes.Set(sampleValue(samples[0]))
	goMemoryBytes.Set(sampleValue(samples[1]))
	goGCCycles.Set(sampleValue(samples[2]))
	goGCPauseMs.Set(pauseMs(samples[3]))
}

func sampleValue(s metrics.Sample) float64 {
	switch s.Value.Kind() {
	case metrics.KindUint64:
		return float64(s.Value.Uint64())
	case metrics.KindFloat64:
		return s.Value.Float64()
	}
	return 0
}

// pauseMs sums a pause histogram, each pause at its bucket's lower bound.
func pauseMs(s metrics.Sample) float64 {
	if s.Value.Kind() != metrics.KindFloat64Histogram {
		return 0
	}
	h := s.Value.Float64Histogram()
	total := 0.0
	for i, n := range h.Counts {
		if lower := h.Buckets[i]; n > 0 && lower > 0 {
			total += float64(n) * lower
		}
	}
	return total * 1000
}
