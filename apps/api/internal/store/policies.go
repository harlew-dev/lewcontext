package store

import "time"

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
