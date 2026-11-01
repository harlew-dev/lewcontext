package store

import (
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/events"
	"time"
)

// PolicyVersions are the versions (each document's `updated` date,
// YYYY-MM-DD) of the terms of service and privacy policy: the server's
// current ones (POLICIES=on), or the ones a sign-up agreed to. The zero
// value is none.
type PolicyVersions struct {
	TermsOfService string `json:"termsOfService"`
	PrivacyPolicy  string `json:"privacyPolicy"`
}

// Set reports whether both versions are there.
func (v PolicyVersions) Set() bool {
	return v.TermsOfService != "" && v.PrivacyPolicy != ""
}

// PolicyAgreement is what an account agreed to at sign-up, and when.
type PolicyAgreement struct {
	PolicyVersions
	AcceptedAt time.Time `json:"acceptedAt"`
}

// Policies is what the account agreed to at sign-up, or nil.
func (u User) Policies() *PolicyAgreement {
	if u.TermsOfServiceVersion == nil || u.PrivacyPolicyVersion == nil || u.PoliciesAcceptedAt == nil {
		return nil
	}
	return &PolicyAgreement{
		PolicyVersions: PolicyVersions{TermsOfService: u.TermsOfServiceVersion.Format(time.DateOnly), PrivacyPolicy: u.PrivacyPolicyVersion.Format(time.DateOnly)},
		AcceptedAt:     *u.PoliciesAcceptedAt,
	}
}

// MarshalJSON adds "policies" (Policies, or null) to the user's fields.
func (u User) MarshalJSON() ([]byte, error) {
	type fields User
	return json.Marshal(struct {
		fields
		Policies *PolicyAgreement `json:"policies"`
	}{fields(u), u.Policies()})
}

// dates are the versions as the database's date columns take them: nil for
// none, and an error for a version that isn't a YYYY-MM-DD date (the server
// only ever passes its own, so that's a bug, never a silent "no record").
func (v PolicyVersions) dates() (terms, privacy *time.Time, err error) {
	if v == (PolicyVersions{}) {
		return nil, nil, nil
	}
	t, err := time.Parse(time.DateOnly, v.TermsOfService)
	if err != nil {
		return nil, nil, fmt.Errorf("terms of service version %q: %w", v.TermsOfService, err)
	}
	p, err := time.Parse(time.DateOnly, v.PrivacyPolicy)
	if err != nil {
		return nil, nil, fmt.Errorf("privacy policy version %q: %w", v.PrivacyPolicy, err)
	}
	return &t, &p, nil
}

// event is the versions for auth.registered: nil when none were agreed to.
func (v PolicyVersions) event() *events.PolicyVersions {
	if !v.Set() {
		return nil
	}
	return &events.PolicyVersions{TermsOfService: v.TermsOfService, PrivacyPolicy: v.PrivacyPolicy}
}

// agreeTo records policies on a new account, as accepted at.
func (a *account) agreeTo(policies PolicyVersions, at time.Time) error {
	terms, privacy, err := policies.dates()
	if err != nil || terms == nil {
		return err
	}
	a.TermsOfServiceVersion, a.PrivacyPolicyVersion, a.PoliciesAcceptedAt = terms, privacy, &at
	return nil
}

// policies are the versions the sign-up agreed to, or none.
func (p pendingRegistration) policies() PolicyVersions {
	if p.TermsOfServiceVersion == nil || p.PrivacyPolicyVersion == nil {
		return PolicyVersions{}
	}
	return PolicyVersions{TermsOfService: p.TermsOfServiceVersion.Format(time.DateOnly), PrivacyPolicy: p.PrivacyPolicyVersion.Format(time.DateOnly)}
}

// userColumns are the users columns of a User the account itself sees (a
// session's), in the order of its fields.
const userColumns = "u.id, u.public_id, u.name, u.email, u.created_at, u.email_verified_at, u.terms_of_service_version, u.privacy_policy_version, u.policies_accepted_at"
