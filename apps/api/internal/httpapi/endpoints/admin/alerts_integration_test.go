package admin_test

import (
	"context"
	"encoding/json"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"testing"
	"time"
)

// The Alerts page: open alerts critical first, resolved and all by a
// cursor, one alert; each read recorded once; bad parameters and unknown
// IDs refused; viewers read; /me carries the open count and the admin's
// email setting, which they change themselves.
func TestAdminAlerts(t *testing.T) {
	_, db := server(t)
	h := router.New(serverWith(t, db, func(*config.Config) {}))
	ctx := context.Background()
	f := factory.TestFaker(t)
	viewerAccount, viewer := adminSignedIn(t, db, f)
	now := time.Now()
	warn := apitest.Must(factory.Alerts.State(factory.AlertOpenedAt(now.Add(-time.Minute))).CreateOne(ctx, db, f))(t)
	crit := apitest.Must(factory.Alerts.State(factory.AlertOn("admin.door_refusals", "critical")).State(factory.AlertOpenedAt(now.Add(-time.Hour))).CreateOne(ctx, db, f))(t)
	old := apitest.Must(factory.Alerts.State(factory.AlertOn("http.latency_p95", "warn")).State(factory.AlertResolvedAt(now.Add(-time.Hour))).CreateOne(ctx, db, f))(t)
	type page struct {
		Data []struct {
			ID, Signal, Level, Title, Meaning string
			ResolvedAt                        *time.Time
		}
		Next *string
	}
	list := func(query string, want int) page {
		t.Helper()
		w := adminCall(t, h, "GET", "/api/admin/alerts"+query, "", "", want, viewer.Cookie())
		var p page
		if want == 200 {
			p = decodeAdmin[page](t, w.Body.Bytes())
		}
		return p
	}
	open := list("", 200)
	if len(open.Data) != 2 || open.Data[0].ID != crit.PublicID || open.Data[1].ID != warn.PublicID || open.Data[0].Title != "Admin door refusals" || open.Data[0].Meaning == "" {
		t.Fatalf("open: %+v", open)
	}
	if got := list("?state=resolved", 200); len(got.Data) != 1 || got.Data[0].ID != old.PublicID {
		t.Fatalf("resolved: %+v", got)
	}
	first := list("?state=all&perPage=2", 200)
	if len(first.Data) != 2 || first.Next == nil || first.Data[0].ID != warn.PublicID {
		t.Fatalf("all, page 1: %+v", first)
	}
	if second := list("?state=all&perPage=2&before="+*first.Next, 200); len(second.Data) != 1 || second.Next != nil {
		t.Fatalf("all, page 2: %+v", second)
	}
	for _, bad := range []string{"?state=x", "?perPage=0", "?perPage=1000", "?before=nope", "?state=open&before=" + crit.PublicID, "?q=1", "?state=all&state=open", "?state=all&before=alt_00000000000000"} {
		list(bad, 400)
	}
	if reads := apitest.EventsNamed(t, db, "admin.alert.listed"); len(reads) != 4 || reads[0].Payload["state"] != "open" || reads[0].Payload["admin"] != viewerAccount.User.PublicID || reads[0].UserID.Valid || reads[0].WorkspaceID.Valid {
		t.Fatalf("admin.alert.listed: %+v", reads)
	}
	// One alert.
	w := adminCall(t, h, "GET", "/api/admin/alerts/"+crit.PublicID, "", "", 200, viewer.Cookie())
	var one struct {
		ID, Signal, Page string
		Details          map[string]any
	}
	if err := json.Unmarshal(w.Body.Bytes(), &one); err != nil || one.ID != crit.PublicID || one.Page != "security" {
		t.Fatalf("show: %s %v", w.Body.String(), err)
	}
	adminCall(t, h, "GET", "/api/admin/alerts/alt_00000000000000", "", "", 404, viewer.Cookie())
	adminCall(t, h, "GET", "/api/admin/alerts/nope", "", "", 404, viewer.Cookie())
	if v := apitest.OnlyEvent(t, db, "admin.alert.viewed"); v.Payload["alert"] != crit.PublicID {
		t.Fatalf("admin.alert.viewed: %+v", v.Payload)
	}
	// /me: the count and the setting; changing it is the admin's own.
	me := func() map[string]any {
		var out map[string]any
		_ = json.Unmarshal(adminCall(t, h, "GET", "/api/admin/me", "", "", 200, viewer.Cookie()).Body.Bytes(), &out)
		return out
	}
	if m := me(); m["openAlerts"] != float64(2) || m["alertEmail"] != "critical" {
		t.Fatalf("me: %+v", m)
	}
	for _, bad := range []string{`{"alertEmail":"sometimes"}`, `{}`} {
		adminCall(t, h, "PATCH", "/api/admin/me", bad, viewer.CSRFToken, 422, viewer.Cookie())
	}
	adminCall(t, h, "PATCH", "/api/admin/me", `{"alertEmail":"all"}`, viewer.CSRFToken, 204, viewer.Cookie())
	adminCall(t, h, "PATCH", "/api/admin/me", `{"alertEmail":"all"}`, viewer.CSRFToken, 204, viewer.Cookie())
	adminCall(t, h, "PATCH", "/api/admin/me", `{"alertEmail":"none"}`, "wrong", 403, viewer.Cookie())
	if m := me(); m["alertEmail"] != "all" {
		t.Fatalf("me: %+v", m)
	}
	if e := apitest.OnlyEvent(t, db, "admin.updated"); e.Payload["changes"].(map[string]any)["alertEmail"] == nil || !e.ActorID.Valid {
		t.Fatalf("admin.updated: %+v", e)
	}
	// Without a session, nothing.
	adminCall(t, h, "GET", "/api/admin/alerts", "", "", 401)
}

// Acknowledging is housekeeping: an operator, no code, no reason; a viewer
// 403; once only; not a resolved alert. Silencing is an action: a code and
// a reason, up to 24 hours, carried to the signal's next alert. Resolving
// an error group is housekeeping too. Each records its event.
func TestAdminAlertHousekeeping(t *testing.T) {
	_, db := server(t)
	h := router.New(serverWith(t, db, func(*config.Config) {}))
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator, session := operatorSignedIn(t, db, f)
	_, viewer := adminSignedIn(t, db, f)
	alert := apitest.Must(factory.Alerts.CreateOne(ctx, db, f))(t)
	resolved := apitest.Must(factory.Alerts.State(factory.AlertOn("http.latency_p95", "warn")).State(factory.AlertResolvedAt(time.Now())).CreateOne(ctx, db, f))(t)
	path := "/api/admin/alerts/" + alert.PublicID

	adminCall(t, h, "PATCH", path, `{"acknowledged":true}`, viewer.CSRFToken, 403, viewer.Cookie())
	adminCall(t, h, "PATCH", path, `{"acknowledged":false}`, session.CSRFToken, 422, session.Cookie())
	adminCall(t, h, "PATCH", path, `{}`, session.CSRFToken, 422, session.Cookie())
	adminCall(t, h, "PATCH", "/api/admin/alerts/alt_00000000000000", `{"acknowledged":true}`, session.CSRFToken, 404, session.Cookie())
	adminCall(t, h, "PATCH", "/api/admin/alerts/"+resolved.PublicID, `{"acknowledged":true}`, session.CSRFToken, 409, session.Cookie())
	if len(apitest.EventsNamed(t, db, "admin.alert.acknowledged")) != 0 {
		t.Fatal("a refusal was recorded")
	}
	w := adminCall(t, h, "PATCH", path, `{"acknowledged":true}`, session.CSRFToken, 200, session.Cookie())
	var got struct {
		AcknowledgedAt *time.Time
		AcknowledgedBy *struct{ ID string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.AcknowledgedAt == nil || got.AcknowledgedBy == nil || got.AcknowledgedBy.ID != operator.User.PublicID {
		t.Fatalf("acknowledged: %s", w.Body.String())
	}
	adminCall(t, h, "PATCH", path, `{"acknowledged":true}`, session.CSRFToken, 409, session.Cookie())
	if e := apitest.OnlyEvent(t, db, "admin.alert.acknowledged"); e.Payload["alert"] != alert.PublicID || e.Payload["admin"] != operator.User.PublicID || e.Payload["signal"] != alert.Signal {
		t.Fatalf("admin.alert.acknowledged: %+v", e.Payload)
	}

	// Silencing: a code first, then a reason and a length.
	silence := path + "/silence"
	adminCall(t, h, "POST", silence, reasonBody(reason, "duration", "4h"), session.CSRFToken, 403, session.Cookie())
	_, stepped := operatorSignedIn(t, db, f, factory.SteppedUp)
	adminCall(t, h, "POST", silence, reasonBody(reason, "duration", "48h"), stepped.CSRFToken, 422, stepped.Cookie())
	adminCall(t, h, "POST", silence, reasonBody("short", "duration", "4h"), stepped.CSRFToken, 422, stepped.Cookie())
	adminCall(t, h, "POST", "/api/admin/alerts/"+resolved.PublicID+"/silence", reasonBody(reason, "duration", "4h"), stepped.CSRFToken, 409, stepped.Cookie())
	w = adminCall(t, h, "POST", silence, reasonBody(reason, "duration", "4h"), stepped.CSRFToken, 201, stepped.Cookie())
	var silenced struct{ SilencedUntil *time.Time }
	if err := json.Unmarshal(w.Body.Bytes(), &silenced); err != nil || silenced.SilencedUntil == nil || time.Until(*silenced.SilencedUntil) < 3*time.Hour+50*time.Minute || time.Until(*silenced.SilencedUntil) > 4*time.Hour+time.Minute {
		t.Fatalf("silenced: %s", w.Body.String())
	}
	if e := apitest.OnlyEvent(t, db, "admin.alert.silenced"); e.Payload["reason"] != reason || e.Payload["signal"] != alert.Signal || e.Payload["until"] == nil {
		t.Fatalf("admin.alert.silenced: %+v", e.Payload)
	}

	// An error group: resolved once, recorded, reopened by a new occurrence
	// (store tests); a refusal records nothing.
	group := apitest.Must(factory.AppErrors.CreateOne(ctx, db, f))(t)
	errPath := "/api/admin/errors/" + group.PublicID
	adminCall(t, h, "PATCH", errPath, `{"resolved":true}`, viewer.CSRFToken, 403, viewer.Cookie())
	adminCall(t, h, "PATCH", errPath, `{"resolved":false}`, session.CSRFToken, 422, session.Cookie())
	adminCall(t, h, "PATCH", "/api/admin/errors/err_00000000000000", `{"resolved":true}`, session.CSRFToken, 404, session.Cookie())
	adminCall(t, h, "PATCH", "/api/admin/errors/nope", `{"resolved":true}`, session.CSRFToken, 404, session.Cookie())
	adminCall(t, h, "PATCH", errPath, `{"resolved":true}`, session.CSRFToken, 204, session.Cookie())
	adminCall(t, h, "PATCH", errPath, `{"resolved":true}`, session.CSRFToken, 409, session.Cookie())
	if e := apitest.OnlyEvent(t, db, "admin.error.resolved"); e.Payload["error"] != group.PublicID || e.Payload["admin"] != operator.User.PublicID {
		t.Fatalf("admin.error.resolved: %+v", e.Payload)
	}
	// admin-action:per-user counts them, 60 an hour.
	busy, busySession := operatorSignedIn(t, db, f)
	apitest.Must(factory.RateLimits.State(factory.ForLimit("admin-action:per-user", busy.User.PublicID)).State(factory.Exhausted(60)).CreateOne(ctx, db, f))(t)
	adminCall(t, h, "PATCH", "/api/admin/alerts/"+resolved.PublicID, `{"acknowledged":true}`, busySession.CSRFToken, 429, busySession.Cookie())
}
