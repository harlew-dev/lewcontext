package store

import (
	"encoding/json"
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
// none. Callers have checked they're YYYY-MM-DD (the server's own).
func (v PolicyVersions) dates() (terms, privacy *time.Time) {
	if !v.Set() {
		return nil, nil
	}
	t, err1 := time.Parse(time.DateOnly, v.TermsOfService)
	p, err2 := time.Parse(time.DateOnly, v.PrivacyPolicy)
	if err1 != nil || err2 != nil {
		return nil, nil
	}
	return &t, &p
}

// event is the versions for auth.registered: nil when none were agreed to.
func (v PolicyVersions) event() *events.PolicyVersions {
	if !v.Set() {
		return nil
	}
	return &events.PolicyVersions{TermsOfService: v.TermsOfService, PrivacyPolicy: v.PrivacyPolicy}
}

// agreeTo records policies on a new account, as accepted at.
func (a *account) agreeTo(policies PolicyVersions, at time.Time) {
	terms, privacy := policies.dates()
	if terms == nil {
		return
	}
	a.TermsOfServiceVersion, a.PrivacyPolicyVersion, a.PoliciesAcceptedAt = terms, privacy, &at
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
