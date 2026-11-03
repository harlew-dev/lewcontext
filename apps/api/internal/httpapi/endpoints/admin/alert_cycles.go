package admin

import (
	"lewcontext/api/internal/httpapi"
	"net/http"
)

// AlertCycles reads the scoped, cursor-paged incident history.
type AlertCycles struct{ *httpapi.Server }

func (h AlertCycles) Index(w http.ResponseWriter, r *http.Request) {
	indexIncidentCycles(h.Server, w, r, "alert")
}
