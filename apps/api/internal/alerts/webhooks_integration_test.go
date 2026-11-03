package alerts

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testSealer is the receivers' sealer, with a key of the tests' own.
func testSealer(t *testing.T) *auth.DestinationSealer {
	t.Helper()
	return apitest.Must(auth.NewDestinationSealer(bytes.Repeat([]byte{7}, auth.AppKeySize)))(t)
}

// addWebhook adds an enabled webhook, as an operator would, receiving the
// signals and event kinds given (none: nothing).
func addWebhook(t *testing.T, s *store.Store, name, url, format string, signals, kinds []string) store.AlertWebhook {
	t.Helper()
	admin := adminUser(t, s.DB)
	in := store.AlertWebhookInput{Kind: "webhook", Name: &name, URL: &url, Format: &format, Signals: &signals, Events: &kinds}
	return apitest.Must(s.CreateAlertWebhook(context.Background(), in, testSealer(t), store.AdminAction{Admin: admin, Reason: "Testing alerts to a receiver"}))(t)
}

func addHeartbeat(t *testing.T, s *store.Store, name, url string) store.AlertWebhook {
	t.Helper()
	admin := adminUser(t, s.DB)
	in := store.AlertWebhookInput{Kind: "heartbeat", Name: &name, URL: &url}
	return apitest.Must(s.CreateAlertWebhook(context.Background(), in, testSealer(t), store.AdminAction{Admin: admin, Reason: "Testing the heartbeat"}))(t)
}

// keyOf reads a webhook's signing key and its public ID.
func keyOf(t *testing.T, s *store.Store, id string) (string, string) {
	t.Helper()
	key, keyID, err := s.FindAlertWebhookSigningKey(context.Background(), id, testSealer(t))
	if err != nil {
		t.Fatal(err)
	}
	return key, keyID
}

func queuedFor(w store.AlertWebhook) events.Delivered {
	return events.Delivered{EventID: "evt_0123456789abcd", Target: w.ID, TargetRevision: 1}
}

func hook(s *store.Store, t *testing.T, guard bool, timeout time.Duration, now func() time.Time) Webhook {
	return Webhook{Store: s, Sealer: testSealer(t), Client: NewClient(timeout, guard), Now: now}
}

func hmacOf(key string, stamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(stamp + "."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// A webhook's post is signed with its own key, labelled by its public ID; two
// webhooks never share a key; for 15 minutes after a rotation a second
// signature with the previous key goes over the same body.
func TestWebhookPostsSignedJSONWithItsOwnKey(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()
	rc, other := &receiver{status: 204}, &receiver{status: 204}
	srv, srv2 := httptest.NewServer(rc), httptest.NewServer(other)
	defer srv.Close()
	defer srv2.Close()
	a := addWebhook(t, s, "Pager", srv.URL+secretPath, FormatJSON, []string{"test.x"}, []string{"opened"})
	b := addWebhook(t, s, "Chat", srv2.URL, FormatJSON, []string{"test.x"}, []string{"opened"})
	keyA, idA := keyOf(t, s, a.ID)
	keyB, idB := keyOf(t, s, b.ID)
	if keyA == keyB || idA == idB || idA != a.KeyID || !strings.HasPrefix(idA, "key_") || len(idA) != 12 {
		t.Fatalf("keys: %q %q ids %q %q", keyA, keyB, idA, idB)
	}
	at := time.Unix(1793543400, 0)
	w := hook(s, t, false, WebhookTimeout, func() time.Time { return at })
	if err := w.Post(ctx, notice, queuedFor(a)); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(rc.bodies[0], &got); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"alert", "event", "state", "signal", "title", "level", "value", "unit", "openedAt", "resolvedAt", "meaning"} {
		if _, ok := got[field]; !ok {
			t.Errorf("no %s in %s", field, rc.bodies[0])
		}
	}
	h := rc.headers[0]
	if h.Get(SignatureHeader) != "t=1793543400,v1="+hmacOf(keyA, "1793543400", rc.bodies[0]) || h.Get(KeyIDHeader) != idA ||
		h.Get("Content-Type") != "application/json" || h.Get(DeliveryHeader) != "evt_0123456789abcd" {
		t.Fatalf("headers: %v", h)
	}
	if len(other.bodies) != 0 {
		t.Fatal("the other webhook got a post")
	}
	// Rotating: the new key signs, the old one signs too until the overlap ends.
	admin := adminUser(t, s.DB)
	rotated := apitest.Must(s.UpdateAlertWebhookKey(ctx, a.ID, a.Revision, testSealer(t), store.AdminAction{Admin: admin, Reason: "Rotating the key on schedule"}))(t)
	if rotated.KeyID == idA || rotated.OverlapUntil == nil {
		t.Fatalf("rotated: %+v", rotated)
	}
	newKey, newID := keyOf(t, s, a.ID)
	d := queuedFor(a)
	if err := w.Post(ctx, notice, d); err != nil {
		t.Fatal(err)
	}
	h = rc.headers[1]
	want := "t=1793543400,v1=" + hmacOf(newKey, "1793543400", rc.bodies[1]) + ",v1=" + hmacOf(keyA, "1793543400", rc.bodies[1])
	if h.Get(SignatureHeader) != want || h.Get(KeyIDHeader) != newID+","+idA || newKey == keyA {
		t.Fatalf("overlap: %v want %s", h, want)
	}
	// A second rotation during the overlap is refused.
	if _, err := s.UpdateAlertWebhookKey(ctx, a.ID, rotated.Revision, testSealer(t), store.AdminAction{Admin: admin, Reason: "Rotating again too soon"}); err == nil {
		t.Fatal("rotated during the overlap")
	}
	// After the overlap only the new key signs, even before it is erased.
	w.Now = func() time.Time { return time.Now().Add(20 * time.Minute) }
	if err := w.Post(ctx, notice, d); err != nil {
		t.Fatal(err)
	}
	if h := rc.headers[2]; strings.Count(h.Get(SignatureHeader), "v1=") != 1 || h.Get(KeyIDHeader) != newID {
		t.Fatalf("after the overlap: %v", h)
	}
}

// A notice is never sent to a receiver it wasn't queued for: a deleted,
// disabled or re-pointed webhook skips (no error, so the delivery
// completes), while a renamed or re-scoped one still sends.
func TestWebhookSkipsWhenItsReceiverChanged(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()
	rc := &receiver{status: 204}
	srv := httptest.NewServer(rc)
	defer srv.Close()
	admin := adminUser(t, s.DB)
	act := func(why string) store.AdminAction {
		return store.AdminAction{Admin: admin, Reason: why + " for the test"}
	}
	w := hook(s, t, false, WebhookTimeout, nil)
	hooks := map[string]store.AlertWebhook{}
	for _, name := range []string{"renamed", "disabled", "repointed", "reformatted", "deleted"} {
		hooks[name] = addWebhook(t, s, name, srv.URL+"/"+name, FormatJSON, []string{"test.x"}, []string{"opened"})
	}
	rename, scope, off := "Renamed", []string{"test.y"}, false
	apitest.Must(s.UpdateAlertWebhook(ctx, hooks["renamed"].ID, store.AlertWebhookInput{Revision: 1, Name: &rename, Signals: &scope}, testSealer(t), act("Renaming")))(t)
	apitest.Must(s.UpdateAlertWebhook(ctx, hooks["disabled"].ID, store.AlertWebhookInput{Revision: 1, Enabled: &off}, testSealer(t), act("Disabling")))(t)
	url, slack := "https://elsewhere.example.net/hook", FormatSlack
	apitest.Must(s.UpdateAlertWebhook(ctx, hooks["repointed"].ID, store.AlertWebhookInput{Revision: 1, URL: &url}, testSealer(t), act("Moving the receiver")))(t)
	apitest.Must(s.UpdateAlertWebhook(ctx, hooks["reformatted"].ID, store.AlertWebhookInput{Revision: 1, Format: &slack}, testSealer(t), act("Switching to Slack")))(t)
	if err := s.DeleteAlertWebhook(ctx, hooks["deleted"].ID, act("Removing it")); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int{"renamed": 1, "disabled": 0, "repointed": 0, "reformatted": 0, "deleted": 0} {
		before := len(rc.bodies)
		if err := w.Post(ctx, notice, queuedFor(hooks[name])); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := len(rc.bodies) - before; got != want {
			t.Errorf("%s: %d posts, want %d", name, got, want)
		}
	}
}

// A failure is an error (so the delivery is retried) that never holds the URL
// or a key; a redirect isn't followed; with the guard on, a loopback receiver
// is refused before connecting; a receiver that never answers times out.
func TestWebhookFailures(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()
	rc := &receiver{status: 500}
	srv := httptest.NewServer(rc)
	defer srv.Close()
	a := addWebhook(t, s, "Failing", srv.URL+secretPath, FormatJSON, nil, nil)
	key, _ := keyOf(t, s, a.ID)
	w := hook(s, t, false, WebhookTimeout, nil)
	err := w.Post(ctx, notice, queuedFor(a))
	if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "500") {
		t.Fatalf("500: %v", err)
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		http.Redirect(rw, r, srv.URL+"/elsewhere", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	rc.status = 204
	r := addWebhook(t, s, "Redirecting", redirect.URL+secretPath, FormatJSON, nil, nil)
	if err := w.Post(ctx, notice, queuedFor(r)); err == nil || !strings.Contains(err.Error(), "307") || len(rc.bodies) != 1 {
		t.Fatalf("redirect followed, or not an error: %v (%d posts)", err, len(rc.bodies))
	}
	guarded := hook(s, t, true, WebhookTimeout, nil)
	err = guarded.Post(ctx, notice, queuedFor(a))
	if err == nil || !strings.Contains(err.Error(), ErrPrivateTarget.Error()) || strings.Contains(err.Error(), "SECRET") || len(rc.bodies) != 1 {
		t.Fatalf("guarded loopback: %v (%d posts)", err, len(rc.bodies))
	}
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer slow.Close()
	defer close(release)
	sl := addWebhook(t, s, "Slow", slow.URL+secretPath, FormatJSON, nil, nil)
	quick := hook(s, t, false, 50*time.Millisecond, nil)
	if err := quick.Post(ctx, notice, queuedFor(sl)); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("slow: %v", err)
	}
}

// Every enabled heartbeat is pinged, a disabled one isn't, one failing never
// stops the others, and an error never holds a URL.
func TestHeartbeatsArePingedFromTheDatabase(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()
	pings := map[string]int{}
	var failing int
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		pings[r.URL.Path]++
		if r.Method != http.MethodGet {
			t.Errorf("heartbeat used %s", r.Method)
		}
		if r.URL.Path == "/bad"+secretPath {
			failing++
			w.WriteHeader(500)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	addHeartbeat(t, s, "Uptime", srv.URL+"/one")
	addHeartbeat(t, s, "Backup", srv.URL+"/two")
	bad := addHeartbeat(t, s, "Broken", srv.URL+"/bad"+secretPath)
	off := addHeartbeat(t, s, "Paused", srv.URL+"/off")
	disabled := false
	admin := adminUser(t, s.DB)
	apitest.Must(s.UpdateAlertWebhook(ctx, off.ID, store.AlertWebhookInput{Revision: 1, Enabled: &disabled}, testSealer(t), store.AdminAction{Admin: admin, Reason: "Pausing the heartbeat"}))(t)
	h := &Heartbeat{Store: s, Sealer: testSealer(t), Client: NewClient(HeartbeatTimeout, false)}
	err := h.Ping(ctx)
	if err == nil || strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "1 of 3") {
		t.Fatalf("ping: %v", err)
	}
	if pings["/one"] != 1 || pings["/two"] != 1 || pings["/off"] != 0 || failing != 1 {
		t.Fatalf("pings: %v", pings)
	}
	// Deleting the broken one: all good.
	if err := s.DeleteAlertWebhook(ctx, bad.ID, store.AdminAction{Admin: admin, Reason: "Removing the broken one"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	// With the guard on, loopback heartbeats are refused without the URL.
	guarded := &Heartbeat{Store: s, Sealer: testSealer(t), Client: NewClient(HeartbeatTimeout, true)}
	if err := guarded.Ping(ctx); err == nil || strings.Contains(err.Error(), srv.URL) {
		t.Fatalf("guarded: %v", err)
	}
}
