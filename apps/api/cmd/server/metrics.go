package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/store"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"
	"unicode/utf8"
)

// Metrics, server instances and /metrics (docs/api/observability.md): what
// this server starts besides the API, and how it stops them.

// Final shutdown budget: once requests and the worker have finished,
// server.stopped and the last minute's metrics get these, and the error
// groups the rest of finalBudget (flushErrors), so a container's stop
// timeout stays SHUTDOWN_DELAY + 20 s.
const (
	finalBudget      = 5 * time.Second
	stopEventBudget  = 1500 * time.Millisecond
	lastMinuteBudget = 1500 * time.Millisecond
)

// Rollup timings: the first run a minute after startup, then hourly; each
// run at most a minute.
const (
	firstRollup    = time.Minute
	rollupInterval = time.Hour
	rollupTimeout  = time.Minute
	// silentServer is how long a server_instances row stays after its
	// server was last heard from.
	silentServer = 24 * time.Hour
)

// newServerInstance is this process's server_instances row: a new ID, the
// host's name, the build's version and the configuration's summary (never a
// secret: config.Summary).
func newServerInstance(ctx context.Context, db bun.IDB, c config.Config) (store.ServerInstance, error) {
	applied, embedded, err := database.CountMigrations(ctx, db)
	if err != nil {
		return store.ServerInstance{}, err
	}
	summary, err := json.Marshal(c.Summary(config.MigrationsSummary{Applied: applied, Embedded: embedded}))
	if err != nil {
		return store.ServerInstance{}, err
	}
	hostname := serverHostname()
	now := time.Now()
	return store.ServerInstance{
		ID: observe.NewServerID(), Hostname: hostname, Version: observe.Version(),
		StartedAt: now, LastSeenAt: now, Config: summary,
	}, nil
}

// maxHostname is server_instances.hostname's limit, in bytes.
const maxHostname = 255

// serverHostname is the host's name, cut to maxHostname bytes on a
// character boundary, or "invalid" when it isn't UTF-8 (or can't be read).
func serverHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "invalid"
	}
	return cutHostname(hostname)
}

// cutHostname is hostname cut to maxHostname bytes on a character
// boundary, or "invalid" when it isn't UTF-8.
func cutHostname(hostname string) string {
	if !utf8.ValidString(hostname) {
		return "invalid"
	}
	for len(hostname) > maxHostname {
		_, size := utf8.DecodeLastRuneInString(hostname)
		hostname = hostname[:len(hostname)-size]
	}
	return hostname
}

// minuteFlush writes each minute's metrics with the server's heartbeat
// (METRICS_STORE=off: the heartbeat alone), reading the pending deliveries
// first for their gauge.
func minuteFlush(repository *store.Store, instance store.ServerInstance, c config.Config) *observe.MinuteFlush {
	return &observe.MinuteFlush{
		Registry: observe.Metrics,
		Discard:  c.MetricsStore == config.MetricsStoreOff,
		Before: func(ctx context.Context) {
			count, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			if n, err := repository.CountPendingDeliveries(count); err == nil {
				observe.DeliveriesPending.Set(float64(n))
			}
		},
		Store: func(ctx context.Context, m observe.Minute) error {
			return repository.RecordMetrics(ctx, instance, m)
		},
	}
}

// runMinutes runs the minute's flush until stopMinutes is called, which
// waits for it to finish (cutting a write in progress short).
func runMinutes(flush *observe.MinuteFlush) (stopMinutes func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		flush.Run(ctx)
	}()
	return func() {
		cancel()
		<-done
	}
}

// summarizeMetrics runs the hourly rollup until ctx is done: a minute after
// startup, then every hour. Each server runs it; the advisory lock lets one
// at a time do the work, and the others skip their turn. It also deletes
// the rows of servers silent for a day.
func summarizeMetrics(ctx context.Context, repository *store.Store) {
	wait := firstRollup
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = rollupInterval
		run, cancel := context.WithTimeout(ctx, rollupTimeout)
		rollup, err := repository.SummarizeMetrics(run)
		cancel()
		if ctx.Err() != nil {
			return
		}
		// Whatever the rollup did, servers silent for a day go.
		gone, goneErr := deleteSilentServers(ctx, repository)
		err = errors.Join(err, goneErr)
		if rollup.Locked || gone > 0 {
			slog.Debug("metrics rolled up", "locked", rollup.Locked, "hours", rollup.Hours, "minutesDeleted", rollup.MinutesDeleted, "serversDeleted", gone)
		}
		if err != nil && ctx.Err() == nil {
			slog.Warn("rolling up metrics failed; trying again in an hour", observe.ErrorAttr(err))
			observe.Report(ctx, observe.Problem{Source: observe.SourceBackground, Route: "metrics rollup", Err: err})
		}
	}
}

// silentServersTimeout bounds deleting the rows of servers silent for a
// day.
const silentServersTimeout = 10 * time.Second

// deleteSilentServers deletes the server_instances rows of servers silent
// for a day: after each rollup, whatever it did, and at startup, so a
// crash loop's rows go even when no server lives long enough to roll up.
func deleteSilentServers(ctx context.Context, repository *store.Store) (int, error) {
	run, cancel := context.WithTimeout(ctx, silentServersTimeout)
	defer cancel()
	return repository.DeleteSilentServerInstances(run, time.Now().Add(-silentServer))
}

// rollUp runs summarizeMetrics until stopRollup is called, which waits for
// it to finish (a run in progress is rolled back).
func rollUp(ctx context.Context, repository *store.Store) (stopRollup func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		summarizeMetrics(ctx, repository)
	}()
	return func() {
		cancel()
		<-done
	}
}

// serveMetrics starts /metrics on METRICS_ADDR, its own listener with its
// own timeouts, never the API's. It listens before returning, so an
// address already taken stops the server at startup. Nil without
// METRICS_ADDR.
func serveMetrics(c config.Config) (*http.Server, error) {
	if c.MetricsAddr == "" {
		return nil, nil
	}
	listener, err := net.Listen("tcp", c.MetricsAddr)
	if err != nil {
		return nil, err
	}
	server := observe.MetricsServer(c.MetricsAddr, observe.MetricsHandler(observe.Metrics, c.MetricsToken))
	server.ErrorLog = slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn)
	go func() {
		auth := "none"
		if c.MetricsToken != "" {
			auth = "bearer"
		}
		slog.Info("serving /metrics", "address", c.MetricsAddr, "auth", auth)
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("the /metrics listener stopped", "error", err)
		}
	}()
	return server, nil
}

// stopServer records server.stopped with its reason, then writes the
// metrics counted since the last minute, each within its share of the
// final budget, once requests, the worker and the minute's loop have
// finished. Neither is retried.
func stopServer(repository *store.Store, instance store.ServerInstance, reason string, flush *observe.MinuteFlush) {
	stop, cancel := context.WithTimeout(context.Background(), stopEventBudget)
	defer cancel()
	if err := repository.RecordServerStop(stop, instance, reason); err != nil {
		slog.Warn("recording server.stopped failed", observe.ErrorAttr(err))
	}
	last, cancelLast := context.WithTimeout(context.Background(), lastMinuteBudget)
	defer cancelLast()
	if err := flush.Flush(last); err != nil {
		slog.Warn("storing the last minute's metrics failed", observe.ErrorAttr(err))
	}
}
