package admin_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"lewcontext/api/internal/config"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
)

func TestAdminIncidentDiscussions(t *testing.T) {
	for _, kind := range []string{"alert", "error"} {
		t.Run(kind, func(t *testing.T) {
			_, db := server(t)
			s := serverWith(t, db, func(*config.Config) {})
			h := router.New(s)
			ctx := context.Background()
			f := factory.TestFaker(t)
			viewerAccount, viewer := adminSignedIn(t, db, f)
			_, operator := operatorSignedIn(t, db, f)
			_, stepped := operatorSignedIn(t, db, f, factory.SteppedUp)
			var id string
			var parentID int64
			if kind == "alert" {
				row := apitest.Must(factory.Alerts.CreateOne(ctx, db, f))(t)
				id, parentID = row.PublicID, row.ID
			} else {
				row := apitest.Must(factory.AppErrors.CreateOne(ctx, db, f))(t)
				id, parentID = row.PublicID, row.ID
			}
			cycle := apitest.Must(factory.IncidentCycles.State(func(c *factory.IncidentCycle) {
				if kind == "alert" {
					c.AlertID = parentID
				} else {
					c.AppErrorID = parentID
				}
			}).CreateOne(ctx, db, f))(t)
			collection := "/api/admin/" + map[string]string{"alert": "alerts", "error": "errors"}[kind] + "/" + id + "/messages"
			body := func(text string) string {
				b, err := json.Marshal(map[string]string{"cycle": cycle.PublicID, "body": text})
				if err != nil {
					t.Fatal(err)
				}
				return string(b)
			}
			for _, text := range []string{"   ", strings.Repeat("界", 2001), "hidden\u202eoverride", "nul\x00text"} {
				adminCall(t, h, "POST", collection, body(text), viewer.CSRFToken, 422, viewer.Cookie())
			}
			adminCall(t, h, "POST", collection, `{"cycle":"`+cycle.PublicID+`","body":"valid","resolved":true}`, viewer.CSRFToken, 400, viewer.Cookie())
			adminCall(t, h, "POST", collection, body("valid"), "", 403, viewer.Cookie())
			w := adminCall(t, h, "POST", collection, body("  "+strings.Repeat("界", 2000)+"  "), viewer.CSRFToken, 201, viewer.Cookie())
			var msg store.IncidentMessage
			if err := json.Unmarshal(w.Body.Bytes(), &msg); err != nil {
				t.Fatal(err)
			}
			if msg.Body == nil || len([]rune(*msg.Body)) != 2000 || msg.Author == nil || msg.Author.ID != viewerAccount.User.PublicID {
				t.Fatalf("saved: %s", w.Body.String())
			}
			event := apitest.OnlyEvent(t, db, "admin."+kind+".commented")
			if event.Payload["cycle"] != cycle.PublicID || event.Payload["message"] != msg.PublicID || event.Payload["body"] != nil || strings.Contains(string(eventJSON(t, event.Payload)), "界") {
				t.Fatalf("audit copied body: %+v", event.Payload)
			}
			adminCall(t, h, "GET", collection+"?cycle="+cycle.PublicID, "", "", 200, viewer.Cookie())
			viewed := apitest.OnlyEvent(t, db, "admin."+kind+".viewed")
			if viewed.Payload["section"] != "messages" || viewed.Payload["cycle"] != cycle.PublicID {
				t.Fatalf("read audit: %+v", viewed.Payload)
			}
			for _, query := range []string{"", "?cycle=" + cycle.PublicID + "&cycle=" + cycle.PublicID, "?cycle=" + cycle.PublicID + "&extra=1", "?cycle=" + cycle.PublicID + "&perPage=101"} {
				adminCall(t, h, "GET", collection+query, "", "", 400, viewer.Cookie())
			}
			redact := collection + "/" + msg.PublicID + "/redaction"
			reason := `{"reason":"Sensitive investigation material removed"}`
			adminCall(t, h, "POST", redact, reason, viewer.CSRFToken, 403, viewer.Cookie())
			adminCall(t, h, "POST", redact, reason, operator.CSRFToken, 403, operator.Cookie())
			adminCall(t, h, "POST", redact, `{"reason":"short"}`, stepped.CSRFToken, 422, stepped.Cookie())
			w = adminCall(t, h, "POST", redact, reason, stepped.CSRFToken, 201, stepped.Cookie())
			if strings.Contains(w.Body.String(), "界") || !strings.Contains(w.Body.String(), `"body":null`) {
				t.Fatalf("redaction retained body: %s", w.Body.String())
			}
			redaction := apitest.OnlyEvent(t, db, "admin."+kind+".redacted")
			if redaction.Payload["message"] != msg.PublicID || redaction.Payload["body"] != nil {
				t.Fatalf("redaction audit: %+v", redaction.Payload)
			}
			// The feature budget is shared across both parent resources.
			busyAccount, busy := adminSignedIn(t, db, f)
			apitest.Must(factory.RateLimits.State(factory.ForLimit("incident-comment:per-user", busyAccount.User.PublicID)).State(factory.Exhausted(60)).CreateOne(ctx, db, f))(t)
			adminCall(t, h, "POST", collection, body("refused by shared budget"), busy.CSRFToken, 429, busy.Cookie())
		})
	}
}

func eventJSON(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
