package web

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// The policy pages as React Router prerenders them: each page's HTML with
// its loader data as an inline script, and its .data file for in-app
// navigation; and the manifest the app build writes.
const termsScript = "window.__reactRouterContext.streamController.enqueue(\"terms\");"

func policyBuild(manifest string) fstest.MapFS {
	files := build()
	files["policy/terms-of-service/index.html"] = &fstest.MapFile{Data: []byte(`<!DOCTYPE html><html><body><h1>Terms of Service</h1>` +
		`<script>` + termsScript + `</script></body></html>`)}
	files["policy/terms-of-service.data"] = &fstest.MapFile{Data: []byte(`[{"_1":2}]`)}
	files["policy/privacy-policy/index.html"] = &fstest.MapFile{Data: []byte(`<!DOCTYPE html><html><body><h1>Privacy Policy</h1></body></html>`)}
	files["policy/privacy-policy.data"] = &fstest.MapFile{Data: []byte(`[{"_1":2}]`)}
	if manifest != "" {
		files[policiesFile] = &fstest.MapFile{Data: []byte(manifest)}
	}
	return files
}

const (
	editedManifest      = `{"termsOfService":{"version":"2026-11-01","boilerplate":false},"privacyPolicy":{"version":"2026-10-15","boilerplate":false}}`
	boilerplateManifest = `{"termsOfService":{"version":"2026-11-01","boilerplate":false},"privacyPolicy":{"version":"2026-11-01","boilerplate":true}}`
)

// Off (the default), the pages, their data files and the manifest answer
// exactly as an unknown URL does.
func TestPolicyPagesAreUnknownUntilServed(t *testing.T) {
	p := pages(t, policyBuild(editedManifest))
	unknown := serve(t, p, "GET", "/nowhere")
	for _, target := range []string{"/policy/terms-of-service", "/policy/privacy-policy", "/policy/terms-of-service.data", "/policy", "/.policies.json"} {
		w := serve(t, p, "GET", target)
		if w.Code != 404 || w.Body.String() != unknown.Body.String() || w.Header().Get("Content-Security-Policy") != unknown.Header().Get("Content-Security-Policy") ||
			w.Header().Get("Cache-Control") != unknown.Header().Get("Cache-Control") {
			t.Errorf("%s: %d %v, want the unknown page", target, w.Code, w.Header())
		}
	}
}

func TestServesPolicyPagesWhenOn(t *testing.T) {
	p := pages(t, policyBuild(editedManifest))
	got, err := p.ServePolicies()
	if err != nil {
		t.Fatal(err)
	}
	if got.TermsOfService.Version != "2026-11-01" || got.PrivacyPolicy.Version != "2026-10-15" {
		t.Fatalf("versions: %+v", got)
	}
	w := serve(t, p, "GET", "/policy/terms-of-service")
	csp := w.Header().Get("Content-Security-Policy")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Terms of Service") || w.Header().Get("Cache-Control") != "no-cache" ||
		!strings.Contains(csp, hash(termsScript)) || strings.Contains(scriptSrc(csp), "unsafe-inline") {
		t.Fatalf("terms page: %d %v %q", w.Code, w.Header(), w.Body.String())
	}
	if w := serve(t, p, "GET", "/policy/privacy-policy"); w.Code != 200 || !strings.Contains(w.Body.String(), "Privacy Policy") {
		t.Fatalf("privacy page: %d", w.Code)
	}
	if w := serve(t, p, "GET", "/policy/terms-of-service.data"); w.Code != 200 || w.Body.String() != `[{"_1":2}]` {
		t.Fatalf("data file: %d %q", w.Code, w.Body.String())
	}
	// The manifest stays a dot-file, never served.
	if w := serve(t, p, "GET", "/.policies.json"); w.Code != 404 {
		t.Fatalf("manifest served: %d", w.Code)
	}
}

func TestRefusesPoliciesWithoutTheirBuild(t *testing.T) {
	if _, err := pages(t, build()).ServePolicies(); !errors.Is(err, ErrPoliciesNotBuilt) {
		t.Fatalf("want ErrPoliciesNotBuilt, got %v", err)
	}
	for name, manifest := range map[string]string{
		"not JSON":      `{`,
		"unknown field": `{"termsOfService":{"version":"2026-11-01"},"privacyPolicy":{"version":"2026-11-01"},"cookies":{}}`,
		"bad date":      `{"termsOfService":{"version":"2026-13-01"},"privacyPolicy":{"version":"2026-11-01"}}`,
		"missing":       `{"termsOfService":{"version":"2026-11-01"}}`,
	} {
		if _, err := New(policyBuild(manifest), nil, false); err == nil {
			t.Errorf("%s: manifest accepted", name)
		}
	}
}

// The committed template is fine to read in development, never to publish.
func TestProductionRefusesTemplatePolicies(t *testing.T) {
	dev, err := New(policyBuild(boilerplateManifest), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dev.ServePolicies(); err != nil {
		t.Fatalf("development: %v", err)
	}
	prod, err := New(policyBuild(boilerplateManifest), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prod.ServePolicies(); err == nil || !strings.Contains(err.Error(), "privacy-policy.md") {
		t.Fatalf("production with a template: %v", err)
	}
	if serve(t, prod, "GET", "/policy/privacy-policy").Code != 404 {
		t.Fatal("a refused document was published")
	}
	prod, _ = New(policyBuild(editedManifest), nil, true)
	if _, err := prod.ServePolicies(); err != nil {
		t.Fatalf("production with edited documents: %v", err)
	}
}

// scriptSrc is a policy's script-src directive.
func scriptSrc(csp string) string {
	for _, directive := range strings.Split(csp, ";") {
		if strings.HasPrefix(strings.TrimSpace(directive), "script-src") {
			return directive
		}
	}
	return ""
}

// A CAPTCHA provider's exact origins join script-src and a frame-src on the
// app's pages only, beside the hashes, never as an inline-script keyword.
func TestThirdPartyOriginsInTheCSP(t *testing.T) {
	p := pages(t, policyBuild(editedManifest))
	before := serve(t, p, "GET", "/").Header().Get("Content-Security-Policy")
	if strings.Contains(before, "frame-src") || strings.Contains(before, "cloudflare") {
		t.Fatalf("without a provider: %q", before)
	}
	p.AllowThirdParty(ThirdParty{Script: []string{"https://challenges.cloudflare.com"}, Frame: []string{"https://challenges.cloudflare.com"}})
	for _, target := range []string{"/", "/app/settings", "/nowhere"} {
		csp := serve(t, p, "GET", target).Header().Get("Content-Security-Policy")
		script := scriptSrc(csp)
		if !strings.Contains(script, "https://challenges.cloudflare.com") || strings.Contains(script, "unsafe-inline") ||
			!strings.Contains(script, "'sha256-") || !strings.Contains(csp, "frame-src 'self' https://challenges.cloudflare.com") {
			t.Errorf("%s: %q", target, csp)
		}
	}
	// The admin build's shell is untouched.
	w := httptest.NewRecorder()
	p.ServeAdmin(w, httptest.NewRequest("GET", "/admin/x", nil))
	if strings.Contains(w.Header().Get("Content-Security-Policy"), "cloudflare") {
		t.Fatal("the admin build got the third party")
	}
}
