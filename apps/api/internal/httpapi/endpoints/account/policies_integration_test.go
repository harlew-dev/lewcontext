package account_test

import (
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"testing"
)

// currentPolicies are the versions the policy tests publish.
var currentPolicies = store.PolicyVersions{TermsOfService: "2026-11-01", PrivacyPolicy: "2026-10-15"}

func TestPoliciesEndpoint(t *testing.T) {
	s, _ := server(t)
	h := router.New(s)
	off := apitest.Call(t, h, "GET", "/api/policies", "", "", nil, 200)
	if off.Body.String() != `{"enabled":false}`+"\n" || off.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("off: %q %v", off.Body.String(), off.Header())
	}
	s.Policies = currentPolicies
	on := apitest.Call(t, h, "GET", "/api/policies", "", "", nil, 200)
	if on.Body.String() != `{"enabled":true,"privacyPolicy":"2026-10-15","termsOfService":"2026-11-01"}`+"\n" {
		t.Fatalf("on: %q", on.Body.String())
	}
}
