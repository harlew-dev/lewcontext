package admin

import (
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
	"slices"
	"strings"
)

// Deliveries are the admin dashboard's /api/admin/deliveries.
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
