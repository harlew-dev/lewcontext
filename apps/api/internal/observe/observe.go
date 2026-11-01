// Package observe is how the server reports on itself (plan A3,
// docs/api/observability.md): the one log handler every command installs
// (logs.go), each request's ID and what its access line reports
// (request.go), and the hook grouped errors will use (problems.go). It
// imports nothing else of the app's, so the core, the web build and the
// event worker can all use it.
package observe
