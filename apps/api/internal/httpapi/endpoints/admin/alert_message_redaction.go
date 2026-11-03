package admin

import (
	"lewcontext/api/internal/httpapi"
	"net/http"
)

// AlertMessageRedaction creates an audited tombstone, behind operator step-up.
type AlertMessageRedaction struct{ *httpapi.Server }

func (h AlertMessageRedaction) Store(w http.ResponseWriter, r *http.Request) {
	storeIncidentRedaction(h.Server, w, r, "alert")
}
