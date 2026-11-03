package account

import (
	"lewcontext/api/internal/httpapi"
	"net/http"
)

// ApiKeyEvents is the log of one personal API key, read by its owner:
// /api/auth/api-keys/{key}/events. It is that key's own events only (its
// lifecycle and what it wrote), in a workspace the owner still belongs to,
// and needs no workspace permission.
type ApiKeyEvents struct{ *httpapi.Server }

func (h ApiKeyEvents) Index(w http.ResponseWriter, r *http.Request) { h.IndexOwnApiKeyLog(w, r) }
