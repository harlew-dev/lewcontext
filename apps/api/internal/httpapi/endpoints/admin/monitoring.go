package admin

import (
	"context"
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/monitor"
	"lewcontext/api/internal/store"
	"net/http"
	"time"
)

// Monitoring is the admin dashboard's Monitoring pages,
// /api/admin/monitoring/{page} (docs/api/monitoring.md): one endpoint per
// page, read whole (its signals, charts and review lists) and recorded once
// (admin.monitoring.viewed). Every page is a set of bounded aggregates, so
// it shares the Events charts' bound: one at a time for each admin, two at
// once on this server, a 5 s deadline.
type Monitoring struct{ *httpapi.Server }

// Answers when a page can't be read within its bounds. Nothing was read,
// so nothing is recorded.
const (
	monitoringBusy    = "Monitoring is busy. Try again in a moment."
	monitoringTooSlow = "This page took too long. Try a shorter window."
)

func (h Monitoring) Show(w http.ResponseWriter, r *http.Request) {
	page, ok := monitor.FindPage(r.PathValue("page"))
	if !ok {
		httpapi.Fail(w, 404, "Page not found.")
		return
	}
	params, ok := input.Query(r, "window")
	if !ok {
		httpapi.Fail(w, 400, "Invalid query parameters.")
		return
	}
	name := params["window"]
	if name == "" {
		name = monitor.DefaultWindow
	}
	now := time.Now().UTC()
	window, ok := monitor.ResolveWindow(name, now)
	if !ok {
		httpapi.Fail(w, 400, "Check the window.")
		return
	}
	admin := httpapi.Admin(r).User.PublicID
	view, err := h.show(r.Context(), admin, page, window, now)
	if err != nil && r.Context().Err() != nil {
		// The admin went away: no 500, no grouped error (ClientGone).
		err = errors.Join(r.Context().Err(), err)
	}
	switch {
	case errors.Is(err, errMonitoringBusy):
		w.Header().Set("Retry-After", "1")
		httpapi.Fail(w, 503, monitoringBusy)
		return
	case errors.Is(err, errMonitoringTooSlow):
		httpapi.Fail(w, 503, monitoringTooSlow)
		return
	case err != nil:
		httpapi.InternalError(w, r, err)
		return
	}
	// One read recorded for the page, once it was read.
	e := events.AdminMonitoringViewed{Admin: admin, Page: page.Name, Window: window.Name}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 200, view)
}

var (
	errMonitoringBusy    = errors.New("monitoring busy")
	errMonitoringTooSlow = errors.New("monitoring too slow")
)

// show reads the page within its bounds: a slot (httpapi.Summaries), then
// the deadline, each signal and section on its own budget within it
// (monitor/sections.go). Too slow only when nothing at all was read.
func (h Monitoring) show(ctx context.Context, admin string, page monitor.Page, window monitor.Window, now time.Time) (monitor.View, error) {
	release, ok := h.Summaries.Acquire(ctx, admin)
	if !ok {
		if err := ctx.Err(); err != nil {
			return monitor.View{}, err
		}
		return monitor.View{}, errMonitoringBusy
	}
	defer release()
	bounded, cancel := context.WithTimeout(ctx, h.Summaries.Deadline)
	defer cancel()
	env := monitor.Env{Store: h.Repo, Limits: httpapi.RateLimits, Now: now, Budget: h.Summaries.Section}
	view, err := monitor.Show(bounded, env, page, window)
	if err != nil && ctx.Err() == nil && (bounded.Err() != nil || errors.Is(err, monitor.ErrNothingRead)) {
		return monitor.View{}, errMonitoringTooSlow
	}
	return view, err
}
