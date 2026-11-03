package admin

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/monitor"
	"lewcontext/api/internal/store"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Alerts are the admin dashboard's /api/admin/alerts
// (docs/api/monitoring.md#alerts): the alerts the evaluator opened, each
// read recorded (admin.alert.listed, .viewed), and acknowledging one, a
// housekeeping action (the operator role, no code: it only stops the
// reminders).
type Alerts struct{ *httpapi.Server }

// alertsPerPage is a page's length by default, and its most.
const (
	alertsPerPage    = 50
	maxAlertsPerPage = 100
)

// alertView is an alert as the dashboard shows it: the stored alert, and
// what its signal is (its title, unit, page and thresholds, and the
// rule's meaning), read from the definitions in code.
type alertView struct {
	store.Alert
	Title    string  `json:"title"`
	Unit     string  `json:"unit"`
	Page     string  `json:"page"`
	Warn     float64 `json:"warn,omitempty"`
	Critical float64 `json:"critical,omitempty"`
	Meaning  string  `json:"meaning"`
	// ForSeconds is how long its signal must stay at a level to open one.
	ForSeconds int  `json:"forSeconds"`
	Hourly     bool `json:"hourly"`
}

// viewOf is a as the dashboard shows it. A signal no longer defined is
// shown by its name.
func viewOf(a store.Alert) alertView {
	v := alertView{Alert: a, Title: a.Signal, Unit: monitor.UnitCount}
	if sig, ok := monitor.FindSignal(a.Signal); ok {
		v.Title, v.Unit, v.Page, v.Warn, v.Critical = sig.Label, sig.Unit, sig.Page, sig.Warn, sig.Critical
	}
	if rule, ok := monitor.FindAlertRule(a.Signal); ok {
		v.Meaning, v.ForSeconds, v.Hourly = rule.Meaning, int(rule.For/time.Second), rule.Hourly
		if rule.Title != "" {
			v.Title = rule.Title
		}
	}
	return v
}

// alertPage is Index's answer.
type alertPage struct {
	Data []alertView `json:"data"`
	Next *string     `json:"next"`
}

// Index lists alerts: ?state=open (the default: every open alert, critical
// first), resolved or all (newest first, a cursor page: before, perPage).
// Recorded as admin.alert.listed. GET /api/admin/alerts.
func (h Alerts) Index(w http.ResponseWriter, r *http.Request) {
	params, ok := input.Query(r, "state", "before", "perPage", "q", "level", "health", "signals", "acknowledged", "silenced", "from", "to")
	if !ok {
		httpapi.Fail(w, 400, "Check the filters.")
		return
	}
	f := store.AlertFilter{State: params["state"], Before: params["before"], Search: strings.TrimSpace(params["q"]), Level: params["level"],
		Health: params["health"], Acknowledged: params["acknowledged"], Silenced: params["silenced"]}
	var okSearch, okFrom, okTo, okSignals bool
	okSearch = utf8.RuneCountInString(f.Search) <= 100 && input.ValidText(f.Search, false)
	f.From, okFrom = timeFilter(params["from"])
	f.To, okTo = timeFilter(params["to"])
	f.Signals, okSignals = alertSignals(params["signals"])
	if f.State == "" {
		f.State = store.AlertStateOpen
	}
	perPage, okPage := input.Int(params["perPage"], alertsPerPage, maxAlertsPerPage)
	switch {
	case !okPage, !okSearch, !okFrom, !okTo, !okSignals,
		f.Level != "" && f.Level != "warn" && f.Level != "critical",
		f.Health != "" && !slices.Contains([]string{"active", "confirming", "recovered", "unknown"}, f.Health),
		f.Acknowledged != "" && f.Acknowledged != "yes" && f.Acknowledged != "no",
		f.Silenced != "" && f.Silenced != "yes" && f.Silenced != "no",
		f.From != nil && f.To != nil && !f.From.Before(*f.To),
		f.State != store.AlertStateOpen && f.State != store.AlertStateResolved && f.State != store.AlertStateAll,
		f.Before != "" && !input.PublicID("alt", f.Before):
		httpapi.Fail(w, 400, "Check the filters.")
		return
	}
	page, err := h.Repo.IndexAlerts(r.Context(), f, perPage)
	if errors.Is(err, store.ErrNoCursor) {
		httpapi.Fail(w, 400, "That page is no longer available. Start from the newest alerts.")
		return
	}
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	e := events.AdminAlertListed{Admin: httpapi.Admin(r).User.PublicID, State: f.State, Before: f.Before, Filters: events.AlertListFilters{
		Search: f.Search, Level: f.Level, Health: f.Health, Signals: f.Signals, Acknowledged: f.Acknowledged, Silenced: f.Silenced, From: f.From, To: f.To}}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	out := alertPage{Data: make([]alertView, len(page.Data)), Next: page.Next}
	for i, a := range page.Data {
		out.Data[i] = viewOf(a)
	}
	httpapi.JSON(w, 200, out)
}

// maxAlertSignalFilters bounds the signals one list request may name.
const maxAlertSignalFilters = 50

// alertSignals reads a comma-separated list of alert signals, each one in the
// catalogue, without repeats.
func alertSignals(raw string) ([]string, bool) {
	if raw == "" {
		return nil, true
	}
	list := strings.Split(raw, ",")
	if len(list) > maxAlertSignalFilters {
		return nil, false
	}
	seen := map[string]bool{}
	for _, s := range list {
		if _, known := monitor.FindAlertRule(s); !known || seen[s] {
			return nil, false
		}
		seen[s] = true
	}
	return list, true
}

// Show is one alert, recorded as admin.alert.viewed. 404 for an unknown
// one, or an ID of another shape. GET /api/admin/alerts/{alert}.
func (h Alerts) Show(w http.ResponseWriter, r *http.Request) {
	if _, ok := input.Query(r); !ok {
		httpapi.Fail(w, 400, "Invalid query parameters.")
		return
	}
	id, ok := targetAlert(w, r)
	if !ok {
		return
	}
	alert, err := h.Repo.FindAlert(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, 404, "Alert not found.")
		return
	}
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	if err := h.Repo.Record(r.Context(), events.AdminAlertViewed{Admin: httpapi.Admin(r).User.PublicID, Alert: id}, store.EventScope{}); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 200, viewOf(alert))
}

// acknowledgeInput is Update's body: {"acknowledged": true}, the one
// change it makes.
type acknowledgeInput struct {
	Acknowledged bool   `json:"acknowledged"`
	Resolved     bool   `json:"resolved"`
	Cycle        string `json:"cycle"`
	Message      string `json:"message"`
	Revision     int64  `json:"revision"`
}

// Update acknowledges an open alert ({"acknowledged": true}), which stops
// its 4-hourly reminders, recording admin.alert.acknowledged: 200 with the
// alert. 422 without acknowledged true; 404 unknown; 409 when it's
// resolved or acknowledged already. A housekeeping action: the operator
// role and admin-action:per-user, no code or reason.
// PATCH /api/admin/alerts/{alert}.
func (h Alerts) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := targetAlert(w, r)
	if !ok {
		return
	}
	var in acknowledgeInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if in.Acknowledged == in.Resolved || (in.Acknowledged && (in.Cycle != "" || in.Message != "" || in.Revision != 0)) || (in.Resolved && (!input.PublicID("inc", in.Cycle) || !input.PublicID("msg", in.Message) || in.Revision < 1)) {
		httpapi.FailField(w, 422, "resolved", "Choose one action and a current-cycle comment and revision for resolution.")
		return
	}
	var alert store.Alert
	var err error
	if in.Resolved {
		alert, err = h.Repo.ResolveRecoveredAlert(r.Context(), id, store.AlertResolution{Cycle: in.Cycle, Message: in.Message, Revision: in.Revision}, httpapi.Admin(r).User)
	} else {
		alert, err = h.Repo.AcknowledgeAlert(r.Context(), id, httpapi.Admin(r).User)
	}
	if failAlert(w, err) {
		return
	}
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 200, viewOf(alert))
}

// targetAlert is the {alert} in the path, answering 404 unless it's shaped
// like an alert's public ID.
func targetAlert(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("alert")
	if !input.PublicID("alt", id) {
		httpapi.Fail(w, 404, "Alert not found.")
		return "", false
	}
	return id, true
}

// failAlert answers the refusals a change to an alert shares, or reports
// false.
func failAlert(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		httpapi.Fail(w, 404, "Alert not found.")
	case errors.Is(err, store.ErrAlertResolved):
		httpapi.Fail(w, 409, "This alert is resolved.")
	case errors.Is(err, store.ErrAlreadyDone):
		httpapi.Fail(w, 409, "This alert is acknowledged already.")
	case errors.Is(err, store.ErrIncidentConflict):
		httpapi.Fail(w, 409, "This incident changed or does not have fresh confirmed recovery. Reload before resolving.")
	default:
		return false
	}
	return true
}
