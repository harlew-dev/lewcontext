package factory

import (
	"encoding/json"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"time"
)

// Metrics in the database (docs/api/observability.md): the minute tables
// each server writes, the hour tables the rollup writes, and the servers
// themselves. Seeders make none: made-up traffic would be misleading.

// metricRoutes are route patterns the API registers.
var metricRoutes = []string{
	"GET /api/auth/me",
	"GET /api/workspaces/{workspace}/projects",
	"POST /api/workspaces/{workspace}/projects",
	"page /app/*",
}

type RequestMinute struct {
	bun.BaseModel   `bun:"table:request_minutes,alias:rm"`
	Minute          time.Time
	Server          string
	Route           string
	Requests        int64
	Status4xx       int64 `bun:"status_4xx"`
	Status429       int64 `bun:"status_429"`
	Status5xx       int64 `bun:"status_5xx"`
	Timeouts        int64
	Aborted         int64
	DurationSumMs   float64
	DurationBuckets []int64 `bun:",array"`
}

// RequestMinutes builds a server's minutes in the last hour, on a route the
// API has, every request under 5 ms. Give each its own minute (AtMinute)
// or route when making several for one server.
var RequestMinutes = Define(func(f *gofakeit.Faker) RequestMinute {
	requests := int64(f.IntRange(1, 100))
	return RequestMinute{
		Minute:          time.Now().UTC().Truncate(time.Minute).Add(-time.Duration(f.IntRange(1, 60)) * time.Minute),
		Server:          ServerID(f),
		Route:           f.RandomString(metricRoutes),
		Requests:        requests,
		DurationSumMs:   float64(requests) * 2,
		DurationBuckets: buckets(requests),
	}
})

type RequestHour struct {
	bun.BaseModel   `bun:"table:request_hours,alias:rh"`
	ID              int64 `bun:",pk,autoincrement"`
	Hour            time.Time
	Route           string
	Requests        int64
	Status4xx       int64 `bun:"status_4xx"`
	Status429       int64 `bun:"status_429"`
	Status5xx       int64 `bun:"status_5xx"`
	Timeouts        int64
	Aborted         int64
	DurationSumMs   float64
	DurationBuckets []int64 `bun:",array"`
}

// RequestHours builds hours in the last 90 days, each with a route and hour
// of its own (by position).
var RequestHours = Define(func(f *gofakeit.Faker) RequestHour {
	requests := int64(f.IntRange(1, 5000))
	return RequestHour{
		Hour:            time.Now().UTC().Truncate(time.Hour).Add(-time.Duration(f.IntRange(1, 90*24)) * time.Hour),
		Route:           f.RandomString(metricRoutes) + " " + f.UUID(),
		Requests:        requests,
		DurationSumMs:   float64(requests) * 2,
		DurationBuckets: buckets(requests),
	}
})

type MetricMinute struct {
	bun.BaseModel `bun:"table:metric_minutes,alias:mm"`
	Minute        time.Time
	Server        string
	Metric        string
	Labels        string
	Count         int64
	Sum           float64
	Buckets       []int64 `bun:",array"`
}

// MetricMinutes builds a server's counter minutes in the last hour.
var MetricMinutes = Define(func(f *gofakeit.Faker) MetricMinute {
	return MetricMinute{
		Minute: time.Now().UTC().Truncate(time.Minute).Add(-time.Duration(f.IntRange(1, 60)) * time.Minute),
		Server: ServerID(f),
		Metric: "rate_limit_refused_total",
		Labels: `limit="auth:per-ip:block"`,
		Count:  int64(f.IntRange(1, 50)),
	}
})

type MetricHour struct {
	bun.BaseModel `bun:"table:metric_hours,alias:mh"`
	ID            int64 `bun:",pk,autoincrement"`
	Hour          time.Time
	Metric        string
	Labels        string
	Count         int64
	Sum           float64
	Buckets       []int64 `bun:",array"`
}

// MetricHours builds counter hours in the last 90 days, each with labels
// of its own.
var MetricHours = Define(func(f *gofakeit.Faker) MetricHour {
	return MetricHour{
		Hour:   time.Now().UTC().Truncate(time.Hour).Add(-time.Duration(f.IntRange(1, 90*24)) * time.Hour),
		Metric: "events_recorded_total",
		Labels: `name="` + f.UUID() + `"`,
		Count:  int64(f.IntRange(1, 500)),
	}
})

type ServerInstance struct {
	bun.BaseModel `bun:"table:server_instances,alias:si"`
	ID            string `bun:",pk"`
	Hostname      string
	Version       string
	StartedAt     time.Time
	LastSeenAt    time.Time
	Config        json.RawMessage `bun:"type:jsonb"`
}

// ServerInstances builds servers started in the last day and seen in the
// last minute.
var ServerInstances = Define(func(f *gofakeit.Faker) ServerInstance {
	now := time.Now()
	return ServerInstance{
		ID:         ServerID(f),
		Hostname:   f.DomainName(),
		Version:    "dev",
		StartedAt:  now.Add(-time.Duration(f.IntRange(1, 24*60)) * time.Minute),
		LastSeenAt: now.Add(-time.Duration(f.IntRange(0, 59)) * time.Second),
		Config:     json.RawMessage(`{"production":false,"mailDriver":"off"}`),
	}
})

// LastSeenAt sets when a server was last heard from.
func LastSeenAt(at time.Time) func(*ServerInstance) {
	return func(s *ServerInstance) { s.LastSeenAt = at }
}

// ServerID is a fake server ID in the real shape: srv_ and 14 base62.
func ServerID(f *gofakeit.Faker) string {
	const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	id := []byte("srv_")
	for range 14 {
		id = append(id, base62[f.IntN(len(base62))])
	}
	return string(id)
}

// buckets puts every request in the first (under 5 ms) of the twelve.
func buckets(requests int64) []int64 {
	b := make([]int64, 12)
	b[0] = requests
	return b
}
