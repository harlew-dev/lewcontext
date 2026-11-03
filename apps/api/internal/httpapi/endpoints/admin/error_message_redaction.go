package admin

import (
	"lewcontext/api/internal/httpapi"
	"net/http"
)

// ErrorMessageRedaction creates an audited tombstone, behind operator step-up.
type ErrorMessageRedaction struct{ *httpapi.Server }

func (h ErrorMessageRedaction) Store(w http.ResponseWriter, r *http.Request) {
	storeIncidentRedaction(h.Server, w, r, "error")
}
