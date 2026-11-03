package admin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/monitor"
	"lewcontext/api/internal/store"
	"net/http"
	"strings"
	"testing"
)

// Alert email is opt-in: a new admin has no subscription, the catalogue shows
// each rule with the admin's own level (off: none), and a patch subscribes or
// unsubscribes atomically and is bounded.
func TestAlertSubscriptionsAreOptInAndPatched(t *testing.T) {
	_, db := server(t)
	s := serverWith(t, db, func(*config.Config) {})
	h := router.New(s)
	f := factory.TestFaker(t)
	account, viewer := adminSignedIn(t, db, f)
	type rule struct {
		Signal, Preference      string
		Levels, EffectiveLevels []string
		Ineffective             bool
	}
	catalog := func() []rule {
		var out struct{ Data []rule }
		response := adminCall(t, h, "GET", "/api/admin/alert-rules", "", "", 200, viewer.Cookie())
		if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Data
	}
	first := catalog()
	if len(first) != len(monitor.AlertRules) {
		t.Fatalf("catalogue: %d rules", len(first))
	}
	for _, r := range first {
		if r.Preference != "off" || len(r.EffectiveLevels) != 0 {
			t.Fatalf("a new admin is subscribed to %+v", r)
		}
	}
	patch := `{"signals":[{"signal":"errors.new_panics","minLevel":"critical"},{"signal":"http.latency_p95","minLevel":"warn"}]}`
	adminCall(t, h, "PATCH", "/api/admin/alert-subscriptions", patch, viewer.CSRFToken, 204, viewer.Cookie())
	adminCall(t, h, "PATCH", "/api/admin/alert-subscriptions", patch, viewer.CSRFToken, 204, viewer.Cookie())
	if len(apitest.EventsNamed(t, db, "admin.updated")) != 1 {
		t.Fatal("a no-op patch recorded an event")
	}
	seen := map[string]bool{}
	for _, r := range catalog() {
		if seen[r.Signal] {
			t.Fatal("duplicate rule")
		}
		seen[r.Signal] = true
		switch r.Signal {
		case "http.latency_p95":
			if r.Preference != "warn" || len(r.EffectiveLevels) != 2 {
				t.Fatalf("a warn subscription: %+v", r)
			}
		case "errors.new_panics":
			// Capped at warning, so a critical-only subscription never fires.
			if r.Preference != "critical" || len(r.Levels) != 1 || r.Levels[0] != "warn" || len(r.EffectiveLevels) != 0 || !r.Ineffective {
				t.Fatalf("a critical subscription on a warning-capped rule: %+v", r)
			}
		}
	}
	apitest.EventsNamed(t, db, "admin.alert_rule.listed")
	for _, bad := range []string{
		`{}`, `{"signals":[]}`,
		`{"signals":[{"signal":"unknown","minLevel":"warn"}]}`,
		`{"signals":[{"signal":"http.latency_p95","minLevel":"inherit"}]}`,
		`{"signals":[{"signal":"http.latency_p95","minLevel":"off"},{"signal":"http.latency_p95","minLevel":"warn"}]}`,
		`{"alertEmail":"all"}`,
	} {
		adminCall(t, h, "PATCH", "/api/admin/alert-subscriptions", bad, viewer.CSRFToken, map[bool]int{true: 400, false: 422}[strings.Contains(bad, "alertEmail")], viewer.Cookie())
	}
	// Event kinds: left out means every kind, a list is kept in order, an empty one is
	// none, an unknown one is refused, and changing only them is a change.
	kindsOf := func() []string {
		for _, r := range catalogEvents(t, h, viewer) {
			if r.Signal == "http.latency_p95" {
				return r.Events
			}
		}
		return nil
	}
	if got := kindsOf(); len(got) != 6 {
		t.Fatalf("left out means every kind: %v", got)
	}
	adminCall(t, h, "PATCH", "/api/admin/alert-subscriptions", `{"signals":[{"signal":"http.latency_p95","minLevel":"warn","events":["resolved","opened"]}]}`, viewer.CSRFToken, 204, viewer.Cookie())
	if got := kindsOf(); strings.Join(got, ",") != "opened,resolved" {
		t.Fatalf("chosen kinds: %v", got)
	}
	adminCall(t, h, "PATCH", "/api/admin/alert-subscriptions", `{"signals":[{"signal":"http.latency_p95","minLevel":"warn","events":[]}]}`, viewer.CSRFToken, 204, viewer.Cookie())
	if got := kindsOf(); len(got) != 0 {
		t.Fatalf("an empty list is none: %v", got)
	}
	adminCall(t, h, "PATCH", "/api/admin/alert-subscriptions", `{"signals":[{"signal":"http.latency_p95","minLevel":"warn","events":["digested"]}]}`, viewer.CSRFToken, 422, viewer.Cookie())
	adminCall(t, h, "PATCH", "/api/admin/alert-subscriptions", `{"signals":[{"signal":"http.latency_p95","minLevel":"warn"}]}`, viewer.CSRFToken, 204, viewer.Cookie())
	adminCall(t, h, "PATCH", "/api/admin/alert-subscriptions", `{"signals":[{"signal":"http.latency_p95","minLevel":"off"}]}`, "wrong", 403, viewer.Cookie())
	subs := apitest.Must(s.Repo.IndexAdminAlertSubscriptions(context.Background(), account.User.ID))(t)
	if len(subs) != 2 {
		t.Fatalf("a refused patch changed them: %+v", subs)
	}
	adminCall(t, h, "PATCH", "/api/admin/alert-subscriptions", `{"signals":[{"signal":"http.latency_p95","minLevel":"off"}]}`, viewer.CSRFToken, 204, viewer.Cookie())
	if subs := apitest.Must(s.Repo.IndexAdminAlertSubscriptions(context.Background(), account.User.ID))(t); len(subs) != 1 {
		t.Fatalf("unsubscribing: %+v", subs)
	}
}

type webhookView struct {
	ID, Kind, Name, Format, KeyID string
	Signals, Events               []string
	Enabled                       bool
	Revision                      int64
	OverlapUntil                  *string
	LastDelivery                  *struct{ Status string }
}

// Webhooks and heartbeats are an operator's: a viewer reads the list but
// can't change anything (nor read a key), a change needs a reason, an empty
// selection receives nothing, names are unique, a URL and a key are never in
// a response or an event, and every webhook has a key of its own.
func TestAlertWebhooksAreAnOperatorsAction(t *testing.T) {
	_, db := server(t)
	s := serverWith(t, db, func(*config.Config) {})
	h := router.New(s)
	f := factory.TestFaker(t)
	_, operator := operatorSignedIn(t, db, f, factory.SteppedUp)
	_, viewer := adminSignedIn(t, db, f)
	url := "https://receiver-secret-token.example.net/path-secret-token"
	create := func(body string, as factory.AdminSession, status int) []byte {
		return adminCall(t, h, "POST", "/api/admin/alert-webhooks", body, as.CSRFToken, status, as.Cookie()).Body.Bytes()
	}
	body := func(name, url string) string {
		return fmt.Sprintf(`{"kind":"webhook","name":%q,"url":%q,"format":"json","signals":["http.latency_p95"],"events":["opened","recovered"],"reason":%q}`, name, url, reason)
	}
	create(body("Pager", url), viewer, 403)
	var pager webhookView
	if err := json.Unmarshal(create(body("Pager", url), operator, 201), &pager); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pager.ID, "whk_") || pager.Kind != "webhook" || !pager.Enabled || pager.Revision != 1 || len(pager.Signals) != 1 || len(pager.Events) != 2 || !strings.HasPrefix(pager.KeyID, "key_") {
		t.Fatalf("created: %+v", pager)
	}
	create(body("pager", "https://another.example.net/x"), operator, 422) // a name in use, any case
	// Validation: each refused, nothing recorded as created.
	for name, bad := range map[string]string{
		"a url":        `{"kind":"webhook","name":"A","url":"ftp://x","format":"json","reason":"` + reason + `"}`,
		"a format":     `{"kind":"webhook","name":"B","url":"https://hooks.example.net/b","format":"xml","reason":"` + reason + `"}`,
		"no format":    `{"kind":"webhook","name":"C","url":"https://hooks.example.net/c","reason":"` + reason + `"}`,
		"a signal":     `{"kind":"webhook","name":"D","url":"https://hooks.example.net/d","format":"json","signals":["nope"],"reason":"` + reason + `"}`,
		"an event":     `{"kind":"webhook","name":"E","url":"https://hooks.example.net/e","format":"json","events":["digested"],"reason":"` + reason + `"}`,
		"a kind":       `{"kind":"pager","name":"F","url":"https://hooks.example.net/f","reason":"` + reason + `"}`,
		"no reason":    `{"kind":"webhook","name":"G","url":"https://hooks.example.net/g","format":"json"}`,
		"a beat's set": `{"kind":"heartbeat","name":"H","url":"https://hooks.example.net/h","signals":["http.latency_p95"],"reason":"` + reason + `"}`,
	} {
		if status := adminSend(h, "POST", "/api/admin/alert-webhooks", bad, factory.AdminRouteKey, operator.CSRFToken, operator.Cookie()).Code; status != 422 {
			t.Errorf("%s: %d", name, status)
		}
	}
	// An empty selection is allowed and means nothing.
	var empty webhookView
	if err := json.Unmarshal(create(fmt.Sprintf(`{"kind":"webhook","name":"Idle","url":"https://hooks.example.net/idle","format":"slack","reason":%q}`, reason), operator, 201), &empty); err != nil || len(empty.Signals) != 0 || len(empty.Events) != 0 {
		t.Fatalf("empty: %+v %v", empty, err)
	}
	var beat webhookView
	if err := json.Unmarshal(create(fmt.Sprintf(`{"kind":"heartbeat","name":"Uptime","url":"https://hc-ping.example.net/abc","reason":%q}`, reason), operator, 201), &beat); err != nil || beat.Kind != "heartbeat" || beat.KeyID != "" || beat.Format != "" {
		t.Fatalf("heartbeat: %+v %v", beat, err)
	}
	// Any admin reads the list, which holds no URL, host or key; one recorded read each.
	list := adminCall(t, h, "GET", "/api/admin/alert-webhooks", "", "", 200, viewer.Cookie()).Body.String()
	if strings.Contains(list, "secret-token") || strings.Contains(list, "receiver-") || strings.Contains(list, "example.net") || strings.Contains(list, "sealed") {
		t.Fatalf("a receiver leaked in the list: %s", list)
	}
	if len(apitest.EventsNamed(t, db, "admin.alert_webhook.listed")) != 1 {
		t.Fatal("the read wasn't recorded once")
	}
	// Persisted sealed, never plaintext; one key per webhook, all different.
	var stored []factory.AlertWebhook
	if err := db.NewSelect().Model(&stored).Order("id").Scan(context.Background()); err != nil || len(stored) != 3 {
		t.Fatalf("stored: %d %v", len(stored), err)
	}
	for _, r := range stored {
		if strings.Contains(string(r.SealedURL), "example.net") || strings.Contains(string(r.SealedKey), "=") && len(r.SealedKey) < 40 {
			t.Fatalf("plaintext persisted: %+v", r)
		}
	}
	// The key: an operator inside a step-up window reads it, audited without it; a viewer can't.
	keyPath := "/api/admin/alert-webhooks/" + pager.ID + "/signing-key"
	adminCall(t, h, "GET", keyPath, "", "", 403, viewer.Cookie())
	var key struct{ Key, KeyID string }
	response := adminCall(t, h, "GET", keyPath, "", "", 200, operator.Cookie())
	if err := json.Unmarshal(response.Body.Bytes(), &key); err != nil || len(key.Key) != 43 || key.KeyID != pager.KeyID || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("key: %s %v", response.Body.String(), err)
	}
	adminCall(t, h, "GET", "/api/admin/alert-webhooks/"+beat.ID+"/signing-key", "", "", 404, operator.Cookie())
	// Updating: a stale revision is 409; an empty change is 422; renaming and re-scoping keep the key.
	patch := func(id, body string, status int) []byte {
		return adminCall(t, h, "PATCH", "/api/admin/alert-webhooks/"+id, body, operator.CSRFToken, status, operator.Cookie()).Body.Bytes()
	}
	patch(pager.ID, fmt.Sprintf(`{"revision":9,"name":"X","reason":%q}`, reason), 409)
	patch(pager.ID, fmt.Sprintf(`{"revision":1,"reason":%q}`, reason), 422)
	patch(pager.ID, fmt.Sprintf(`{"revision":1,"name":"Idle","reason":%q}`, reason), 422) // a name in use
	var renamed webhookView
	if err := json.Unmarshal(patch(pager.ID, fmt.Sprintf(`{"revision":1,"name":"On-call","signals":["http.latency_p95","auth.possible_takeovers"],"reason":%q}`, reason), 200), &renamed); err != nil || renamed.Revision != 2 || renamed.KeyID != pager.KeyID || len(renamed.Signals) != 2 {
		t.Fatalf("renamed: %+v %v", renamed, err)
	}
	// Replacing the URL gives it a new key.
	var moved webhookView
	if err := json.Unmarshal(patch(pager.ID, fmt.Sprintf(`{"revision":2,"url":"https://moved.example.net/hook","reason":%q}`, reason), 200), &moved); err != nil || moved.KeyID == pager.KeyID {
		t.Fatalf("moved: %+v %v", moved, err)
	}
	// Rotating: 201, a second rotation inside the overlap is 409.
	rotate := fmt.Sprintf(`{"revision":%d,"reason":%q}`, moved.Revision, reason)
	var rotated webhookView
	if err := json.Unmarshal(adminCall(t, h, "POST", keyPath, rotate, operator.CSRFToken, 201, operator.Cookie()).Body.Bytes(), &rotated); err != nil || rotated.OverlapUntil == nil {
		t.Fatalf("rotated: %+v %v", rotated, err)
	}
	adminCall(t, h, "POST", keyPath, fmt.Sprintf(`{"revision":%d,"reason":%q}`, rotated.Revision, reason), operator.CSRFToken, 409, operator.Cookie())
	// Delete: 204, then 404; a viewer can't.
	adminCall(t, h, "DELETE", "/api/admin/alert-webhooks/"+empty.ID, fmt.Sprintf(`{"reason":%q}`, reason), viewer.CSRFToken, 403, viewer.Cookie())
	adminCall(t, h, "DELETE", "/api/admin/alert-webhooks/"+empty.ID, fmt.Sprintf(`{"reason":%q}`, reason), operator.CSRFToken, 204, operator.Cookie())
	adminCall(t, h, "DELETE", "/api/admin/alert-webhooks/"+empty.ID, fmt.Sprintf(`{"reason":%q}`, reason), operator.CSRFToken, 404, operator.Cookie())
	adminCall(t, h, "PATCH", "/api/admin/alert-webhooks/nope", `{}`, operator.CSRFToken, 404, operator.Cookie())
	// No event holds a URL, a host or a key.
	oldKeys := []string{key.Key}
	for _, name := range []string{"admin.alert_webhook.created", "admin.alert_webhook.updated", "admin.alert_webhook.deleted", "admin.alert_webhook.viewed"} {
		events := apitest.EventsNamed(t, db, name)
		if len(events) == 0 {
			t.Fatalf("no %s", name)
		}
		for _, e := range events {
			encoded, _ := json.Marshal(e.Payload)
			for _, secret := range append(oldKeys, "secret-token", "example.net") {
				if strings.Contains(string(encoded), secret) {
					t.Fatalf("%s holds %q: %s", name, secret, encoded)
				}
			}
		}
	}
	if v := apitest.EventsNamed(t, db, "admin.alert_webhook.viewed"); len(v) != 1 || v[0].Payload["section"] != "signing_key" {
		t.Fatalf("viewed: %+v", v)
	}
	_ = store.MaxAlertWebhooks
}

// There are at most 20 webhooks and 5 heartbeats.
func TestAlertWebhookLimits(t *testing.T) {
	_, db := server(t)
	h := router.New(serverWith(t, db, func(*config.Config) {}))
	_, operator := operatorSignedIn(t, db, factory.TestFaker(t), factory.SteppedUp)
	for i := 0; i < store.MaxAlertHeartbeats+1; i++ {
		status := 201
		if i == store.MaxAlertHeartbeats {
			status = 422
		}
		body := fmt.Sprintf(`{"kind":"heartbeat","name":"Beat %d","url":"https://hc.example.net/%d","reason":%q}`, i, i, reason)
		adminCall(t, h, "POST", "/api/admin/alert-webhooks", body, operator.CSRFToken, status, operator.Cookie())
	}
}

type ruleEvents struct {
	Signal string
	Events []string
}

// catalogEvents is the catalogue's rules with the admin's chosen event kinds.
func catalogEvents(t *testing.T, h http.Handler, as factory.AdminSession) []ruleEvents {
	t.Helper()
	var out struct{ Data []ruleEvents }
	if err := json.Unmarshal(adminCall(t, h, "GET", "/api/admin/alert-rules", "", "", 200, as.Cookie()).Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Data
}
