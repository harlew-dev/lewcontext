package account

import (
	"lewcontext/api/internal/httpapi"
	"net/http"
)

type ApiKeys struct{ *httpapi.Server }

func (h ApiKeys) Index(w http.ResponseWriter, r *http.Request) { h.IndexManagedApiKeys(w, r, false) }
func (h ApiKeys) Store(w http.ResponseWriter, r *http.Request) {
	h.SaveManagedApiKey(w, r, false, true)
}
func (h ApiKeys) Update(w http.ResponseWriter, r *http.Request) {
	h.SaveManagedApiKey(w, r, false, false)
}
func (h ApiKeys) Destroy(w http.ResponseWriter, r *http.Request) { h.RevokeManagedApiKey(w, r, false) }
