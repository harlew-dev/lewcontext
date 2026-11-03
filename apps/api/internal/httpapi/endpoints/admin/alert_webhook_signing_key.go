package admin

import (
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/store"
	"net/http"
)

// AlertWebhookSigningKey is one webhook's signing key,
// /api/admin/alert-webhooks/{webhook}/signing-key: read by an operator inside
// a step-up window to copy into the receiver, or rotated. Every webhook has a
// key of its own; the key is never in a list, an event, a log or an error.
type AlertWebhookSigningKey struct{ *httpapi.Server }

// Show reads the key and its public ID: 200 {key, keyId}, `no-store`,
// recorded as admin.alert_webhook.viewed (section signing_key, never the
// key). 404 unknown or a heartbeat. GET .../signing-key.
func (h AlertWebhookSigningKey) Show(w http.ResponseWriter, r *http.Request) {
	id, ok := targetWebhook(w, r)
	if !ok {
		return
	}
	sealer, err := auth.NewDestinationSealer(h.Config.AppKey)
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	key, keyID, err := h.Repo.FindAlertWebhookSigningKey(r.Context(), id, sealer)
	if failWebhook(w, err) {
		return
	}
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	admin := httpapi.Admin(r).User
	if err := h.Repo.Record(r.Context(), events.AdminAlertWebhookViewed{Admin: admin.PublicID, Webhook: id, Section: "signing_key"}, store.EventScope{ActorID: admin.ID}); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpapi.JSON(w, 200, map[string]string{"key": key, "keyId": keyID})
}

// Store rotates the key: 201 with the webhook. For 15 minutes each post
// carries a second signature made with the previous key. Body {revision,
// reason}; 409 when stale or a rotation is still overlapping. POST
// .../signing-key.
func (h AlertWebhookSigningKey) Store(w http.ResponseWriter, r *http.Request) {
	id, ok := targetWebhook(w, r)
	if !ok {
		return
	}
	var in struct {
		Revision *int64 `json:"revision"`
		Reason   string `json:"reason"`
	}
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
	sealer, err := auth.NewDestinationSealer(h.Config.AppKey)
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	out, err := h.Repo.UpdateAlertWebhookKey(r.Context(), id, *in.Revision, sealer, action)
	if failWebhook(w, err) {
		return
	}
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpapi.JSON(w, 201, out)
}
