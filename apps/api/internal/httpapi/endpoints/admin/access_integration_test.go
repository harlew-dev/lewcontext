package admin_test

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/store"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

// fromAddress sends every request to h as if from ip (the connection's
// address), as a client outside or inside ADMIN_ALLOWED_IPS would.
func fromAddress(h http.Handler, ip string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = netip.AddrPortFrom(netip.MustParseAddr(ip), 1234).String()
		h.ServeHTTP(w, r)
	})
}

// serverWith is a server with the test pages on db, with config changed by
// change.
func serverWith(t *testing.T, db *bun.DB, change func(*config.Config)) *httpapi.Server {
	t.Helper()
	c := apitest.Config()
	change(&c)
	s := apitest.Must(httpapi.New(&store.Store{DB: db}, c))(t)
	s.Pages = apitest.Pages(t)
	return s
}

// eventually waits for the asynchronous admin.access_refused records to
// settle, then returns the events named name.
func eventually(t *testing.T, db *bun.DB, name string, want int) []apitest.Event {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		found := apitest.EventsNamed(t, db, name)
		if len(found) >= want || time.Now().After(deadline) {
			// A moment more, so an extra event would show up too.
			time.Sleep(50 * time.Millisecond)
			return apitest.EventsNamed(t, db, name)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// With ADMIN_ALLOWED_IPS, the right key from anywhere else gets exactly what
// a wrong key gets, for pages, the API and the assets pass alike, and the
// operator learns of it once per address per window; from inside, as before.
func TestAdminAllowedIPs(t *testing.T) {
	_, db := server(t)
	s := serverWith(t, db, func(c *config.Config) {
		c.AdminAllowedIPs = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("2001:db8:1::/48")}
	})
	inside, outside := fromAddress(router.New(s), "203.0.113.7"), fromAddress(router.New(s), "198.51.100.9")
	f := factory.TestFaker(t)
	_, session := adminSignedIn(t, db, f)
	key := factory.AdminRouteKey

	// From inside: the door, the API and the files.
	pass := passFrom(t, inside, key)
	adminCall(t, inside, "GET", "/api/admin/me", "", "", 200, session.Cookie())
	if w := apitest.Get(inside, "GET", apitest.AdminAsset, pass); w.Code != 200 {
		t.Fatalf("a file from inside: %d", w.Code)
	}
	if len(apitest.EventsNamed(t, db, "admin.access_refused")) != 0 {
		t.Fatal("a refusal recorded from inside")
	}

	// From outside, the right key is a wrong one: every API route, every
	// page, and a valid pass.
	for _, route := range adminRoutesList {
		want := adminSend(outside, route.method, route.path, "{}", key+"x", session.CSRFToken, session.Cookie())
		got := adminSend(outside, route.method, route.path, "{}", key, session.CSRFToken, session.Cookie())
		sameAnswer(t, route.method+" "+route.path+" from outside", got, want)
	}
	nowhere := apitest.Get(outside, "GET", "/nowhere", nil)
	for _, target := range []string{"/admin/" + key, "/admin/" + key + "/users"} {
		sameAnswer(t, target+" from outside", apitest.Get(outside, "GET", target, session.Cookie()), nowhere)
	}
	sameAnswer(t, "a valid pass from outside", apitest.Get(outside, "GET", apitest.AdminAsset, pass), nowhere)
	// A wrong key from outside records nothing: the address is checked only
	// once the key matched.
	adminSend(fromAddress(router.New(s), "198.51.100.10"), "GET", "/api/admin/me", "", key+"x", "")

	// One event for all of that, with the address in events.ip only.
	refused := eventually(t, db, "admin.access_refused", 1)
	if len(refused) != 1 || refused[0].IP != "198.51.100.9" || refused[0].Payload["reason"] != "ip_not_allowed" || refused[0].ActorID.Valid || refused[0].UserID.Valid {
		t.Fatalf("admin.access_refused: %+v", refused)
	}
	// Another address gets its own; IPv6 counts by its /64; inside an
	// allowed /48 it's let in.
	adminSend(fromAddress(router.New(s), "198.51.100.11"), "GET", "/api/admin/me", "", key, "")
	adminSend(fromAddress(router.New(s), "2001:db8:2:3::1"), "GET", "/api/admin/me", "", key, "")
	adminSend(fromAddress(router.New(s), "2001:db8:2:3::ffff"), "GET", "/api/admin/me", "", key, "")
	if w := adminSend(fromAddress(router.New(s), "2001:db8:1:9::1"), "GET", "/api/admin/me", "", key, "", session.Cookie()); w.Code != 200 {
		t.Fatalf("inside the IPv6 range: %d", w.Code)
	}
	if refused := eventually(t, db, "admin.access_refused", 3); len(refused) != 3 {
		t.Fatalf("want one event per address (an IPv6 /64 counting once), got %d: %+v", len(refused), refused)
	}
}

// The previous key keeps live sessions made under it working, with a
// header saying so; it never opens the sign-in, and to anyone without such a
// session it is a wrong key.
func TestPreviousAdminKey(t *testing.T) {
	_, db := server(t)
	previousKey := factory.AdminRouteKey
	currentKey := "rotated-" + factory.AdminRouteKey
	s := serverWith(t, db, func(c *config.Config) {
		c.AdminRouteKey = currentKey
		c.AdminRouteKeyPrevious = previousKey
	})
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	// A session from before the rotation (factory sessions are made under
	// factory.AdminRouteKey), and one made since.
	admin, old := adminSignedIn(t, db, f)
	current := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(admin.User.ID)).State(factory.UnderKey(currentKey)).CreateOne(ctx, db, f))(t)
	idle := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(admin.User.ID)).State(factory.AdminIdle).CreateOne(ctx, db, f))(t)
	wrong := previousKey + "x"
	send := func(method, path, key string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		return adminSend(h, method, path, "{}", key, csrf, cookie)
	}

	// Its sign-in is a wrong key's, page and API, with or without a session.
	nowhere := apitest.Get(h, "GET", "/nowhere", nil)
	for _, cookie := range []*http.Cookie{nil, old.Cookie()} {
		sameAnswer(t, "the previous key's sign-in page", apitest.Get(h, "GET", "/admin/"+previousKey, cookie), nowhere)
		for _, path := range []string{"/api/admin/login", "/api/admin/login/mfa/setup", "/api/admin/login/mfa"} {
			sameAnswer(t, path+" under the previous key", send("POST", path, previousKey, cookie, ""), send("POST", path, wrong, cookie, ""))
		}
	}
	// Without a live session made under one of the keys, every route is a
	// wrong key's too: no cookie, an ended session, an unknown token.
	unknown := &http.Cookie{Name: auth.AdminCookieName(false), Value: strings.Repeat("A", 43)}
	for name, cookie := range map[string]*http.Cookie{"no session": nil, "an idle one": idle.Cookie(), "an unknown one": unknown} {
		for _, route := range adminRoutesList {
			sameAnswer(t, route.method+" "+route.path+" with "+name, send(route.method, route.path, previousKey, cookie, old.CSRFToken), send(route.method, route.path, wrong, cookie, old.CSRFToken))
		}
		sameAnswer(t, "an inner page with "+name, apitest.Get(h, "GET", "/admin/"+previousKey+"/users", cookie), nowhere)
	}

	// Its own session works, and says the key is being retired; the API and
	// pages alike, and the page's pass is the current key's.
	if got := adminSend(h, "GET", "/api/admin/me", "", previousKey, "", old.Cookie()); got.Code != 200 || got.Header().Get("X-Admin-Key-Status") != "previous" {
		t.Fatalf("the previous key's session: %d %v", got.Code, got.Header())
	}
	page := apitest.Get(h, "GET", "/admin/"+previousKey+"/users", old.Cookie())
	if page.Code != 200 || page.Header().Get("X-Admin-Key-Status") != "previous" || !strings.Contains(page.Body.String(), "admin shell") {
		t.Fatalf("an inner page under the previous key: %d %v", page.Code, page.Header())
	}
	checkAssetsPass(t, "a page under the previous key", page, s)
	// The current key's session doesn't say so.
	if got := adminSend(h, "GET", "/api/admin/me", "", currentKey, "", current.Cookie()); got.Code != 200 || got.Header().Get("X-Admin-Key-Status") != "" {
		t.Fatalf("the current key's session: %d %v", got.Code, got.Header())
	}

	// A session made under the other key: 401 under either.
	for name, got := range map[string]*httptest.ResponseRecorder{
		"a current session under the previous key": adminSend(h, "GET", "/api/admin/me", "", previousKey, "", current.Cookie()),
		"a previous session under the current key": adminSend(h, "GET", "/api/admin/me", "", currentKey, "", old.Cookie()),
	} {
		if got.Code != 401 || got.Header().Get("X-Admin-Key-Status") != "" {
			t.Fatalf("%s: %d %v", name, got.Code, got.Header())
		}
	}
	sameAnswer(t, "an inner page with the current key's session", apitest.Get(h, "GET", "/admin/"+previousKey+"/users", current.Cookie()), nowhere)
	if w := apitest.Get(h, "GET", "/admin/"+currentKey+"/users", old.Cookie()); w.Code != 302 {
		t.Fatalf("an inner page under the current key with the previous key's session: %d", w.Code)
	}

	// Removing the previous key ends its sessions: the old key is a wrong
	// one, and the new one doesn't take them.
	removed := router.New(serverWith(t, db, func(c *config.Config) { c.AdminRouteKey = currentKey }))
	sameAnswer(t, "the removed key", adminSend(removed, "GET", "/api/admin/me", "", previousKey, "", old.Cookie()), adminSend(removed, "GET", "/api/admin/me", "", wrong, "", old.Cookie()))
	if w := adminSend(removed, "GET", "/api/admin/me", "", currentKey, "", old.Cookie()); w.Code != 401 {
		t.Fatalf("a removed key's session under the new key: %d", w.Code)
	}

	// Signing out works under the previous key.
	adminSend(h, "POST", "/api/admin/logout", "{}", previousKey, old.CSRFToken, old.Cookie())
	if w := send("GET", "/api/admin/me", previousKey, old.Cookie(), ""); w.Code != 404 {
		t.Fatalf("after signing out under the previous key: %d", w.Code)
	}
	apitest.OnlyEvent(t, db, "admin.logged_out")
}

// A viewer reads but can't act; /api/admin/me says which role it has; the
// role changes only from the command line, recorded.
func TestAdminRoles(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	viewer, viewerSession := adminSignedIn(t, db, f)
	operator := apitest.WithMFA(t, db, f)
	apitest.Must(factory.Admins.State(factory.AdminFor(operator.User.ID)).State(factory.AdminRole(auth.AdminOperator)).CreateOne(ctx, db, f))(t)
	operatorSession := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(operator.User.ID)).CreateOne(ctx, db, f))(t)

	for session, role := range map[*factory.AdminSession]string{&viewerSession: "viewer", &operatorSession: "operator"} {
		var me struct{ Role string }
		apitest.Must(0, json.Unmarshal(adminCall(t, h, "GET", "/api/admin/me", "", "", 200, session.Cookie()).Body.Bytes(), &me))(t)
		if me.Role != role {
			t.Fatalf("/me role: %q, want %q", me.Role, role)
		}
	}
	// The step-up is registered with Role(operator), as every action is: a
	// viewer is refused before its code is looked at, an operator's code is
	// checked.
	act := func(session factory.AdminSession, a apitest.MFAAccount, step int64, want int) {
		t.Helper()
		adminCall(t, h, "POST", "/api/admin/step-up", apitest.CodeBody(a.CodeAt(step)), session.CSRFToken, want, session.Cookie())
	}
	act(viewerSession, viewer, 0, 403)
	act(operatorSession, operator, 0, 204)
	// Still behind the key and the session.
	adminCall(t, h, "POST", "/api/admin/step-up", apitest.CodeBody("123456"), "", 401)
	if w := adminSend(h, "POST", "/api/admin/step-up", apitest.CodeBody(operator.CodeAt(1)), "wrong", operatorSession.CSRFToken, operatorSession.Cookie()); w.Code != 404 {
		t.Fatalf("an action with a wrong key: %d", w.Code)
	}

	// Granting another role changes it, recording admin.updated (no actor);
	// granting again with none keeps it.
	before := len(apitest.EventsNamed(t, db, "admin.granted"))
	a, from := must2(s.Repo.GrantAdmin(ctx, viewer.User.Email, auth.AdminOperator))(t)
	if a.Role != auth.AdminOperator || from != auth.AdminViewer {
		t.Fatalf("changing the role: %+v from %q", a, from)
	}
	updated := apitest.OnlyEvent(t, db, "admin.updated")
	role, _ := updated.Payload["changes"].(map[string]any)["role"].(map[string]any)
	if updated.ActorID.Valid || !apitest.Is(updated.UserID, viewer.User.ID) || updated.Payload["user"] != viewer.User.PublicID || role["from"] != "viewer" || role["to"] != "operator" {
		t.Fatalf("admin.updated: %+v", updated)
	}
	if len(apitest.EventsNamed(t, db, "admin.granted")) != before {
		t.Fatal("a role change recorded admin.granted")
	}
	act(viewerSession, viewer, 0, 204)
	if a, from := must2(s.Repo.GrantAdmin(ctx, viewer.User.Email, ""))(t); a.Role != auth.AdminOperator || from != "" {
		t.Fatalf("granting again without a role: %+v from %q", a, from)
	}
	if e := apitest.EventsNamed(t, db, "admin.granted"); len(e) != before+1 || e[len(e)-1].Payload["role"] != "operator" {
		t.Fatalf("granting again: %+v", e)
	}
	// Back to viewer: the next request can't act.
	must2(s.Repo.GrantAdmin(ctx, viewer.User.Email, auth.AdminViewer))(t)
	act(viewerSession, viewer, 1, 403)
	// A new admin without a role is a viewer.
	user := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	if a, _ := must2(s.Repo.GrantAdmin(ctx, user.Email, ""))(t); a.Role != auth.AdminViewer {
		t.Fatalf("a new admin: %+v", a)
	}
}

// adminSessionRow reads where an admin session signed in from.
func adminSessionRow(t *testing.T, db bun.IDB, userID int64) (ip, userAgent string) {
	t.Helper()
	err := db.NewSelect().TableExpr("admin_sessions").ColumnExpr("host(ip), user_agent").Where("user_id = ?", userID).Scan(context.Background(), &ip, &userAgent)
	if err != nil {
		t.Fatal(err)
	}
	return ip, userAgent
}

// A session remembers where it signed in; used from another network, it is
// recorded once, and keeps working.
func TestAdminSessionMoved(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	a := withAdmin(t, db, f)

	// Signing in stores the client's address and browser.
	challenge := apitest.CookieNamed(adminLogin(t, h, a.User.Email, 200), auth.AdminMFACookieName(false))
	adminCall(t, h, "POST", "/api/admin/login/mfa", apitest.CodeBody(a.CodeAt(0)), "", 200, challenge)
	if ip, agent := adminSessionRow(t, db, a.User.ID); ip != "127.0.0.1" || agent != apitest.UserAgent {
		t.Fatalf("the session's sign-in: %q %q", ip, agent)
	}

	session := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(a.User.ID)).State(factory.SignedInFrom("198.51.100.20")).State(func(s *factory.AdminSession) {
		s.CreatedAt = time.Now().Add(-time.Hour)
	}).CreateOne(ctx, db, f))(t)
	// The same /24 isn't a move.
	near := fromAddress(h, "198.51.100.99")
	adminCall(t, near, "GET", "/api/admin/me", "", "", 200, session.Cookie())
	if len(apitest.EventsNamed(t, db, "admin.session.moved")) != 0 {
		t.Fatal("a request from the same /24 recorded a move")
	}
	// From elsewhere: recorded once, by network, with the session's age.
	far := fromAddress(h, "203.0.113.5")
	adminCall(t, far, "GET", "/api/admin/me", "", "", 200, session.Cookie())
	adminCall(t, far, "GET", "/api/admin/users", "", "", 200, session.Cookie())
	adminCall(t, fromAddress(h, "192.0.2.1"), "GET", "/api/admin/me", "", "", 200, session.Cookie())
	moved := apitest.OnlyEvent(t, db, "admin.session.moved")
	age, _ := moved.Payload["sessionAge"].(float64)
	if !apitest.Is(moved.ActorID, a.User.ID) || moved.UserID.Valid || moved.IP != "203.0.113.5" ||
		moved.Payload["from"] != "198.51.100.0/24" || moved.Payload["to"] != "203.0.113.0/24" || moved.Payload["admin"] != a.User.PublicID || age < 3500 || age > 3700 {
		t.Fatalf("admin.session.moved: %+v", moved)
	}
}

// adminsPage is GET /api/admin/admins.
type adminsPage struct {
	Data []store.AdminAccount `json:"data"`
}

// The Admins page lists every admin with their role, MFA, last admin
// sign-in and live sessions, and is recorded as a read.
func TestAdminAdmins(t *testing.T) {
	_, db := server(t)
	previousKey := factory.AdminRouteKey
	s := serverWith(t, db, func(c *config.Config) {
		c.AdminRouteKey = "rotated-" + factory.AdminRouteKey
		c.AdminRouteKeyPrevious = previousKey
	})
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	// An operator with MFA: this session (under the current key), one from
	// before the rotation, and ended ones that aren't listed.
	me := apitest.WithMFA(t, db, f)
	apitest.Must(factory.Admins.State(factory.AdminFor(me.User.ID)).State(factory.AdminRole(auth.AdminOperator)).State(func(a *factory.Admin) {
		a.CreatedAt = time.Now().AddDate(0, -6, 0)
	}).CreateOne(ctx, db, f))(t)
	mine := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(me.User.ID)).State(factory.UnderKey(s.Config.AdminRouteKey)).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(me.User.ID)).State(factory.SignedInFrom("2001:db8::7")).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(me.User.ID)).State(factory.AdminExpired).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(me.User.ID)).State(factory.UnderKey("long-gone-"+factory.AdminRouteKey)).CreateOne(ctx, db, f))(t)
	// A newer viewer without MFA, inside the setup window.
	newcomer := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	apitest.Must(factory.Admins.State(factory.AdminFor(newcomer.ID)).State(factory.SetupWindow).CreateOne(ctx, db, f))(t)
	// Signed in to the dashboard a week ago, and to the app since (which
	// doesn't count).
	signedIn := time.Now().AddDate(0, 0, -7).UTC().Truncate(time.Second)
	apitest.Must(factory.Events.State(factory.EventOf("admin.logged_in", map[string]any{"user": me.User.PublicID})).State(func(e *factory.Event) {
		e.ActorID, e.CreatedAt = me.User.ID, signedIn
	}).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.Events.State(factory.EventOf("auth.logged_in", map[string]any{"user": me.User.PublicID})).State(func(e *factory.Event) {
		e.ActorID, e.UserID, e.CreatedAt = me.User.ID, me.User.ID, time.Now()
	}).CreateOne(ctx, db, f))(t)

	key := s.Config.AdminRouteKey
	w := adminSend(h, "GET", "/api/admin/admins", "", key, "", mine.Cookie())
	if w.Code != 200 {
		t.Fatalf("the Admins page: %d %s", w.Code, w.Body.String())
	}
	list := decodeAdmin[adminsPage](t, w.Body.Bytes()).Data
	if len(list) != 2 || list[0].User.PublicID != me.User.PublicID || list[1].User.PublicID != newcomer.PublicID {
		t.Fatalf("admins, oldest grant first: %+v", list)
	}
	first, second := list[0], list[1]
	if first.Role != auth.AdminOperator || !first.MFAEnabled || first.MFASetupUntil != nil || first.LastSignedInAt == nil || !first.LastSignedInAt.Equal(signedIn) || first.User.Email != me.User.Email {
		t.Fatalf("the operator: %+v", first)
	}
	if second.Role != auth.AdminViewer || second.MFAEnabled || second.MFASetupUntil == nil || second.LastSignedInAt != nil || len(second.Sessions) != 0 {
		t.Fatalf("the newcomer: %+v", second)
	}
	if len(first.Sessions) != 2 {
		t.Fatalf("want the two live sessions, got %+v", first.Sessions)
	}
	byIP := map[string]store.AdminAccountSession{}
	for _, session := range first.Sessions {
		byIP[session.IP] = session
	}
	if this := byIP["127.0.0.1"]; !this.Current || this.PreviousKey || this.UserAgent != mine.UserAgent {
		t.Fatalf("this session: %+v", this)
	}
	if old := byIP["2001:db8::7"]; old.Current || !old.PreviousKey {
		t.Fatalf("the previous key's session: %+v", old)
	}
	listed := apitest.OnlyEvent(t, db, "admin.admin.listed")
	if !apitest.Is(listed.ActorID, me.User.ID) || listed.UserID.Valid || listed.WorkspaceID.Valid || listed.Payload["admin"] != me.User.PublicID {
		t.Fatalf("admin.admin.listed: %+v", listed)
	}
	if w := adminSend(h, "GET", "/api/admin/admins?x=1", "", key, "", mine.Cookie()); w.Code != 400 {
		t.Fatalf("an unknown parameter: %d", w.Code)
	}
	// An app session doesn't count.
	app := apitest.SignedIn(t, db, f)
	if w := adminSend(h, "GET", "/api/admin/admins", "", key, "", app.Cookie()); w.Code != 401 {
		t.Fatalf("an app session: %d", w.Code)
	}
}

// Signing out your other admin sessions keeps this one, needs the CSRF
// token, and any admin may.
func TestRevokeOtherAdminSessions(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	// A viewer: signing out your own sessions needs no role.
	a, session := adminSignedIn(t, db, f)
	if admin := apitest.Must(s.Repo.FindAdmin(ctx, a.User.ID))(t); admin.Role != auth.AdminViewer {
		t.Fatalf("want a viewer, got %q", admin.Role)
	}
	other := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(a.User.ID)).CreateOne(ctx, db, f))(t)
	_, someoneElse := adminSignedIn(t, db, f)

	adminCall(t, h, "DELETE", "/api/admin/sessions", "", "", 403, session.Cookie())
	adminCall(t, h, "DELETE", "/api/admin/sessions", "", "", 401)
	adminCall(t, h, "DELETE", "/api/admin/sessions", "", session.CSRFToken, 204, session.Cookie())
	adminCall(t, h, "GET", "/api/admin/me", "", "", 200, session.Cookie())
	adminCall(t, h, "GET", "/api/admin/me", "", "", 401, other.Cookie())
	adminCall(t, h, "GET", "/api/admin/me", "", "", 200, someoneElse.Cookie())
	e := apitest.OnlyEvent(t, db, "admin.sessions_revoked")
	if !apitest.Is(e.ActorID, a.User.ID) || e.UserID.Valid || e.Payload["count"] != float64(1) || e.Payload["admin"] != a.User.PublicID {
		t.Fatalf("admin.sessions_revoked: %+v", e)
	}
}

// unreachable is a database nothing answers on: every query fails at once,
// as in an outage.
func unreachable(t *testing.T) *bun.DB {
	t.Helper()
	cfg := apitest.Must(pgx.ParseConfig("postgresql://nobody:x@127.0.0.1:1/nothing?sslmode=disable&connect_timeout=1"))(t)
	db := bun.NewDB(stdlib.OpenDB(*cfg), pgdialect.New())
	t.Cleanup(func() { db.Close() })
	return db
}

// In an outage the previous key fails closed: exactly a wrong key, pages and
// API, and no assets pass. The current key keeps the existing outage rule
// (the page and its pass, which its sign-in page hands anyone with the key).
func TestPreviousAdminKeyInAnOutage(t *testing.T) {
	previousKey, currentKey := factory.AdminRouteKey, "rotated-"+factory.AdminRouteKey
	s := serverWith(t, unreachable(t), func(c *config.Config) {
		c.AdminRouteKey = currentKey
		c.AdminRouteKeyPrevious = previousKey
	})
	h := router.New(s)
	reported := &problems{}
	observe.SetReporter(reported)
	t.Cleanup(func() { observe.SetReporter(nil) })
	cookie := &http.Cookie{Name: auth.AdminCookieName(false), Value: strings.Repeat("A", 43)}
	nowhere := apitest.Get(h, "GET", "/nowhere", nil)

	page := apitest.Get(h, "GET", "/admin/"+previousKey+"/users", cookie)
	sameAnswer(t, "an inner page under the previous key in an outage", page, nowhere)
	for _, route := range []struct{ method, path string }{{"GET", "/api/admin/me"}, {"GET", "/api/admin/admins"}, {"DELETE", "/api/admin/sessions"}, {"POST", "/api/admin/logout"}} {
		got := adminSend(h, route.method, route.path, "{}", previousKey, "x", cookie)
		want := adminSend(h, route.method, route.path, "{}", previousKey+"x", "x", cookie)
		sameAnswer(t, route.method+" "+route.path+" under the previous key in an outage", got, want)
	}

	// The current key: the page is served (it shows the outage itself) with
	// its pass; the API says the server failed.
	current := apitest.Get(h, "GET", "/admin/"+currentKey+"/users", cookie)
	if current.Code != 200 || current.Header().Get("X-Admin-Key-Status") != "" {
		t.Fatalf("an inner page under the current key in an outage: %d %v", current.Code, current.Header())
	}
	checkAssetsPass(t, "an inner page under the current key in an outage", current, s)
	if w := adminSend(h, "GET", "/api/admin/me", "", currentKey, "", cookie); w.Code != 500 {
		t.Fatalf("/me under the current key in an outage: %d", w.Code)
	}

	// Each failed session check is reported, under its route's label or
	// pattern, never the key: both pages' gates, the previous key's API
	// checks (answered as a wrong key) and the current key's 500.
	count := map[string]int{}
	for _, p := range reported.list {
		if strings.Contains(p.Route+p.Err.Error(), previousKey) || strings.Contains(p.Route+p.Err.Error(), currentKey) {
			t.Fatalf("a report names the key: %+v", p)
		}
		message, _, _ := strings.Cut(p.Err.Error(), ":")
		count[p.Route+" · "+message]++
	}
	if count["page /admin/* · admin page session check"] != 2 || count["GET /api/admin/me · admin session check"] != 1 ||
		count["GET /api/admin/admins · admin session check"] != 1 || len(reported.list) != 7 {
		t.Fatalf("reported: %v", count)
	}
}

// problems records what observe.Report is handed (requests here are served
// one at a time).
type problems struct{ list []observe.Problem }

func (p *problems) Report(_ context.Context, problem observe.Problem) {
	p.list = append(p.list, problem)
}

// lastSeen reads an admin session's last_seen_at.
func lastSeen(t *testing.T, db bun.IDB, s factory.AdminSession) time.Time {
	t.Helper()
	var at time.Time
	if err := db.NewSelect().TableExpr("admin_sessions").Column("last_seen_at").Where("token_hash = ?", s.TokenHash).Scan(context.Background(), &at); err != nil {
		t.Fatal(err)
	}
	return at
}

// A session sent with the other key is refused without touching it, so a
// request with the wrong key can't keep someone else's session from idling
// out; with its own key it's refreshed.
func TestRefusedAdminSessionsAreLeftAlone(t *testing.T) {
	_, db := server(t)
	previousKey, currentKey := factory.AdminRouteKey, "rotated-"+factory.AdminRouteKey
	h := router.New(serverWith(t, db, func(c *config.Config) {
		c.AdminRouteKey = currentKey
		c.AdminRouteKeyPrevious = previousKey
	}))
	ctx := context.Background()
	f := factory.TestFaker(t)
	a := withAdmin(t, db, f)
	quiet := func(s *factory.AdminSession) { s.LastSeenAt = time.Now().Add(-10 * time.Minute) }
	current := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(a.User.ID)).State(factory.UnderKey(currentKey)).State(quiet).CreateOne(ctx, db, f))(t)
	old := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(a.User.ID)).State(quiet).CreateOne(ctx, db, f))(t)
	before := map[string]time.Time{"current": lastSeen(t, db, current), "old": lastSeen(t, db, old)}

	if w := adminSend(h, "GET", "/api/admin/me", "", previousKey, "", current.Cookie()); w.Code != 401 {
		t.Fatalf("a current session under the previous key: %d", w.Code)
	}
	if w := adminSend(h, "GET", "/api/admin/me", "", currentKey, "", old.Cookie()); w.Code != 401 {
		t.Fatalf("a previous session under the current key: %d", w.Code)
	}
	apitest.Get(h, "GET", "/admin/"+previousKey+"/users", current.Cookie())
	if !lastSeen(t, db, current).Equal(before["current"]) || !lastSeen(t, db, old).Equal(before["old"]) {
		t.Fatal("a refused session's idle timer was refreshed")
	}
	adminSend(h, "GET", "/api/admin/me", "", currentKey, "", current.Cookie())
	if !lastSeen(t, db, current).After(before["current"]) {
		t.Fatal("a session used with its own key wasn't refreshed")
	}
}

// The previous key from outside the allowlist is refused like the current
// one, pages and API, and recorded.
func TestPreviousAdminKeyOutsideTheAllowlist(t *testing.T) {
	_, db := server(t)
	previousKey, currentKey := factory.AdminRouteKey, "rotated-"+factory.AdminRouteKey
	s := serverWith(t, db, func(c *config.Config) {
		c.AdminRouteKey = currentKey
		c.AdminRouteKeyPrevious = previousKey
		c.AdminAllowedIPs = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	})
	outside := fromAddress(router.New(s), "198.51.100.9")
	_, old := adminSignedIn(t, db, factory.TestFaker(t))
	nowhere := apitest.Get(outside, "GET", "/nowhere", nil)
	sameAnswer(t, "an inner page under the previous key from outside", apitest.Get(outside, "GET", "/admin/"+previousKey+"/users", old.Cookie()), nowhere)
	sameAnswer(t, "/me under the previous key from outside",
		adminSend(outside, "GET", "/api/admin/me", "", previousKey, "", old.Cookie()),
		adminSend(outside, "GET", "/api/admin/me", "", previousKey+"x", "", old.Cookie()))
	if w := adminSend(fromAddress(router.New(s), "203.0.113.4"), "GET", "/api/admin/me", "", previousKey, "", old.Cookie()); w.Code != 200 {
		t.Fatalf("the previous key from inside: %d", w.Code)
	}
	if refused := eventually(t, db, "admin.access_refused", 1); len(refused) != 1 || refused[0].IP != "198.51.100.9" {
		t.Fatalf("admin.access_refused: %+v", refused)
	}
}

// Two requests at once from a new network record one move.
func TestAdminSessionMovedOnceUnderConcurrency(t *testing.T) {
	s, db := server(t)
	h := fromAddress(router.New(s), "203.0.113.5")
	f := factory.TestFaker(t)
	for range 5 {
		a := withAdmin(t, db, f)
		session := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(a.User.ID)).State(factory.SignedInFrom("198.51.100.20")).CreateOne(context.Background(), db, f))(t)
		var wg sync.WaitGroup
		codes := make([]int, 2)
		for i := range codes {
			wg.Go(func() {
				codes[i] = adminSend(h, "GET", "/api/admin/me", "", factory.AdminRouteKey, "", session.Cookie()).Code
			})
		}
		wg.Wait()
		if codes[0] != 200 || codes[1] != 200 {
			t.Fatalf("concurrent requests: %v", codes)
		}
	}
	if moved := apitest.EventsNamed(t, db, "admin.session.moved"); len(moved) != 5 {
		t.Fatalf("want one move per session, got %d", len(moved))
	}
}

// Admin sign-in needs an IP client address (sessions and events store one):
// without one it's refused clearly, before anything is checked.
func TestAdminSignInWithoutAnIPAddress(t *testing.T) {
	s, db := server(t)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = "@"
		router.New(s).ServeHTTP(w, r)
	})
	a := withAdmin(t, db, factory.TestFaker(t))
	for path, body := range map[string]string{"/api/admin/login": apitest.LoginBody(a.User.Email), "/api/admin/login/mfa": apitest.CodeBody(a.CodeAt(0))} {
		w := adminSend(h, "POST", path, body, factory.AdminRouteKey, "")
		if w.Code != 503 || !strings.Contains(w.Body.String(), "Admin sign-in isn't available") {
			t.Fatalf("%s from a unix socket: %d %s", path, w.Code, w.Body.String())
		}
	}
	if len(apitest.EventsNamed(t, db, "admin.login_attempted")) != 0 {
		t.Fatal("a refused sign-in recorded an attempt")
	}
}
