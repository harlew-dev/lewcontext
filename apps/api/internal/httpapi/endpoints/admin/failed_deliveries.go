package admin

import (
	"errors"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/store"
	"net/http"
	"slices"
	"time"
)

// FailedDeliveries are the deliveries that gave up, as the admin dashboard
// acts on them together: queuing a listener's failures again after an
// outage (SMTP down for an hour).
type FailedDeliveries struct{ *httpapi.Server }

// maxRetryAge is how far back a bulk retry reaches: the admin dashboard's
// bounded window (plans/features/admin/README.md, rule 4). Listeners also
// drop requests older than their link's or code's lifetime.
const maxRetryAge = 7 * 24 * time.Hour

// retryInput is Retry's body: which listener's failures, failed since
// when, and why.
type retryInput struct {
	Listener    string    `json:"listener"`
	FailedSince time.Time `json:"failedSince"`
	Reason      string    `json:"reason"`
}

// Retry queues again every delivery to the listener that failed since
// failedSince (at most 7 days ago), the most recent 1,000 of them, as
// Deliveries.Retry does one. 200 {count}; 409 when none failed. Recorded as
// admin.delivery.retried with the count. POST /api/admin/deliveries/retry.
func (h FailedDeliveries) Retry(w http.ResponseWriter, r *http.Request) {
	var in retryInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !slices.Contains(listeners(h.Server), in.Listener) {
		httpapi.FailField(w, 422, "listener", "Pick a listener.")
		return
	}
	if age := time.Since(in.FailedSince); age < 0 || age > maxRetryAge {
		httpapi.FailField(w, 422, "failedSince", "Pick a time in the last 7 days.")
		return
	}
	action, ok := adminAction(w, r, in.Reason)
	if !ok {
		return
	}
	count, err := h.Repo.RequeueFailedDeliveries(r.Context(), in.Listener, in.FailedSince, action)
	switch {
	case errors.Is(err, store.ErrNothingToRetry):
		httpapi.Fail(w, 409, "No delivery to that listener failed since then.")
	case err != nil:
		httpapi.InternalError(w, r, err)
	default:
		httpapi.JSON(w, 200, map[string]int{"count": count})
	}
}
