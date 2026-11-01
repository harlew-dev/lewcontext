package admin

import (
	"context"
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/monitor"
	"lewcontext/api/internal/store"
	"net/http"
)

// Errors is one grouped error's page on the Monitoring pages,
// /api/admin/errors/{error} (docs/api/monitoring.md#one-group): its
// scrubbed message and stack, its route's occurrences over a window, and
// the release now running. Read within the Monitoring pages' bounds and
// recorded as admin.error.viewed. Read-only: resolving a group is an
// admin action (plans/features/admin/actions.md).
type Errors struct{ *httpapi.Server }

// Show reads one group over ?window=1h|24h|7d (24h by default); a group
// ID of any other shape, or none, is 404.
func (h Errors) Show(w http.ResponseWriter, r *http.Request) {
	window, now, ok := monitoringWindow(w, r)
	if !ok {
		return
	}
	id := r.PathValue("error")
	admin := httpapi.Admin(r).User.PublicID
	env := monitor.Env{Store: h.Repo, Limits: httpapi.RateLimits, Now: now}
	view, err := bounded(r.Context(), h.Server, admin, func(ctx context.Context) (monitor.ErrorView, error) {
		return monitor.ShowError(ctx, env, id, window)
	})
	if errors.Is(err, monitor.ErrNoErrorGroup) {
		httpapi.Fail(w, 404, "Error not found.")
		return
	}
	if failBounded(w, r, err) {
		return
	}
	e := events.AdminErrorViewed{Admin: admin, Error: view.Group.ID, Window: window.Name}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 200, view)
}
