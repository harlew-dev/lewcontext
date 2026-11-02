package account_test

import (
	"encoding/json"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"testing"
)

// A sign-up's name follows the profile's rule, checked from the body before
// any limit or lookup, with verification on or off. NUL used to be a 500,
// and other control and bidi characters reached users.name.
func TestSignUpNamesRefuseHiddenCharacters(t *testing.T) {
	for _, verify := range []bool{false, true} {
		s, db := server(t)
		if verify {
			s, db, _ = verificationServer(t, false)
		}
		h := router.New(s)
		for _, name := range []string{"Ada\x00", "Ada\tLovelace", "Ada\u202Eecalevol", "Ada\u200BL", ""} {
			body, _ := json.Marshal(map[string]string{"name": name, "email": "ada@example.com", "password": "correct horse battery staple"})
			w := apitest.Call(t, h, "POST", "/api/auth/register", string(body), "", nil, 422)
			if apitest.ErrorField(t, w) != "name" {
				t.Errorf("verify=%v %q: %s", verify, name, w.Body.String())
			}
		}
		if countRows(t, db, "users")+countRows(t, db, "pending_registrations") != 0 {
			t.Fatalf("verify=%v: a refused name was saved", verify)
		}
		// Emoji joined with ZWJ are a name like any other.
		want := 201
		if verify {
			want = 202
		}
		body, _ := json.Marshal(map[string]string{"name": "Ada 👩\u200D💻", "email": "ada@example.com", "password": "correct horse battery staple"})
		apitest.Call(t, h, "POST", "/api/auth/register", string(body), "", nil, want)
	}
}

func TestProfileNamesRefuseHiddenCharacters(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	session := apitest.SignedIn(t, db, factory.TestFaker(t))
	for _, name := range []string{"Ada\x1b[2J", "Ada\u2028L", "Ada\u202EL"} {
		body, _ := json.Marshal(map[string]string{"name": name})
		apitest.Call(t, h, "PATCH", "/api/auth/me", string(body), session.CSRFToken, session.Cookie(), 422)
	}
	body, _ := json.Marshal(map[string]string{"name": "🏳\uFE0F\u200D🌈 Ada"})
	apitest.Call(t, h, "PATCH", "/api/auth/me", string(body), session.CSRFToken, session.Cookie(), 200)
}
