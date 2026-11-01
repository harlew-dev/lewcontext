package account_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

// registerWith is a sign-up body agreeing to policies, or to nothing when
// policies is "".
func registerWith(email, policies string) string {
	body := fmt.Sprintf(`{"name":"Ada Lovelace","email":%q,"password":"correct horse battery staple"`, email)
	if policies != "" {
		body += `,"policies":` + policies
	}
	return body + "}"
}

func agreement(v store.PolicyVersions) string {
	return fmt.Sprintf(`{"termsOfService":%q,"privacyPolicy":%q}`, v.TermsOfService, v.PrivacyPolicy)
}

// sessionUser is the user in a session response.
func sessionUser(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body struct{ User map[string]any }
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.User
}

// A refused agreement is refused on its field before any limit, hash or
// write, so it counts against nothing and records nothing.
func TestSignUpMustAgreeToThePolicies(t *testing.T) {
	s, db, box := verificationServer(t, false)
	s.Policies = currentPolicies
	h := router.New(s)
	for name, c := range map[string]struct{ policies, message string }{
		"missing":       {"", "Agree to the Terms of Service"},
		"empty":         {`{"termsOfService":"","privacyPolicy":""}`, "Agree to the Terms of Service"},
		"one missing":   {`{"termsOfService":"2026-11-01"}`, "Agree to the Terms of Service"},
		"stale":         {agreement(store.PolicyVersions{TermsOfService: "2026-10-01", PrivacyPolicy: "2026-10-15"}), "changed since this page opened"},
		"invented":      {agreement(store.PolicyVersions{TermsOfService: "2027-01-01", PrivacyPolicy: "2026-10-15"}), "changed since this page opened"},
		"not a version": {`{"termsOfService":"yes","privacyPolicy":"yes"}`, "changed since this page opened"},
	} {
		w := apitest.Call(t, h, "POST", "/api/auth/register", registerWith("ada@example.com", c.policies), "", nil, 422)
		if apitest.ErrorField(t, w) != "policies" || !strings.Contains(w.Body.String(), c.message) {
			t.Errorf("%s: %s", name, w.Body.String())
		}
	}
	// Only auth:per-ip, which LimitAuth counts for every request (a
	// malformed body too); none of the sign-up's own limits.
	if all, perIP := apitest.CountRateLimits(t, db, "%"), apitest.CountRateLimits(t, db, "auth:per-ip%"); all != perIP {
		t.Fatalf("refused agreements counted %d limits besides auth:per-ip", all-perIP)
	}
	if len(apitest.EventsNamed(t, db, "auth.registration.requested")) != 0 || countRows(t, db, "pending_registrations") != 0 {
		t.Fatal("a refused agreement saved a sign-up")
	}

	// The current versions: the sign-up keeps them, and the account gets
	// them, accepted when the sign-up was submitted.
	w := apitest.Call(t, h, "POST", "/api/auth/register", registerWith("ada@example.com", agreement(currentPolicies)), "", nil, 202)
	cookie := mustCookie(t, w, auth.RegistrationCookieName(false))
	var row struct {
		Terms, Privacy time.Time
		CreatedAt      time.Time
	}
	if err := db.NewSelect().TableExpr("pending_registrations").ColumnExpr("terms_of_service_version, privacy_policy_version, created_at").Scan(context.Background(), &row.Terms, &row.Privacy, &row.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if row.Terms.Format(time.DateOnly) != "2026-11-01" || row.Privacy.Format(time.DateOnly) != "2026-10-15" {
		t.Fatalf("pending row: %+v", row)
	}
	deliverSignUp(t, s, box)
	done := apitest.Call(t, h, "POST", "/api/auth/register/verify", apitest.CodeBody(box.code(t)), "", cookie, 201)
	got := sessionUser(t, done)["policies"].(map[string]any)
	accepted, _ := time.Parse(time.RFC3339Nano, got["acceptedAt"].(string))
	if got["termsOfService"] != "2026-11-01" || got["privacyPolicy"] != "2026-10-15" || !accepted.Equal(row.CreatedAt) {
		t.Fatalf("user policies %v, want accepted at %s", got, row.CreatedAt)
	}
	registered := apitest.OnlyEvent(t, db, "auth.registered")
	if p, _ := registered.Payload["policies"].(map[string]any); p["termsOfService"] != "2026-11-01" || p["privacyPolicy"] != "2026-10-15" {
		t.Fatalf("auth.registered %v", registered.Payload)
	}
	// The session carries them too, as /api/auth/me.
	me := apitest.Call(t, h, "GET", "/api/auth/me", "", "", mustCookie(t, done, auth.CookieName(false)), 200)
	if p, _ := sessionUser(t, me)["policies"].(map[string]any); p["termsOfService"] != "2026-11-01" {
		t.Fatalf("me: %s", me.Body.String())
	}
}

// With verification off the account is made at once, agreeing now; a taken
// email is still refused as before.
func TestSignUpAgreesWithoutVerification(t *testing.T) {
	s, db := server(t)
	s.Policies = currentPolicies
	h := router.New(s)
	apitest.Call(t, h, "POST", "/api/auth/register", registerWith("bo@example.com", ""), "", nil, 422)
	before := time.Now()
	w := apitest.Call(t, h, "POST", "/api/auth/register", registerWith("bo@example.com", agreement(currentPolicies)), "", nil, 201)
	got := sessionUser(t, w)["policies"].(map[string]any)
	accepted, _ := time.Parse(time.RFC3339Nano, got["acceptedAt"].(string))
	if got["termsOfService"] != "2026-11-01" || accepted.Before(before.Add(-time.Second)) {
		t.Fatalf("policies %v", got)
	}
	if p, _ := apitest.OnlyEvent(t, db, "auth.registered").Payload["policies"].(map[string]any); p["privacyPolicy"] != "2026-10-15" {
		t.Fatal("auth.registered has no policies")
	}
	apitest.Call(t, h, "POST", "/api/auth/register", registerWith("bo@example.com", agreement(currentPolicies)), "", nil, 409)
}

// Off, an agreement sent anyway (a page opened just before the switch) is
// accepted and ignored: nothing recorded.
func TestSignUpIgnoresPoliciesWhenOff(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	w := apitest.Call(t, h, "POST", "/api/auth/register", registerWith("cy@example.com", agreement(currentPolicies)), "", nil, 201)
	if policies, ok := sessionUser(t, w)["policies"]; !ok || policies != nil {
		t.Fatalf("policies %v", policies)
	}
	if _, ok := apitest.OnlyEvent(t, db, "auth.registered").Payload["policies"]; ok {
		t.Fatal("auth.registered recorded policies while off")
	}
}

// Login and the admin login share Credentials, which still refuse the field.
func TestLoginRefusesAPoliciesField(t *testing.T) {
	s, _ := server(t)
	s.Policies = currentPolicies
	h := router.New(s)
	body := `{"email":"ada@example.com","password":"correct horse battery staple","policies":` + agreement(currentPolicies) + `}`
	apitest.Call(t, h, "POST", "/api/auth/login", body, "", nil, 400)
}

// The record is all or nothing: a version without when it was accepted is
// refused by the table itself.
func TestUsersRefuseAPartialAgreement(t *testing.T) {
	_, db := server(t)
	f := factory.TestFaker(t)
	u := apitest.Must(factory.Users.CreateOne(context.Background(), db, f))(t)
	_, err := db.NewUpdate().TableExpr("users").Set("terms_of_service_version = ?", "2026-11-01").Where("id = ?", u.ID).Exec(context.Background())
	if err == nil {
		t.Fatal("a version without policies_accepted_at was stored")
	}
	agreed := apitest.Must(factory.Users.State(factory.AgreedToPolicies("2026-11-01", "2026-10-15")).CreateOne(context.Background(), db, f))(t)
	if agreed.PoliciesAcceptedAt == nil {
		t.Fatal("factory state didn't agree")
	}
}

func countRows(t *testing.T, db bun.IDB, table string) int {
	t.Helper()
	n, err := db.NewSelect().TableExpr("?", bun.Ident(table)).Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

// POLICIES turned on while a sign-up waited for its code: the sign-up agreed
// to nothing, so verify ends it as it ends an expired one, the same for a
// taken email as a new one, and makes no account.
func TestSignUpFromBeforeThePoliciesStartsAgain(t *testing.T) {
	s, db, box := verificationServer(t, false)
	h := router.New(s)
	taken := apitest.Must(factory.Users.CreateOne(context.Background(), db, factory.TestFaker(t)))(t)
	var answers []string
	for _, email := range []string{"new@example.com", taken.Email} {
		s.Policies = store.PolicyVersions{}
		w := apitest.Call(t, h, "POST", "/api/auth/register", registerWith(email, ""), "", nil, 202)
		cookie := mustCookie(t, w, auth.RegistrationCookieName(false))
		deliverSignUp(t, s, box)
		code := "000000"
		if email == "new@example.com" {
			code = box.code(t)
		}
		s.Policies = currentPolicies
		ended := apitest.Call(t, h, "POST", "/api/auth/register/verify", apitest.CodeBody(code), "", cookie, 401)
		answers = append(answers, ended.Body.String())
	}
	if answers[0] != answers[1] || !strings.Contains(answers[0], "This sign-up has expired. Start again.") {
		t.Fatalf("answers %q", answers)
	}
	if countUsers(t, db, "new@example.com") != 0 {
		t.Fatal("an account was made without an agreement")
	}
}

// The store refuses a version that isn't a date rather than recording
// nothing.
func TestStoreRefusesABadVersion(t *testing.T) {
	s, _ := server(t)
	_, err := s.Repo.CreateUser(context.Background(), "Ada", "ada@example.com", "hash", store.PolicyVersions{TermsOfService: "yes", PrivacyPolicy: "2026-10-15"})
	if err == nil || !strings.Contains(err.Error(), "terms of service version") {
		t.Fatalf("a bad version: %v", err)
	}
}
