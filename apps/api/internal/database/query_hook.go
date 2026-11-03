package database

import (
	"context"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/observe"
	"log/slog"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// QueryHook times every query (docs/api/observability.md#slow-queries): it
// counts each in db_query_duration_ms by operation, and logs one slower
// than Slow (SLOW_QUERY_MS) at warn, with its operation, table, duration
// and the store method that ran it, counting it in db_slow_queries_total.
// It never logs the query's text (event.Query): Bun writes the values into
// it (emails, digests, tokens). The operation is one of a fixed set and the
// table a builder query's own name. A slow query's error is
// logged scrubbed (observe.ErrorAttr). cmd/server adds it to the database
// with AddQueryHook.
type QueryHook struct {
	// Slow is how long a query may take before it's logged; 0 logs none.
	Slow time.Duration
}

var _ bun.QueryHook = QueryHook{}

// BeforeQuery does nothing: Bun stamps the start itself.
func (QueryHook) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context {
	return ctx
}

// AfterQuery counts the query, and logs it if it was slow.
func (h QueryHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	took := time.Since(event.StartTime)
	operation := queryOperation(event)
	observe.DBQueryDuration.Observe(took, operation)
	if h.Slow <= 0 || took < h.Slow {
		return
	}
	table := queryTable(event)
	observe.DBSlowQueries.Inc(table)
	attrs := []slog.Attr{
		slog.String("operation", operation),
		slog.String("table", table),
		slog.Int64("durationMs", took.Milliseconds()),
		slog.String("method", storeMethod()),
	}
	if event.Err != nil {
		attrs = append(attrs, observe.ErrorAttr(event.Err))
	}
	slog.LogAttrs(ctx, slog.LevelWarn, "slow query", attrs...)
}

// queryOperation is the query's operation, one of a fixed set: select,
// insert, update, delete, or other (a lock, a schema change).
func queryOperation(event *bun.QueryEvent) string {
	switch strings.ToLower(event.Operation()) {
	case "select":
		return "select"
	case "insert":
		return "insert"
	case "update":
		return "update"
	case "delete":
		return "delete"
	}
	return "other"
}

// tableName is a table's name as the label may hold it: a plain SQL name,
// optionally schema-qualified.
var tableName = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}(\.[a-z_][a-z0-9_]{0,62})?$`)

// queryTable is the table a builder query names (its model's or its
// TableExpr's), or "other": never anything taken from the query's text.
func queryTable(event *bun.QueryEvent) string {
	type tableNamer interface{ GetTableName() string }
	if q, ok := event.IQuery.(tableNamer); ok {
		name := strings.Trim(q.GetTableName(), `"`)
		if tableName.MatchString(name) {
			return name
		}
	}
	return "other"
}

// storeMethod is the internal/store method the query ran in (FindUser), or
// "" outside one: read from the stack, only for a slow query. A function's
// name, never its arguments.
func storeMethod() string {
	var pcs [32]uintptr
	n := runtime.Callers(3, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	const store = "lewcontext/api/internal/store."
	for {
		frame, more := frames.Next()
		if name, ok := strings.CutPrefix(frame.Function, store); ok {
			name = strings.TrimPrefix(name, "(*Store).")
			// A closure inside a method (RunInTx's) names the method.
			name, _, _ = strings.Cut(name, ".func")
			return name
		}
		if !more {
			return ""
		}
	}
}
