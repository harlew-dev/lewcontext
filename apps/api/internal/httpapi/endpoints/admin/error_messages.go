package admin

import (
	"lewcontext/api/internal/httpapi"
	"net/http"
)

// ErrorMessages is the viewer-write discussion resource, never a lifecycle action.
type ErrorMessages struct{ *httpapi.Server }

func (h ErrorMessages) Index(w http.ResponseWriter, r *http.Request) {
	indexIncidentMessages(h.Server, w, r, "error")
}
func (h ErrorMessages) Store(w http.ResponseWriter, r *http.Request) {
	storeIncidentMessage(h.Server, w, r, "error")
}
