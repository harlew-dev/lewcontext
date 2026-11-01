package admin_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// operatorSignedIn is a live admin session of an operator (with MFA on),
// with session's states (factory.SteppedUp for an open step-up window).
func operatorSignedIn(t *testing.T, db bun.IDB, f *gofakeit.Faker, session ...func(*factory.AdminSession)) (apitest.MFAAccount, factory.AdminSession) {
	t.Helper()
	ctx := context.Background()
	a := apitest.WithMFA(t, db, f)
	apitest.Must(factory.Admins.State(factory.AdminFor(a.User.ID)).State(factory.AdminRole(auth.AdminOperator)).CreateOne(ctx, db, f))(t)
	states := factory.AdminSessions.State(factory.AdminSessionFor(a.User.ID))
	for _, state := range session {
		states = states.State(state)
	}
	return a, apitest.Must(states.CreateOne(ctx, db, f))(t)
}

// unblockBody is DELETE /api/admin/email-blocks's body, as JSON escapes it.
func unblockBody(email, reason string) string {
	body, _ := json.Marshal(map[string]string{"email": email, "reason": reason})
	return string(body)
}

const reason = "Owner wrote in to support asking to get invitations again."

// steppedUpUntil reads an admin session's step-up window's end, nil when
// it never opened.
func steppedUpUntil(t *testing.T, db bun.IDB, s factory.AdminSession) *time.Time {
	t.Helper()
	var until sql.NullTime
	err := db.NewSelect().Model((*factory.AdminSession)(nil)).Column("stepped_up_until").
		Where("token_hash = ?", s.TokenHash).Scan(context.Background(), &until)
	if err != nil {
		t.Fatal(err)
	}
	if !until.Valid {
		return nil
	}
	return &until.Time
}

// The block list: pages, a search (LIKE's wildcards literal), both sorts,
// each address's newest live link, one recorded read each time, and no
// token or digest in the answer.
func TestAdminEmailBlockList(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	admin, session := adminSignedIn(t, db, f)
	apitest.Must(factory.EmailBlocks.Sequence(func(i int, b *factory.EmailBlock) {
		b.Email = fmt.Sprintf("zed%02d@example.test", i)
		b.CreatedAt = time.Now().Add(time.Duration(i-30) * time.Hour)
	}).Create(ctx, db, f, 25))(t)
	apitest.Must(factory.EmailBlocks.State(factory.Blocked("50%_off@example.test")).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.EmailBlocks.State(factory.Blocked("500xoff@example.test")).CreateOne(ctx, db, f))(t)
	// zed00 has two live links (the newest counts) and an expired one;
	// zed01 only an expired one.
	newest := time.Now().Add(300 * 24 * time.Hour).Truncate(time.Second)
	links := []struct {
		email   string
		expires time.Time
	}{
		{"zed00@example.test", newest.Add(-24 * time.Hour)},
		{"zed00@example.test", newest},
		{"zed00@example.test", time.Now().Add(-time.Hour)},
		{"zed01@example.test", time.Now().Add(-time.Hour)},
	}
	for _, link := range links {
		apitest.Must(factory.EmailBlockLinks.State(factory.BlockLinkFor(link.email)).State(func(l *factory.EmailBlockLink) { l.ExpiresAt = link.expires }).CreateOne(ctx, db, f))(t)
	}
	list := func(query string) store.Page[store.AdminEmailBlock] {
		w := adminCall(t, h, "GET", "/api/admin/email-blocks"+query, "", "", 200, session.Cookie())
		if strings.Contains(w.Body.String(), "token") || strings.Contains(w.Body.String(), "hash") {
			t.Fatalf("a link's secret in the list: %s", w.Body.String())
		}
		return decodeAdmin[store.Page[store.AdminEmailBlock]](t, w.Body.Bytes())
	}

	// Newest first, 20 a page.
	first := list("")
	if first.Pages.Items != 27 || first.Pages.Total != 2 || len(first.Data) != 20 || first.Data[0].Email != "500xoff@example.test" {
		t.Fatalf("first page: %+v", first.Pages)
	}
	second := list("?page=2")
	if len(second.Data) != 7 || second.Data[6].Email != "zed00@example.test" {
		t.Fatalf("second page: %+v", second.Data)
	}
	zed00, zed01 := second.Data[6], second.Data[5]
	if zed00.LinkExpiresAt == nil || !zed00.LinkExpiresAt.Equal(newest) || zed01.LinkExpiresAt != nil {
		t.Fatalf("link expiries: %+v, %+v", zed00, zed01)
	}
	// The two 50… addresses come first, in the database's collation.
	if byEmail := list("?sort=email"); !strings.HasPrefix(byEmail.Data[0].Email, "50") || !strings.HasPrefix(byEmail.Data[1].Email, "50") || byEmail.Data[2].Email != "zed00@example.test" {
		t.Fatalf("sorted by email: %+v", byEmail.Data[:3])
	}
	if last := list("?sort=-email&perPage=1"); last.Data[0].Email != "zed24@example.test" {
		t.Fatalf("sorted by email, descending: %+v", last.Data)
	}
	if oldest := list("?sort=created&perPage=1"); oldest.Data[0].Email != "zed00@example.test" {
		t.Fatalf("oldest first: %+v", oldest.Data)
	}
	// Search: part of an address, any case; % and _ match themselves.
	if found := list("?q=" + "ZED1"); found.Pages.Items != 10 {
		t.Fatalf("search zed1: %d", found.Pages.Items)
	}
	if found := list("?q=50%25_"); found.Pages.Items != 1 || found.Data[0].Email != "50%_off@example.test" {
		t.Fatalf("search 50%%_: %+v", found.Data)
	}
	if found := list("?q=nobody"); found.Pages.Items != 0 || found.Data == nil {
		t.Fatalf("no match: %+v", found)
	}

	// Each read is one admin.email_block.listed, with what was asked, and no
	// user or workspace scope.
	reads := apitest.EventsNamed(t, db, "admin.email_block.listed")
	if len(reads) != 8 {
		t.Fatalf("reads recorded: %d", len(reads))
	}
	search := reads[6]
	if search.Payload["admin"] != admin.User.PublicID || search.Payload["q"] != "50%_" || search.Payload["sort"] != "-created" || search.Payload["page"] != float64(1) {
		t.Fatalf("a search's record: %+v", search.Payload)
	}
	if !apitest.Is(search.ActorID, admin.User.ID) || search.UserID.Valid || search.WorkspaceID.Valid {
		t.Fatalf("scope: %+v", search)
	}
	if reads[1].Payload["page"] != float64(2) || reads[2].Payload["sort"] != "email" {
		t.Fatalf("page and sort recorded: %+v %+v", reads[1].Payload, reads[2].Payload)
	}

	// Bad parameters are 400 and record nothing; an app session isn't an
	// admin one.
	for _, query := range []string{"?sort=name", "?sort=-ip", "?page=0", "?q=%00", "?email=x", "?q=a&q=b"} {
		adminCall(t, h, "GET", "/api/admin/email-blocks"+query, "", "", 400, session.Cookie())
	}
	app := apitest.Must(factory.SessionFor(ctx, db, f, admin.User.ID))(t)
	adminCall(t, h, "GET", "/api/admin/email-blocks", "", "", 401, app.Cookie())
	if n := len(apitest.EventsNamed(t, db, "admin.email_block.listed")); n != 8 {
		t.Fatalf("refused reads were recorded: %d", n)
	}
}

// A fresh code opens the session's 5-minute window, checked and used up as
// at the door (each code once), counted against mfa:per-account and
// recorded as auth.mfa.checked (admin_step_up), right or wrong. It needs the
// operator role and the CSRF token, and belongs to this session alone.
func TestAdminStepUp(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator, session := operatorSignedIn(t, db, f)
	stepUp := func(code string, want int, cookie ...*http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		if len(cookie) == 0 {
			cookie = []*http.Cookie{session.Cookie()}
		}
		return adminCall(t, h, "POST", "/api/admin/step-up", apitest.CodeBody(code), session.CSRFToken, want, cookie...)
	}
	checks := func() []apitest.Event {
		var found []apitest.Event
		for _, e := range apitest.EventsNamed(t, db, "auth.mfa.checked") {
			if e.Payload["step"] == "admin_step_up" {
				found = append(found, e)
			}
		}
		return found
	}

	// Without the CSRF token: refused before the code is looked at.
	adminCall(t, h, "POST", "/api/admin/step-up", apitest.CodeBody(operator.CodeAt(0)), "", 403, session.Cookie())
	// A wrong code: 422 on the field, recorded, nothing opened.
	if w := stepUp("000000", 422); apitest.ErrorField(t, w) != "code" {
		t.Fatalf("a wrong code: %s", w.Body.String())
	}
	if steppedUpUntil(t, db, session) != nil {
		t.Fatal("a wrong code opened the window")
	}
	// The right one: 204, the window 5 minutes ahead, by the database's clock.
	before := time.Now()
	stepUp(operator.CodeAt(0), 204)
	until := steppedUpUntil(t, db, session)
	if until == nil || until.Before(before.Add(auth.AdminStepUpWindow-5*time.Second)) || until.After(time.Now().Add(auth.AdminStepUpWindow+5*time.Second)) {
		t.Fatalf("the window: %v", until)
	}
	recorded := checks()
	if len(recorded) != 2 || recorded[0].Payload["ok"] != false || recorded[1].Payload["ok"] != true || recorded[1].Payload["method"] != "totp" {
		t.Fatalf("auth.mfa.checked: %+v", recorded)
	}
	if !apitest.Is(recorded[1].ActorID, operator.User.ID) || !apitest.Is(recorded[1].UserID, operator.User.ID) || recorded[1].Payload["user"] != operator.User.PublicID || recorded[1].IP != "127.0.0.1" {
		t.Fatalf("the check's scope: %+v", recorded[1])
	}
	// The same code again is refused, as at the door: each step works once.
	stepUp(operator.CodeAt(0), 422)
	// A recovery code works once.
	stepUp(operator.Codes[0].Code, 204)
	if left := checks()[3].Payload["recoveryCodesLeft"]; left != float64(1) {
		t.Fatalf("recovery codes left: %v", left)
	}
	stepUp(operator.Codes[0].Code, 422)

	// The window belongs to this session: another session of the same
	// admin, or a new one, starts without it.
	other := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(operator.User.ID)).CreateOne(ctx, db, f))(t)
	if steppedUpUntil(t, db, other) != nil {
		t.Fatal("a new session started stepped up")
	}

	// A viewer has nothing to step up for: 403, no code checked.
	_, viewer := adminSignedIn(t, db, f)
	checked := len(checks())
	adminCall(t, h, "POST", "/api/admin/step-up", apitest.CodeBody("123456"), viewer.CSRFToken, 403, viewer.Cookie())
	if len(checks()) != checked {
		t.Fatal("a viewer's code was checked")
	}
	// Malformed bodies are 400, before any limit or check.
	adminCall(t, h, "POST", "/api/admin/step-up", `{"code":"1","extra":1}`, session.CSRFToken, 400, session.Cookie())
	if len(checks()) != checked {
		t.Fatal("a malformed body was checked")
	}
}

// Step-up guesses share mfa:per-account with every other code check of the
// account, so a stolen admin session guesses no faster than the door.
func TestAdminStepUpLimit(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator, session := operatorSignedIn(t, db, f)
	apitest.Must(factory.RateLimits.State(factory.ForLimit("mfa:per-account", operator.User.PublicID)).State(factory.Exhausted(5)).CreateOne(ctx, db, f))(t)
	w := adminCall(t, h, "POST", "/api/admin/step-up", apitest.CodeBody(operator.CodeAt(0)), session.CSRFToken, 429, session.Cookie())
	apitest.RetryWithin(t, w, 15*time.Minute)
	if e := apitest.Reached(t, db, "mfa:per-account:block"); e.Payload["user"] != operator.User.PublicID {
		t.Fatalf("rate_limit.reached: %+v", e)
	}
	if steppedUpUntil(t, db, session) != nil || len(apitest.EventsNamed(t, db, "auth.mfa.checked")) != 0 {
		t.Fatal("a refused step-up checked the code")
	}
}

// Unblocking from the dashboard: an operator, inside the step-up window,
// with a reason, records both events with the admin as actor in the
// delete's transaction, and the address can be invited again. Everything
// else is refused, and a refusal changes nothing.
func TestAdminUnblock(t *testing.T) {
	s, db, box, deliver := mailServer(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	const blocked = "ada@example.com"
	apitest.Must(factory.EmailBlocks.State(factory.Blocked(blocked)).CreateOne(ctx, db, f))(t)
	unblock := func(body string, want int, as factory.AdminSession) *httptest.ResponseRecorder {
		t.Helper()
		return adminCall(t, h, "DELETE", "/api/admin/email-blocks", body, as.CSRFToken, want, as.Cookie())
	}
	isBlocked := func() bool { return apitest.Must(s.Repo.CheckEmailBlocked(ctx, blocked))(t) }
	nothingRecorded := func(when string) {
		t.Helper()
		if len(apitest.EventsNamed(t, db, "admin.email.unblocked"))+len(apitest.EventsNamed(t, db, "email.unblocked")) != 0 || !isBlocked() {
			t.Fatalf("%s: unblocked or recorded", when)
		}
	}

	// A viewer can't, stepped up or not.
	_, viewer := adminSignedIn(t, db, f)
	stepped := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(viewer.UserID)).State(factory.SteppedUp).CreateOne(ctx, db, f))(t)
	for _, v := range []factory.AdminSession{viewer, stepped} {
		if w := unblock(unblockBody(blocked, reason), 403, v); strings.Contains(w.Body.String(), "stepUp") {
			t.Fatalf("a viewer was asked for a code: %s", w.Body.String())
		}
	}
	// An operator outside the window is asked for a code.
	_, cold := operatorSignedIn(t, db, f)
	var asked map[string]any
	apitest.Must(0, json.Unmarshal(unblock(unblockBody(blocked, reason), 403, cold).Body.Bytes(), &asked))(t)
	if asked["stepUp"] != true || asked["error"] != "Enter a code to continue." {
		t.Fatalf("outside the window: %+v", asked)
	}
	// A window that ended is the same.
	ended := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(operator.User.ID)).State(func(s *factory.AdminSession) {
		past := time.Now().Add(-time.Second)
		s.SteppedUpUntil = &past
	}).CreateOne(ctx, db, f))(t)
	unblock(unblockBody(blocked, reason), 403, ended)
	// Without the CSRF token, before anything else.
	adminCall(t, h, "DELETE", "/api/admin/email-blocks", unblockBody(blocked, reason), "", 403, session.Cookie())
	// Neither of those counted against admin-action:per-user.
	if n := apitest.CountRateLimits(t, db, "admin-action:per-user:%"); n != 0 {
		t.Fatalf("refusals before the step-up counted: %d", n)
	}
	// The reason is required: 10 to 500 characters, trimmed, counted as
	// characters (nine é's are 18 bytes, still too short), no NUL.
	// Nor anything that hides or rearranges what the record says: a bidi
	// override, a zero-width space, a line separator, a C1 control, DEL.
	hidden := []string{"\u202e", "\u200b", "\u2028", "\u0085", "\u007f", "\ufeff"}
	refused := []string{"", "   too short   ", strings.Repeat("é", 9), strings.Repeat("x", 501), "ten chars\u0000"}
	for _, c := range hidden {
		refused = append(refused, "A reason long enough "+c+"to pass")
	}
	for _, short := range refused {
		if w := unblock(unblockBody(blocked, short), 422, session); apitest.ErrorField(t, w) != "reason" {
			t.Fatalf("reason %q: %s", short, w.Body.String())
		}
	}
	if w := unblock(unblockBody("not an address", reason), 422, session); apitest.ErrorField(t, w) != "email" {
		t.Fatalf("a malformed address: %s", w.Body.String())
	}
	unblock(`{"email":"ada@example.com"}`+"x", 400, session)
	unblock(`{"email":"ada@example.com","reason":"a reason long enough","by":"x"}`, 400, session)
	// An address that isn't blocked: 404, recording nothing.
	if w := unblock(unblockBody("bob@example.com", reason), 404, session); !strings.Contains(w.Body.String(), "This address isn't blocked.") {
		t.Fatalf("not blocked: %s", w.Body.String())
	}
	nothingRecorded("refused")

	// The right request, the address typed as sign-up would take it: 204.
	multiline := strings.Repeat("é", 10) + "\nSecond line of the reason."
	unblock(unblockBody("  ADA@Example.com ", multiline), 204, session)
	if isBlocked() {
		t.Fatal("still blocked")
	}
	action := apitest.OnlyEvent(t, db, "admin.email.unblocked")
	fact := apitest.OnlyEvent(t, db, "email.unblocked")
	if action.Payload["admin"] != operator.User.PublicID || action.Payload["email"] != blocked || action.Payload["reason"] != multiline {
		t.Fatalf("admin.email.unblocked: %+v", action.Payload)
	}
	if fact.Payload["email"] != blocked || fact.Payload["by"] != "admin" {
		t.Fatalf("email.unblocked: %+v", fact.Payload)
	}
	for _, e := range []apitest.Event{action, fact} {
		if !apitest.Is(e.ActorID, operator.User.ID) || e.UserID.Valid || e.WorkspaceID.Valid || e.IP != "127.0.0.1" {
			t.Fatalf("%s's scope: %+v", e.Name, e)
		}
	}
	// One transaction: PostgreSQL's now() is the transaction's start, so both
	// rows have the same time.
	var times []time.Time
	err := db.NewSelect().TableExpr("events").Column("created_at").Where("name IN (?)", bun.In([]string{"admin.email.unblocked", "email.unblocked"})).Scan(ctx, &times)
	if err != nil || len(times) != 2 || !times[0].Equal(times[1]) {
		t.Fatalf("not one transaction: %v %v", times, err)
	}
	// Again: it isn't blocked any more.
	unblock(unblockBody(blocked, reason), 404, session)
	if len(apitest.EventsNamed(t, db, "admin.email.unblocked")) != 1 {
		t.Fatal("a second unblock recorded")
	}

	// The address gets invitations again.
	owner := apitest.SignedIn(t, db, f)
	workspace := apitest.OwnedWorkspace(t, db, f, owner.UserID)
	apitest.Call(t, h, "POST", apitest.WorkspacePath(workspace.PublicID, "/invitations"), fmt.Sprintf(`{"email":%q,"role":"member"}`, blocked), owner.CSRFToken, owner.Cookie(), 201)
	deliver()
	if len(box.Sent) != 1 || box.Sent[0].To != blocked {
		t.Fatalf("after unblocking, sent: %+v", box.Sent)
	}
	apitest.CheckPayloadIDs(t, db)
}

// admin-action:per-user counts every request past the step-up, an address
// that isn't blocked too, so a stolen session can't probe the block list
// for free.
func TestAdminActionLimit(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	apitest.Must(factory.EmailBlocks.State(factory.Blocked("ada@example.com")).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.RateLimits.State(factory.ForLimit("admin-action:per-user", operator.User.PublicID)).State(factory.Exhausted(59)).CreateOne(ctx, db, f))(t)
	adminCall(t, h, "DELETE", "/api/admin/email-blocks", unblockBody("nobody@example.com", reason), session.CSRFToken, 404, session.Cookie())
	w := adminCall(t, h, "DELETE", "/api/admin/email-blocks", unblockBody("ada@example.com", reason), session.CSRFToken, 429, session.Cookie())
	apitest.RetryWithin(t, w, time.Hour)
	if e := apitest.Reached(t, db, "admin-action:per-user:block"); e.Payload["user"] != operator.User.PublicID {
		t.Fatalf("rate_limit.reached: %+v", e)
	}
	if !apitest.Must(s.Repo.CheckEmailBlocked(ctx, "ada@example.com"))(t) || len(apitest.EventsNamed(t, db, "admin.email.unblocked")) != 0 {
		t.Fatal("unblocked past the limit")
	}
}

// The step-up and its actions work under the previous key too, for a live
// session made under it (a fresh code is still needed); the window ends
// with the session.
func TestAdminStepUpUnderThePreviousKeyAndSignOut(t *testing.T) {
	_, db := server(t)
	previousKey := factory.AdminRouteKey
	s := serverWith(t, db, func(c *config.Config) {
		c.AdminRouteKey = "rotated-" + factory.AdminRouteKey
		c.AdminRouteKeyPrevious = previousKey
	})
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator, session := operatorSignedIn(t, db, f)
	apitest.Must(factory.EmailBlocks.State(factory.Blocked("ada@example.com")).CreateOne(ctx, db, f))(t)
	send := func(method, path, body string, want int) {
		t.Helper()
		if w := adminSend(h, method, path, body, previousKey, session.CSRFToken, session.Cookie()); w.Code != want || w.Header().Get("X-Admin-Key-Status") != "previous" {
			t.Fatalf("%s %s under the previous key: %d %v %s", method, path, w.Code, w.Header(), w.Body.String())
		}
	}
	send("DELETE", "/api/admin/email-blocks", unblockBody("ada@example.com", reason), 403)
	send("POST", "/api/admin/step-up", apitest.CodeBody(operator.CodeAt(0)), 204)
	send("DELETE", "/api/admin/email-blocks", unblockBody("ada@example.com", reason), 204)
	// Signing out deletes the session, and its window with it.
	send("POST", "/api/admin/logout", "", 204)
	left, err := db.NewSelect().Model((*factory.AdminSession)(nil)).Where("user_id = ?", operator.User.ID).Count(ctx)
	if err != nil || left != 0 {
		t.Fatalf("sessions left after signing out: %d %v", left, err)
	}
}

// The code that just signed in at the door can't also step up: the two
// share one check (useConfirmedFactor), so each authenticator step works
// once across both. The next step's code can.
func TestAdminStepUpAfterTheDoor(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator := apitest.WithMFA(t, db, f)
	apitest.Must(factory.Admins.State(factory.AdminFor(operator.User.ID)).State(factory.AdminRole(auth.AdminOperator)).CreateOne(ctx, db, f))(t)
	challenge := apitest.CookieNamed(adminLogin(t, h, operator.User.Email, 200), auth.AdminMFACookieName(false))
	w := adminCall(t, h, "POST", "/api/admin/login/mfa", apitest.CodeBody(operator.CodeAt(0)), "", 200, challenge)
	cookie := apitest.CookieNamed(w, auth.AdminCookieName(false))
	var signedIn apitest.SessionResponse
	apitest.Must(0, json.Unmarshal(w.Body.Bytes(), &signedIn))(t)

	// A new session isn't stepped up by the code it signed in with.
	adminCall(t, h, "DELETE", "/api/admin/email-blocks", unblockBody("ada@example.com", reason), signedIn.CSRF, 403, cookie)
	if w := adminCall(t, h, "POST", "/api/admin/step-up", apitest.CodeBody(operator.CodeAt(0)), signedIn.CSRF, 422, cookie); apitest.ErrorField(t, w) != "code" {
		t.Fatalf("the door's code again: %s", w.Body.String())
	}
	adminCall(t, h, "POST", "/api/admin/step-up", apitest.CodeBody(operator.CodeAt(1)), signedIn.CSRF, 204, cookie)
	adminCall(t, h, "DELETE", "/api/admin/email-blocks", unblockBody("ada@example.com", reason), signedIn.CSRF, 404, cookie)
}

// serverWithoutLimits is a server whose rate limits are off
// (RATE_LIMITS=off), for tests that send more codes than mfa:per-account
// allows.
func serverWithoutLimits(t *testing.T) (http.Handler, *bun.DB) {
	t.Helper()
	_, db := server(t)
	return router.New(serverWith(t, db, func(c *config.Config) { c.RateLimits = config.RateLimitsOff })), db
}

// The session's fifth wrong step-up code ends it (401, cookie cleared,
// admin.session.ended), so a stolen admin cookie gets one burst of
// guesses. A right code in between doesn't reset the count. Other sessions
// of the same admin are untouched.
func TestAdminStepUpFailuresEndTheSession(t *testing.T) {
	h, db := serverWithoutLimits(t)
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator, session := operatorSignedIn(t, db, f)
	other := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(operator.User.ID)).CreateOne(ctx, db, f))(t)
	stepUp := func(code string, want int) *httptest.ResponseRecorder {
		t.Helper()
		return adminCall(t, h, "POST", "/api/admin/step-up", apitest.CodeBody(code), session.CSRFToken, want, session.Cookie())
	}
	for range 4 {
		stepUp("000000", 422)
	}
	stepUp(operator.CodeAt(0), 204)
	if len(apitest.EventsNamed(t, db, "admin.session.ended")) != 0 {
		t.Fatal("ended before the fifth wrong code")
	}
	w := stepUp("000000", 401)
	if c := apitest.CookieNamed(w, auth.AdminCookieName(false)); c == nil || c.MaxAge >= 0 {
		t.Fatalf("cookie not cleared: %+v", c)
	}
	ended := apitest.OnlyEvent(t, db, "admin.session.ended")
	if ended.Payload["admin"] != operator.User.PublicID || ended.Payload["reason"] != "step_up_failures" || !apitest.Is(ended.ActorID, operator.User.ID) || ended.UserID.Valid {
		t.Fatalf("admin.session.ended: %+v", ended)
	}
	// Every wrong code was recorded as a check, the last one too.
	failed := 0
	for _, e := range apitest.EventsNamed(t, db, "auth.mfa.checked") {
		if e.Payload["ok"] == false {
			failed++
		}
	}
	if failed != 5 {
		t.Fatalf("wrong codes recorded: %d", failed)
	}
	// The session is gone: sign in again. The other one carries on.
	adminCall(t, h, "GET", "/api/admin/me", "", "", 401, session.Cookie())
	adminCall(t, h, "GET", "/api/admin/me", "", "", 200, other.Cookie())
	adminCall(t, h, "POST", "/api/admin/step-up", apitest.CodeBody(operator.CodeAt(1)), other.CSRFToken, 204, other.Cookie())
}

// One code steps up once, however many requests carry it at once, across
// the admin's sessions; one address is unblocked once, however many
// requests ask at once.
func TestAdminStepUpAndUnblockUnderConcurrency(t *testing.T) {
	h, db := serverWithoutLimits(t)
	ctx := context.Background()
	f := factory.TestFaker(t)
	for round := range 5 {
		operator, session := operatorSignedIn(t, db, f)
		other := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(operator.User.ID)).CreateOne(ctx, db, f))(t)
		code := operator.CodeAt(0)
		statuses := make([]int, 8)
		var wg sync.WaitGroup
		for i := range statuses {
			as := []factory.AdminSession{session, other}[i%2]
			wg.Go(func() {
				statuses[i] = adminSend(h, "POST", "/api/admin/step-up", apitest.CodeBody(code), factory.AdminRouteKey, as.CSRFToken, as.Cookie()).Code
			})
		}
		wg.Wait()
		if n := countStatus(statuses, 204); n != 1 || countStatus(statuses, 422) != len(statuses)-1 {
			t.Fatalf("round %d: one code stepped up %d times: %v", round, n, statuses)
		}

		email := fmt.Sprintf("racing%d@example.com", round)
		_, stepped := operatorSignedIn(t, db, f, factory.SteppedUp)
		apitest.Must(factory.EmailBlocks.State(factory.Blocked(email)).CreateOne(ctx, db, f))(t)
		statuses = make([]int, 6)
		for i := range statuses {
			wg.Go(func() {
				statuses[i] = adminSend(h, "DELETE", "/api/admin/email-blocks", unblockBody(email, reason), factory.AdminRouteKey, stepped.CSRFToken, stepped.Cookie()).Code
			})
		}
		wg.Wait()
		if countStatus(statuses, 204) != 1 || countStatus(statuses, 404) != len(statuses)-1 {
			t.Fatalf("round %d: unblocks: %v", round, statuses)
		}
	}
	if n := len(apitest.EventsNamed(t, db, "admin.email.unblocked")); n != 5 {
		t.Fatalf("admin.email.unblocked: %d, want one a round", n)
	}
	if n := len(apitest.EventsNamed(t, db, "email.unblocked")); n != 5 {
		t.Fatalf("email.unblocked: %d, want one a round", n)
	}
}

func countStatus(statuses []int, status int) int {
	n := 0
	for _, s := range statuses {
		if s == status {
			n++
		}
	}
	return n
}

// The window ends with its session: a password change and an MFA reset
// delete every admin session, so an unblock then answers 401.
func TestAdminStepUpEndsWithTheSession(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	unblock := func(as factory.AdminSession, want int) {
		t.Helper()
		adminCall(t, h, "DELETE", "/api/admin/email-blocks", unblockBody("ada@example.com", reason), as.CSRFToken, want, as.Cookie())
	}
	apitest.Must(factory.EmailBlocks.State(factory.Blocked("ada@example.com")).CreateOne(ctx, db, f))(t)

	// A password change (with MFA on: a code instead of the password).
	changer, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	app := apitest.Must(factory.SessionFor(ctx, db, f, changer.User.ID))(t)
	body := fmt.Sprintf(`{"newPassword":%q,"code":%q}`, apitest.NewPassword, changer.CodeAt(0))
	apitest.Call(t, h, "PATCH", "/api/auth/password", body, app.CSRFToken, app.Cookie(), 200)
	unblock(session, 401)

	// An MFA reset from the command line.
	reset, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	apitest.Must(s.Repo.ResetMFA(ctx, reset.User.Email))(t)
	unblock(session, 401)

	// Still blocked, nothing recorded.
	if !apitest.Must(s.Repo.CheckEmailBlocked(ctx, "ada@example.com"))(t) || len(apitest.EventsNamed(t, db, "admin.email.unblocked")) != 0 {
		t.Fatal("unblocked by an ended session")
	}
}

// A step-up that reaches the store after its session ended (signed out
// meanwhile) is sql.ErrNoRows, the 401, and uses nothing up: the same code
// still steps up another session, and a wrong one counts nowhere.
func TestAdminStepUpRacingSignOut(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator, session := operatorSignedIn(t, db, f)
	other := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(operator.User.ID)).CreateOne(ctx, db, f))(t)
	adminCall(t, h, "POST", "/api/admin/logout", "", session.CSRFToken, 204, session.Cookie())
	user := store.User{ID: operator.User.ID, PublicID: operator.User.PublicID}
	for _, code := range []string{operator.CodeAt(0), "000000", operator.Codes[0].Code} {
		_, err := s.Repo.StepUpAdminSession(ctx, session.TokenHash, user, s.SecondFactor(user, code))
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("a step-up on a signed-out session: %v", err)
		}
	}
	if len(apitest.EventsNamed(t, db, "admin.session.ended")) != 0 {
		t.Fatal("a gone session ended again")
	}
	adminCall(t, h, "POST", "/api/admin/step-up", apitest.CodeBody(operator.CodeAt(0)), other.CSRFToken, 204, other.Cookie())
	adminCall(t, h, "POST", "/api/admin/step-up", apitest.CodeBody(operator.Codes[0].Code), other.CSRFToken, 204, other.Cookie())
}

// Changing an admin's role closes their step-up windows: an action under
// the new role needs a fresh code.
func TestAdminRoleChangeEndsTheWindow(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	apitest.Must(factory.EmailBlocks.State(factory.Blocked("ada@example.com")).CreateOne(ctx, db, f))(t)
	must2(s.Repo.GrantAdmin(ctx, operator.User.Email, auth.AdminViewer))(t)
	if steppedUpUntil(t, db, session) != nil {
		t.Fatal("the window outlived the role change")
	}
	must2(s.Repo.GrantAdmin(ctx, operator.User.Email, auth.AdminOperator))(t)
	w := adminCall(t, h, "DELETE", "/api/admin/email-blocks", unblockBody("ada@example.com", reason), session.CSRFToken, 403, session.Cookie())
	if !strings.Contains(w.Body.String(), `"stepUp":true`) {
		t.Fatalf("back to operator: %s", w.Body.String())
	}
}
