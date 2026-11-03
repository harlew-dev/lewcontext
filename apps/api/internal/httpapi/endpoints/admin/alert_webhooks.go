package admin

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/monitor"
	"lewcontext/api/internal/store"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"
)

// AlertWebhooks are the alert receivers operators manage in the dashboard,
// /api/admin/alert-webhooks (docs/api/monitoring.md): webhooks, each with the
// signals and event kinds it receives and a signing key of its own, and
// heartbeats, pinged after each good evaluation. Reading is any admin's;
// every change is an operator's action (a fresh code and a reason). A
// response never holds a receiver's URL, host or key.
type AlertWebhooks struct{ *httpapi.Server }

// alertWebhookInput is Store's and Update's body. A missing field is left
// as it is on an update.
type alertWebhookInput struct {
	Kind          string    `json:"kind"`
	Revision      *int64    `json:"revision"`
	Reason        string    `json:"reason"`
	Name          *string   `json:"name"`
	URL           *string   `json:"url"`
	Format        *string   `json:"format"`
	Signals       *[]string `json:"signals"`
	Events        *[]string `json:"events"`
	Enabled       *bool     `json:"enabled"`
	RevokeOverlap bool      `json:"revokeOverlap"`
}

// Index lists every webhook and heartbeat with its latest delivery: 200
// {data}; recorded as admin.alert_webhook.listed. GET /api/admin/alert-webhooks.
func (h AlertWebhooks) Index(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) > 0 {
		httpapi.Fail(w, 400, "This resource takes no query parameters.")
		return
	}
	out, err := h.Repo.IndexAlertWebhooks(r.Context())
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	admin := httpapi.Admin(r).User
	if err := h.Repo.Record(r.Context(), events.AdminAlertWebhookListed{Admin: admin.PublicID}, store.EventScope{ActorID: admin.ID}); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"data": out})
}

// validate checks the fields a body gave, answering 422 on the first wrong
// one. kind is "" on an update, whose receiver's own kind the store checks.
func (h AlertWebhooks) validate(w http.ResponseWriter, in *alertWebhookInput, kind string) bool {
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" || utf8.RuneCountInString(name) > 80 || !input.ValidText(name, false) {
			httpapi.FailField(w, 422, "name", "Give a name of 1 to 80 characters.")
			return false
		}
		in.Name = &name
	}
	if in.URL != nil {
		if err := config.ValidateAlertURL(strings.TrimSpace(*in.URL), h.Config.Production, h.Config.AlertAllowPrivateTargets); err != nil {
			httpapi.FailField(w, 422, "url", err.Error())
			return false
		}
		url := strings.TrimSpace(*in.URL)
		in.URL = &url
	}
	if in.Format != nil && *in.Format != "json" && *in.Format != "slack" && !(kind == "heartbeat" && *in.Format == "") {
		httpapi.FailField(w, 422, "format", "Pick json or slack.")
		return false
	}
	if in.Signals != nil {
		known := map[string]bool{}
		for _, rule := range monitor.AlertRules {
			known[rule.Signal] = true
		}
		for _, s := range *in.Signals {
			if !known[s] {
				httpapi.FailField(w, 422, "signals", "Pick signals from the catalogue.")
				return false
			}
		}
	}
	if in.Events != nil {
		for _, e := range *in.Events {
			if !slices.Contains(events.AlertWebhookKinds, e) {
				httpapi.FailField(w, 422, "events", "Pick opened, escalated, recovered, reopened, resolved or reminded.")
				return false
			}
		}
	}
	return true
}

// failWebhook answers the refusals every webhook write shares, or reports
// false for anything else.
func failWebhook(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		httpapi.Fail(w, 404, "Receiver not found.")
	case errors.Is(err, store.ErrAlertWebhookConflict):
		httpapi.Fail(w, 409, err.Error())
	case errors.Is(err, store.ErrAlertWebhookName):
		httpapi.FailField(w, 422, "name", "Another receiver has that name.")
	case errors.Is(err, store.ErrAlertWebhookLimit):
		httpapi.Fail(w, 422, "There are too many receivers of that type. Delete one first.")
	case errors.Is(err, store.ErrAlertWebhookInvalid):
		httpapi.Fail(w, 422, "Check the receiver's settings.")
	default:
		return false
	}
	return true
}

// targetWebhook is the {webhook} in the path, answering 404 unless it's
// shaped like a public ID.
func targetWebhook(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("webhook")
	if !input.PublicID("whk", id) {
		httpapi.Fail(w, 404, "Receiver not found.")
		return "", false
	}
	return id, true
}

// Store adds a webhook or heartbeat: 201 with it, never its URL or key (a
// webhook's key is read with AlertWebhookSigningKey.Show). Body {kind, name,
// url, format (webhooks), signals and events (webhooks), enabled, reason}.
// An empty signals or events list receives nothing. 422 on a field, a name in
// use, or too many receivers. POST /api/admin/alert-webhooks.
func (h AlertWebhooks) Store(w http.ResponseWriter, r *http.Request) {
	var in alertWebhookInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	action, ok := adminAction(w, r, in.Reason)
	if !ok {
		return
	}
	if in.Kind != "webhook" && in.Kind != "heartbeat" {
		httpapi.FailField(w, 422, "kind", "Pick webhook or heartbeat.")
		return
	}
	if in.Name == nil {
		httpapi.FailField(w, 422, "name", "Give the receiver a name.")
		return
	}
	if in.URL == nil {
		httpapi.FailField(w, 422, "url", "Enter the receiver's URL.")
		return
	}
	if in.Kind == "webhook" && in.Format == nil {
		httpapi.FailField(w, 422, "format", "Pick json or slack.")
		return
	}
	if in.Kind == "heartbeat" && (in.Signals != nil && len(*in.Signals) > 0 || in.Events != nil && len(*in.Events) > 0 || in.Format != nil && *in.Format != "") {
		httpapi.Fail(w, 422, "A heartbeat has no signals, events or format.")
		return
	}
	if !h.validate(w, &in, in.Kind) {
		return
	}
	sealer, err := auth.NewDestinationSealer(h.Config.AppKey)
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	out, err := h.Repo.CreateAlertWebhook(r.Context(), store.AlertWebhookInput{Kind: in.Kind, Name: in.Name, URL: in.URL, Format: in.Format,
		Signals: in.Signals, Events: in.Events, Enabled: in.Enabled}, sealer, action)
	if failWebhook(w, err) {
		return
	}
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 201, out)
}

// Update changes a receiver: 200 with it. Body {revision, reason} and the
// fields to change. Replacing the URL gives a webhook a new key; replacing the
// URL or format skips its queued notices. 409 when the revision is stale,
// 422 on a field or a name in use, 404 unknown. PATCH /api/admin/alert-webhooks/{webhook}.
func (h AlertWebhooks) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := targetWebhook(w, r)
	if !ok {
		return
	}
	var in alertWebhookInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	action, ok := adminAction(w, r, in.Reason)
	if !ok {
		return
	}
	if in.Revision == nil || *in.Revision < 1 {
		httpapi.FailField(w, 422, "revision", "Give the receiver's current revision.")
		return
	}
	if in.Kind != "" {
		httpapi.FailField(w, 422, "kind", "A receiver's type can't change.")
		return
	}
	if in.Name == nil && in.URL == nil && in.Format == nil && in.Signals == nil && in.Events == nil && in.Enabled == nil && !in.RevokeOverlap {
		httpapi.Fail(w, 422, "Choose something to change.")
		return
	}
	if !h.validate(w, &in, "") {
		return
	}
	sealer, err := auth.NewDestinationSealer(h.Config.AppKey)
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	out, err := h.Repo.UpdateAlertWebhook(r.Context(), id, store.AlertWebhookInput{Revision: *in.Revision, Name: in.Name, URL: in.URL, Format: in.Format,
		Signals: in.Signals, Events: in.Events, Enabled: in.Enabled, RevokeOverlap: in.RevokeOverlap}, sealer, action)
	if failWebhook(w, err) {
		return
	}
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 200, out)
}

// Destroy deletes a receiver; its queued notices skip. Body {reason}: 204;
// 404 unknown. DELETE /api/admin/alert-webhooks/{webhook}.
func (h AlertWebhooks) Destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := targetWebhook(w, r)
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if !httpapi.Decode(w, r, &in) {
		return
	}
	action, ok := adminAction(w, r, in.Reason)
	if !ok {
		return
	}
	err := h.Repo.DeleteAlertWebhook(r.Context(), id, action)
	if failWebhook(w, err) {
		return
	}
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	w.WriteHeader(204)
}
