package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"
)

// policiesFile is the web build's list of the policy documents it holds,
// written by the app build (policyManifest in apps/web/vite.config.ts). A
// dot-file, so it is never served.
const policiesFile = ".policies.json"

// policyPrefix is where the policy pages and their .data files live, in URLs
// and in the build: served only once ServePolicies has turned them on.
const policyPrefix = "policy/"

// ErrPoliciesNotBuilt means POLICIES=on but the web build has no policies
// (no build, or one made before they existed).
var ErrPoliciesNotBuilt = errors.New("POLICIES=on needs a web build with the policies: run make build")

// PolicyDocument is one document as built: its version (its `updated`
// date, YYYY-MM-DD) and whether it is still the committed template.
type PolicyDocument struct {
	Version     string `json:"version"`
	Boilerplate bool   `json:"boilerplate"`
}

// Policies are the terms of service and privacy policy in the web build.
type Policies struct {
	TermsOfService PolicyDocument `json:"termsOfService"`
	PrivacyPolicy  PolicyDocument `json:"privacyPolicy"`
}

// readPolicies reads the build's manifest: nil without one, an error for
// one that isn't exactly the two documents with real dates.
func readPolicies(files fs.FS) (*Policies, error) {
	body, err := fs.ReadFile(files, policiesFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p Policies
	d := json.NewDecoder(strings.NewReader(string(body)))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return nil, fmt.Errorf("read %s: %w", policiesFile, err)
	}
	for name, doc := range map[string]PolicyDocument{"termsOfService": p.TermsOfService, "privacyPolicy": p.PrivacyPolicy} {
		if _, err := time.Parse(time.DateOnly, doc.Version); err != nil {
			return nil, fmt.Errorf("read %s: %s has no valid version (%q)", policiesFile, name, doc.Version)
		}
	}
	return &p, nil
}

// ServePolicies publishes the policy pages (POLICIES=on) and returns their
// versions. Until it is called they answer as any unknown URL. It fails
// without the policies in the build (ErrPoliciesNotBuilt), and in
// production while either document is still the committed template, naming
// the file to edit.
func (p *Pages) ServePolicies() (Policies, error) {
	if p.policies == nil {
		return Policies{}, ErrPoliciesNotBuilt
	}
	if p.production {
		for file, doc := range map[string]PolicyDocument{"terms-of-service": p.policies.TermsOfService, "privacy-policy": p.policies.PrivacyPolicy} {
			if doc.Boilerplate {
				return Policies{}, fmt.Errorf("POLICIES=on in production, but apps/web/content/policy/%s.md is still the template (boilerplate: true): edit it, remove that line and rebuild", file)
			}
		}
	}
	p.policiesOn = true
	return *p.policies, nil
}

// unpublished reports whether name (a URL path without its leading slash,
// or a built file) is a policy page or file while the policies are off.
func (p *Pages) unpublished(name string) bool {
	return !p.policiesOn && (name == strings.TrimSuffix(policyPrefix, "/") || strings.HasPrefix(name, policyPrefix))
}
