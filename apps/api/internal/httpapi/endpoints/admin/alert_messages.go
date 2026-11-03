package admin

import (
	"lewcontext/api/internal/httpapi"
	"net/http"
)

// AlertMessages is the viewer-write discussion resource, never a lifecycle action.
type AlertMessages struct{ *httpapi.Server }

func (h AlertMessages) Index(w http.ResponseWriter, r *http.Request) {
	indexIncidentMessages(h.Server, w, r, "alert")
}
func (h AlertMessages) Store(w http.ResponseWriter, r *http.Request) {
	storeIncidentMessage(h.Server, w, r, "alert")
}
