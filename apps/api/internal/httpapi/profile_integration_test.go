package httpapi

import (
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/store"
	"strings"
	"testing"
)

func TestUpdateProfile(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	f := factory.TestFaker(t)
	session := signedIn(t, db, f)
	other := signedIn(t, db, f)
	email := emailOf(t, db, session.UserID)
	update := func(body string, want int) store.User {
		var user store.User
		_ = json.Unmarshal(call(t, h, "PATCH", "/api/auth/me", body, session.CSRFToken, session.Cookie(), want).Body.Bytes(), &user)
		return user
	}

	user := update(`{"name":"  Ada Lovelace "}`, 200)
	if user.Name != "Ada Lovelace" || user.Email != email || user.PublicID == "" {
		t.Fatalf("updated user: %+v", user)
	}
	var me response
	_ = json.Unmarshal(call(t, h, "GET", "/api/auth/me", "", "", session.Cookie(), 200).Body.Bytes(), &me)
	if me.User.Name != "Ada Lovelace" {
		t.Fatalf("name not saved: %+v", me.User)
	}
	_ = json.Unmarshal(call(t, h, "GET", "/api/auth/me", "", "", other.Cookie(), 200).Body.Bytes(), &me)
	if me.User.Name == "Ada Lovelace" {
		t.Fatal("another user's name changed")
	}
	e := onlyEvent(t, db, "auth.profile_updated")
	if !is(e.UserID, session.UserID) || !is(e.ActorID, session.UserID) || !strings.Contains(e.Raw, `"to": "Ada Lovelace"`) {
		t.Fatalf("profile updated: %s", e.Raw)
	}
	update(`{"name":"Ada Lovelace"}`, 200)
	if len(eventsNamed(t, db, "auth.profile_updated")) != 1 {
		t.Fatal("an update to the same name was recorded")
	}

	for _, body := range []string{`{"name":""}`, `{"name":"   "}`, fmt.Sprintf(`{"name":%q}`, strings.Repeat("x", 81)), `{"name":"a\u0000b"}`} {
		update(body, 422)
	}
	// The email address isn't editable.
	update(`{"name":"Ada","email":"new@example.com"}`, 400)
	if emailOf(t, db, session.UserID) != email {
		t.Fatal("email changed")
	}
	call(t, h, "PATCH", "/api/auth/me", `{"name":"Ada"}`, "", session.Cookie(), 403)
	call(t, h, "PATCH", "/api/auth/me", `{"name":"Ada"}`, "", nil, 401)
}
