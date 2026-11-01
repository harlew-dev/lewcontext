// Package observe is how the server reports on itself (plan A3,
// docs/api/observability.md): the one log handler every command installs
// (logs.go), each request's ID and what its access line reports
// (request.go), the error collector behind app_errors (problems.go,
// collector.go, scrub.go), and the metric registry (metrics.go,
// requests.go, catalogue.go) with its minute's flush (minutes.go) and
// /metrics (prometheus.go). It imports nothing else of the app's, so the
// core, the store, the web build and the event worker can all use it.
package observe
