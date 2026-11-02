package admin

import (
	"context"
	"database/sql"
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/monitor"
	"lewcontext/api/internal/store"
	"net/http"
)

// Errors is one grouped error's page on the Monitoring pages,
// /api/admin/errors/{error} (docs/api/monitoring.md#one-group): its
// scrubbed message and stack, its route's occurrences over a window, and
// the release now running. Read within the Monitoring pages' bounds and
// recorded as admin.error.viewed; and resolving it, a housekeeping action
// (plans/features/implemented/admin/actions.md: the operator role, no
// code).
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

// resolveInput is Update's body: {"resolved": true}, the one change it
// makes.
type resolveInput struct {
	Resolved bool `json:"resolved"`
}

// Update marks the group resolved ({"resolved": true}), recording
// admin.error.resolved: 204. Its next occurrence opens it again. 422
// without resolved true; 404 unknown; 409 when it's resolved already. A
// housekeeping action: the operator role and admin-action:per-user, no
// code or reason. PATCH /api/admin/errors/{error}.
func (h Errors) Update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("error")
	if !input.PublicID("err", id) {
		httpapi.Fail(w, 404, "Error not found.")
		return
	}
	var in resolveInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !in.Resolved {
		httpapi.FailField(w, 422, "resolved", "Only resolving an error group is supported.")
		return
	}
	err := h.Repo.ResolveErrorGroup(r.Context(), id, httpapi.Admin(r).User)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		httpapi.Fail(w, 404, "Error not found.")
	case errors.Is(err, store.ErrAlreadyDone):
		httpapi.Fail(w, 409, "This error is resolved already.")
	case err != nil:
		httpapi.InternalError(w, r, err)
	default:
		w.WriteHeader(204)
	}
}
