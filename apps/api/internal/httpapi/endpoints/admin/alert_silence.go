package admin

import (
	"lewcontext/api/internal/httpapi"
	"net/http"
	"time"
)

// AlertSilence is an alert's silence, /api/admin/alerts/{alert}/silence
// (docs/api/monitoring.md#alerts): for planned maintenance or a known
// incident, no email or webhook about the alert's signal for up to 24
// hours, nor about another alert of it opening meanwhile. Silencing hides
// what may be a security alert, so it's an account-and-access action
// (plans/features/implemented/admin/actions.md): the operator role, a
// fresh code and a reason.
type AlertSilence struct{ *httpapi.Server }

// alertSilences are the lengths a silence may have, by the name the API
// takes: 24 hours at most (store.MaxAlertSilence).
var alertSilences = map[string]time.Duration{
	"1h":  time.Hour,
	"4h":  4 * time.Hour,
	"24h": 24 * time.Hour,
}

// silenceInput is Store's body: how long, and why.
type silenceInput struct {
	Duration string `json:"duration"`
	Reason   string `json:"reason"`
}

// Store silences the open alert's signal for duration (1h, 4h or 24h),
// from now, recording admin.alert.silenced: 201 with the alert. Silencing
// again sets a new end. 422 on duration or reason; 404 unknown; 409 when
// it's resolved. POST /api/admin/alerts/{alert}/silence.
func (h AlertSilence) Store(w http.ResponseWriter, r *http.Request) {
	id, ok := targetAlert(w, r)
	if !ok {
		return
	}
	var in silenceInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	d, ok := alertSilences[in.Duration]
	if !ok {
		httpapi.FailField(w, 422, "duration", "Pick how long to silence it for.")
		return
	}
	action, ok := adminAction(w, r, in.Reason)
	if !ok {
		return
	}
	alert, err := h.Repo.SilenceAlert(r.Context(), id, d, action)
	if failAlert(w, err) {
		return
	}
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 201, viewOf(alert))
}
