package apitest

import (
	"github.com/uptrace/bun"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// RetryWithin checks the response's Retry-After is between a second and the
// limit's window.
func RetryWithin(t *testing.T, w *httptest.ResponseRecorder, window time.Duration) {
	t.Helper()
	seconds, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil || seconds < 1 || seconds > int(window.Seconds()) {
		t.Fatalf("Retry-After %q for a %s window", w.Header().Get("Retry-After"), window)
	}
}

// Reached is the one rate_limit.reached event, which must be for limit (with
// its level, e.g. "auth:per-ip:block") and from the test requests' address.
func Reached(t *testing.T, db bun.IDB, limit string) Event {
	t.Helper()
	e := OnlyEvent(t, db, "rate_limit.reached")
	if e.Payload["limit"] != limit || e.IP != "127.0.0.1" || e.Payload["resetsAt"] == nil {
		t.Fatalf("want %s: %+v", limit, e)
	}
	return e
}
