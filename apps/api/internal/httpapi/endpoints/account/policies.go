package account

import (
	"lewcontext/api/internal/httpapi"
	"net/http"
)

// Policies is the terms of service and privacy policy, a public singleton:
// whether they're published (POLICIES) and their current versions, which a
// sign-up sends back to agree to them. /api/policies lives in the account
// area with the other signed-out routes (docs/naming.md).
type Policies struct{ *httpapi.Server }

// Show answers {enabled: false}, or {enabled: true} with both versions.
// Answered from memory, so it isn't limited. GET /api/policies.
func (h Policies) Show(w http.ResponseWriter, r *http.Request) {
	if !h.Policies.Set() {
		httpapi.JSON(w, 200, map[string]any{"enabled": false})
		return
	}
	httpapi.JSON(w, 200, map[string]any{
		"enabled":        true,
		"termsOfService": h.Policies.TermsOfService,
		"privacyPolicy":  h.Policies.PrivacyPolicy,
	})
}
