package httpapi_test

import (
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"strings"
	"testing"
)

func TestUpdateProfile(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	other := apitest.SignedIn(t, db, f)
	email := apitest.EmailOf(t, db, session.UserID)
	update := func(body string, want int) store.User {
		var user store.User
		_ = json.Unmarshal(apitest.Call(t, h, "PATCH", "/api/auth/me", body, session.CSRFToken, session.Cookie(), want).Body.Bytes(), &user)
		return user
	}

	user := update(`{"name":"  Ada Lovelace "}`, 200)
	if user.Name != "Ada Lovelace" || user.Email != email || user.PublicID == "" {
		t.Fatalf("updated user: %+v", user)
	}
	var me apitest.SessionResponse
	_ = json.Unmarshal(apitest.Call(t, h, "GET", "/api/auth/me", "", "", session.Cookie(), 200).Body.Bytes(), &me)
	if me.User.Name != "Ada Lovelace" {
		t.Fatalf("name not saved: %+v", me.User)
	}
	_ = json.Unmarshal(apitest.Call(t, h, "GET", "/api/auth/me", "", "", other.Cookie(), 200).Body.Bytes(), &me)
	if me.User.Name == "Ada Lovelace" {
		t.Fatal("another user's name changed")
	}
	e := apitest.OnlyEvent(t, db, "auth.profile_updated")
	if !apitest.Is(e.UserID, session.UserID) || !apitest.Is(e.ActorID, session.UserID) || !strings.Contains(e.Raw, `"to": "Ada Lovelace"`) {
		t.Fatalf("profile updated: %s", e.Raw)
	}
	update(`{"name":"Ada Lovelace"}`, 200)
	if len(apitest.EventsNamed(t, db, "auth.profile_updated")) != 1 {
		t.Fatal("an update to the same name was recorded")
	}

	for _, body := range []string{`{"name":""}`, `{"name":"   "}`, fmt.Sprintf(`{"name":%q}`, strings.Repeat("x", 81)), `{"name":"a\u0000b"}`} {
		update(body, 422)
	}
	// The email address isn't editable.
	update(`{"name":"Ada","email":"new@example.com"}`, 400)
	if apitest.EmailOf(t, db, session.UserID) != email {
		t.Fatal("email changed")
	}
	apitest.Call(t, h, "PATCH", "/api/auth/me", `{"name":"Ada"}`, "", session.Cookie(), 403)
	apitest.Call(t, h, "PATCH", "/api/auth/me", `{"name":"Ada"}`, "", nil, 401)
}
