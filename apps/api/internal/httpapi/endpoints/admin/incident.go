package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
)

func incidentTarget(w http.ResponseWriter, r *http.Request, kind string) (store.IncidentTarget, bool) {
	id := r.PathValue(kind)
	prefix := "err"
	if kind == "alert" {
		prefix = "alt"
	}
	if !input.PublicID(prefix, id) {
		httpapi.Fail(w, 404, "Incident not found.")
		return store.IncidentTarget{}, false
	}
	return store.IncidentTarget{Kind: kind, PublicID: id}, true
}

func failIncident(w http.ResponseWriter, r *http.Request, err error) bool {
	var quota store.QuotaError
	switch {
	case err == nil:
		return false
	case errors.Is(err, sql.ErrNoRows):
		httpapi.Fail(w, 404, "Incident or comment not found.")
	case errors.Is(err, store.ErrNoCursor):
		httpapi.Fail(w, 400, "That page is no longer available.")
	case errors.Is(err, store.ErrIncidentConflict):
		httpapi.Fail(w, 409, "This incident or comment changed. Reload before trying again.")
	case errors.As(err, &quota):
		httpapi.Fail(w, 422, "An incident cycle can have up to 1,000 comments.")
	default:
		httpapi.InternalError(w, r, err)
	}
	return true
}

func recordIncidentRead(s *httpapi.Server, r *http.Request, target store.IncidentTarget, section, cycle, before string) error {
	admin := httpapi.Admin(r).User.PublicID
	if target.Kind == "alert" {
		return s.Repo.Record(r.Context(), events.AdminAlertViewed{Admin: admin, Alert: target.PublicID, Section: section, Cycle: cycle, Before: before}, store.EventScope{})
	}
	return s.Repo.Record(r.Context(), events.AdminErrorViewed{Admin: admin, Error: target.PublicID, Section: section, Cycle: cycle, Before: before}, store.EventScope{})
}

func indexIncidentCycles(s *httpapi.Server, w http.ResponseWriter, r *http.Request, kind string) {
	target, ok := incidentTarget(w, r, kind)
	if !ok {
		return
	}
	params, ok := input.Query(r, "before", "perPage")
	if !ok {
		httpapi.Fail(w, 400, "Check the query parameters.")
		return
	}
	perPage, ok := input.Int(params["perPage"], 20, 100)
	if !ok || (params["before"] != "" && !input.PublicID("inc", params["before"])) {
		httpapi.Fail(w, 400, "Check the page.")
		return
	}
	page, err := s.Repo.IndexIncidentCycles(r.Context(), target, params["before"], perPage)
	if failIncident(w, r, err) {
		return
	}
	if err := recordIncidentRead(s, r, target, "cycles", "", params["before"]); failIncident(w, r, err) {
		return
	}
	httpapi.JSON(w, 200, page)
}

func indexIncidentMessages(s *httpapi.Server, w http.ResponseWriter, r *http.Request, kind string) {
	target, ok := incidentTarget(w, r, kind)
	if !ok {
		return
	}
	params, ok := input.Query(r, "cycle", "before", "perPage")
	if !ok {
		httpapi.Fail(w, 400, "Check the query parameters.")
		return
	}
	perPage, ok := input.Int(params["perPage"], 20, 100)
	if !ok || !input.PublicID("inc", params["cycle"]) || (params["before"] != "" && !input.PublicID("msg", params["before"])) {
		httpapi.Fail(w, 400, "Choose a cycle and valid page.")
		return
	}
	page, err := s.Repo.IndexIncidentMessages(r.Context(), target, params["cycle"], params["before"], perPage)
	if failIncident(w, r, err) {
		return
	}
	if err := recordIncidentRead(s, r, target, "messages", params["cycle"], params["before"]); failIncident(w, r, err) {
		return
	}
	httpapi.JSON(w, 200, page)
}

func storeIncidentMessage(s *httpapi.Server, w http.ResponseWriter, r *http.Request, kind string) {
	target, ok := incidentTarget(w, r, kind)
	if !ok {
		return
	}
	var in struct {
		Cycle string `json:"cycle"`
		Body  string `json:"body"`
	}
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !input.PublicID("inc", in.Cycle) {
		httpapi.Fail(w, 404, "Incident not found.")
		return
	}
	in.Body = strings.TrimSpace(in.Body)
	if n := utf8.RuneCountInString(in.Body); n < 1 || n > 2000 || !input.ValidText(in.Body, true) {
		httpapi.FailField(w, 422, "body", "Write a comment of 1 to 2,000 characters without hidden control characters.")
		return
	}
	saved, err := s.Repo.CreateIncidentMessage(r.Context(), target, in.Cycle, in.Body, httpapi.Admin(r).User)
	if failIncident(w, r, err) {
		return
	}
	httpapi.JSON(w, 201, saved)
}

func storeIncidentRedaction(s *httpapi.Server, w http.ResponseWriter, r *http.Request, kind string) {
	target, ok := incidentTarget(w, r, kind)
	if !ok {
		return
	}
	message := r.PathValue("message")
	if !input.PublicID("msg", message) {
		httpapi.Fail(w, 404, "Comment not found.")
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
	saved, err := s.Repo.UpdateIncidentMessageRedaction(r.Context(), target, message, action)
	if failIncident(w, r, err) {
		return
	}
	httpapi.JSON(w, 201, saved)
}
