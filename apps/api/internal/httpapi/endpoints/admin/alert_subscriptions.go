package admin

import (
	"errors"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/monitor"
	"lewcontext/api/internal/store"
	"net/http"
	"unicode/utf8"
)

// AlertSubscriptions is the signed-in admin's own alert email,
// /api/admin/alert-subscriptions: opt-in, so nothing is emailed until they
// subscribe to a signal. It touches only their own inbox, so any admin may
// (Role viewer), with no code. Reading it is the catalogue's (AlertRules.Index
// carries each rule's own preference).
type AlertSubscriptions struct{ *httpapi.Server }

type alertSubscriptionsInput struct {
	Signals []store.AlertSubscription `json:"signals"`
}

// Update subscribes the admin to signals, or unsubscribes (minLevel off), as
// a bounded patch: the signals not listed are unchanged. 204; recorded as one
// admin.updated when something changed. 422 on a signal or level. PATCH
// /api/admin/alert-subscriptions.
func (h AlertSubscriptions) Update(w http.ResponseWriter, r *http.Request) {
	var in alertSubscriptionsInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if len(in.Signals) == 0 {
		httpapi.Fail(w, 422, "Choose a signal to change.")
		return
	}
	if len(in.Signals) > 100 {
		httpapi.FailField(w, 422, "signals", "Change up to 100 signals at once.")
		return
	}
	seen := map[string]bool{}
	for _, p := range in.Signals {
		if seen[p.Signal] || p.Signal == "" || utf8.RuneCountInString(p.Signal) > 100 || (p.MinLevel != "warn" && p.MinLevel != "critical" && p.MinLevel != "off") {
			httpapi.FailField(w, 422, "signals", "Check the signal names and levels.")
			return
		}
		seen[p.Signal] = true
	}
	known := map[string]bool{}
	for _, rule := range monitor.AlertRules {
		known[rule.Signal] = true
	}
	err := h.Repo.UpdateAdminAlertSubscriptions(r.Context(), httpapi.Admin(r).User, in.Signals, known)
	if errors.Is(err, store.ErrAlertSubscription) {
		httpapi.FailField(w, 422, "signals", "Pick signals from the catalogue, at warn or critical.")
		return
	}
	if errors.Is(err, store.ErrNotAdmin) {
		httpapi.Fail(w, 401, "Sign in again.")
		return
	}
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	w.WriteHeader(204)
}
