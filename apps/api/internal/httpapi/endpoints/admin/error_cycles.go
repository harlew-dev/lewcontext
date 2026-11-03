package admin

import (
	"lewcontext/api/internal/httpapi"
	"net/http"
)

// ErrorCycles reads the scoped, cursor-paged incident history.
type ErrorCycles struct{ *httpapi.Server }

func (h ErrorCycles) Index(w http.ResponseWriter, r *http.Request) {
	indexIncidentCycles(h.Server, w, r, "error")
}
