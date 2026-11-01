package main

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/store"
	"sync/atomic"
	"testing"
	"time"
)

// The final phase fits its budget: server.stopped and the last minute
// each have a share, and the error groups at least 2 s of what's left.
func TestFinalBudget(t *testing.T) {
	if left := finalBudget - stopEventBudget - lastMinuteBudget; left < 2*time.Second {
		t.Fatalf("the error groups get %v", left)
	}
}

// With the database gone or hanging, stopping writes server.stopped and
// the last minute once each, and gives up within their shares: nothing is
// retried, so a shutdown during an outage still ends in time.
func TestStopServerInAnOutage(t *testing.T) {
	cfg, err := pgx.ParseConfig("postgresql://nobody:x@127.0.0.1:1/nothing?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	db := bun.NewDB(stdlib.OpenDB(*cfg), pgdialect.New())
	t.Cleanup(func() { db.Close() })
	var writes atomic.Int32
	flush := &observe.MinuteFlush{Registry: observe.NewRegistry(), Store: func(ctx context.Context, _ observe.Minute) error {
		writes.Add(1)
		<-ctx.Done() // a database that never answers
		return ctx.Err()
	}}
	start := time.Now()
	stopServer(&store.Store{DB: db}, store.ServerInstance{ID: observe.NewServerID()}, "signal", flush)
	if took := time.Since(start); took > stopEventBudget+lastMinuteBudget+500*time.Millisecond {
		t.Fatalf("stopping took %v", took)
	}
	if writes.Load() != 1 {
		t.Fatalf("%d writes of the last minute", writes.Load())
	}
}
