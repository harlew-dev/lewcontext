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
	window, now, ok := monitoringWindow(w, r)
	if !ok {
		return
	}
	admin := httpapi.Admin(r).User.PublicID
	env := monitor.Env{Store: h.Repo, Limits: httpapi.RateLimits, Now: now, Budget: h.Summaries.Section}
	view, err := bounded(r.Context(), h.Server, admin, func(ctx context.Context) (monitor.View, error) {
		return monitor.Show(ctx, env, page, window)
	})
	if failBounded(w, r, err) {
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

// monitoringWindow reads a Monitoring read's one parameter, its window
// (monitor.DefaultWindow when it's left out), resolved now. Any other
// parameter, or one given twice, answers 400.
func monitoringWindow(w http.ResponseWriter, r *http.Request) (monitor.Window, time.Time, bool) {
	params, ok := input.Query(r, "window")
	if !ok {
		httpapi.Fail(w, 400, "Invalid query parameters.")
		return monitor.Window{}, time.Time{}, false
	}
	name := params["window"]
	if name == "" {
		name = monitor.DefaultWindow
	}
	now := time.Now().UTC()
	window, ok := monitor.ResolveWindow(name, now)
	if !ok {
		httpapi.Fail(w, 400, "Check the window.")
	}
	return window, now, ok
}

var (
	errMonitoringBusy    = errors.New("monitoring busy")
	errMonitoringTooSlow = errors.New("monitoring too slow")
)

// bounded runs a Monitoring read within its bounds: a slot for the admin
// (httpapi.Summaries), then the deadline, each signal and section of a
// page on its own budget within it (monitor/sections.go). Too slow only
// when nothing at all was read (monitor.ErrNothingRead).
func bounded[T any](ctx context.Context, s *httpapi.Server, admin string, read func(context.Context) (T, error)) (T, error) {
	var zero T
	release, ok := s.Summaries.Acquire(ctx, admin)
	if !ok {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		return zero, errMonitoringBusy
	}
	defer release()
	ctxBounded, cancel := context.WithTimeout(ctx, s.Summaries.Deadline)
	defer cancel()
	out, err := read(ctxBounded)
	if err != nil && ctx.Err() == nil && (ctxBounded.Err() != nil || errors.Is(err, monitor.ErrNothingRead)) {
		return zero, errMonitoringTooSlow
	}
	return out, err
}

// failBounded answers a read that failed (busy, too slow, or an error),
// reporting whether it did. An admin who went away gets no 500 and no
// grouped error (httpapi.ClientGone).
func failBounded(w http.ResponseWriter, r *http.Request, err error) bool {
	if err != nil && r.Context().Err() != nil {
		err = errors.Join(r.Context().Err(), err)
	}
	switch {
	case errors.Is(err, errMonitoringBusy):
		w.Header().Set("Retry-After", "1")
		httpapi.Fail(w, 503, monitoringBusy)
	case errors.Is(err, errMonitoringTooSlow):
		httpapi.Fail(w, 503, monitoringTooSlow)
	case err != nil:
		httpapi.InternalError(w, r, err)
	default:
		return false
	}
	return true
}
