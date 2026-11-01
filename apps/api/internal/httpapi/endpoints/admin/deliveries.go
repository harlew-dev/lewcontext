package admin

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
	"slices"
	"strings"
)

// Deliveries are the admin dashboard's /api/admin/deliveries: the list,
// and queuing one failed delivery again (Retry).
type Deliveries struct{ *httpapi.Server }

// deliveryCursor reads a deliveries cursor, evt_…:listener.
func deliveryCursor(raw string) (event, listener string, ok bool) {
	event, listener, found := strings.Cut(raw, ":")
	return event, listener, found && input.PublicID("evt", event) && listener != "" && len(listener) <= 100
}

// adminDeliveryQuery parses the deliveries list's query string.
func (h Deliveries) query(r *http.Request) (store.AdminDeliveryFilter, int, events.DeliveryFilters, bool) {
	var f store.AdminDeliveryFilter
	params, ok := input.Query(r, "status", "listener", "from", "to", "before", "perPage")
	if !ok {
		return f, 0, events.DeliveryFilters{}, false
	}
	perPage, ok := input.Int(params["perPage"], defaultEventsPerPage, maxEventsPerPage)
	valid := ok
	f.Status, f.Listener = params["status"], params["listener"]
	valid = valid && (f.Status == "" || store.DeliveryStatus(f.Status))
	valid = valid && (f.Listener == "" || slices.Contains(listeners(h.Server), f.Listener))
	if params["before"] != "" {
		f.BeforeEvent, f.BeforeListener, ok = deliveryCursor(params["before"])
		valid = valid && ok
	}
	f.From, ok = timeFilter(params["from"])
	valid = valid && ok
	f.To, ok = timeFilter(params["to"])
	valid = valid && ok
	used := events.DeliveryFilters{Status: f.Status, Listener: f.Listener, From: f.From, To: f.To, Before: params["before"]}
	return f, perPage, used, valid
}

func (h Deliveries) Index(w http.ResponseWriter, r *http.Request) {
	f, perPage, used, ok := h.query(r)
	if !ok {
		httpapi.Fail(w, 400, "Check the filters.")
		return
	}
	page, err := h.Repo.IndexAdminDeliveries(r.Context(), f, perPage)
	if errors.Is(err, store.ErrNoCursor) {
		httpapi.Fail(w, 400, "That page is no longer available. Start from the newest deliveries.")
		return
	}
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	e := events.AdminDeliveryListed{Admin: httpapi.Admin(r).User.PublicID, Filters: used}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 200, page)
}

// Retry queues the failed delivery of {event} to {listener} again: not
// failed, no attempts yet, due now. Its listener runs as for a new event,
// so a reset, invitation or sign-up email gets a fresh link or code, never
// the old one, and Send still refuses a blocked address. 404 for an
// unknown delivery (or listener), 409 when it hasn't failed. Recorded as
// admin.delivery.retried. POST /api/admin/deliveries/{event}/{listener}/retry.
func (h Deliveries) Retry(w http.ResponseWriter, r *http.Request) {
	event, listener := r.PathValue("event"), r.PathValue("listener")
	if !input.PublicID("evt", event) || !slices.Contains(listeners(h.Server), listener) {
		httpapi.Fail(w, 404, "Delivery not found.")
		return
	}
	var in destroyInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	action, ok := adminAction(w, r, in.Reason)
	if !ok {
		return
	}
	err := h.Repo.RequeueDelivery(r.Context(), event, listener, action)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		httpapi.Fail(w, 404, "Delivery not found.")
	case errors.Is(err, store.ErrDeliveryNotFailed):
		httpapi.Fail(w, 409, "This delivery hasn’t failed.")
	case err != nil:
		httpapi.InternalError(w, r, err)
	default:
		w.WriteHeader(204)
	}
}
