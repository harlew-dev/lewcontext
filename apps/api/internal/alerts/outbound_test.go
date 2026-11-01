package alerts

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// What counts as a public address for the guard.
func TestPublicAddress(t *testing.T) {
	for _, a := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "0.0.0.0", "100.64.0.1",
		"192.0.0.8", "198.18.0.1", "240.0.0.1", "255.255.255.255", "224.0.0.1", "::1", "::", "fe80::1", "fd00::1", "ff02::1",
		"::ffff:10.0.0.1", "::ffff:127.0.0.1", "64:ff9b::a00:1", "2002:a00:1::1",
		"::127.0.0.1", "::10.0.0.1", "::ffff:0:127.0.0.1", "fec0::1", "2001::1", "192.88.99.1", "192.0.2.5", "198.51.100.7", "203.0.113.9", "2001:db8::1"} {
		if PublicAddress(netip.MustParseAddr(a)) {
			t.Errorf("%s counted as public", a)
		}
	}
	for _, a := range []string{"8.8.8.8", "203.0.114.1", "2001:4860:4860::8888", "2606:4700::1111", "::ffff:8.8.8.8"} {
		if !PublicAddress(netip.MustParseAddr(a)) {
			t.Errorf("%s counted as private", a)
		}
	}
}

// receiver is a webhook receiver that keeps what it's sent and answers
// status.
type receiver struct {
	status  int
	bodies  [][]byte
	headers []http.Header
}

func (rc *receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	rc.bodies = append(rc.bodies, body)
	rc.headers = append(rc.headers, r.Header.Clone())
	w.WriteHeader(rc.status)
	_, _ = w.Write([]byte(strings.Repeat("x", 64<<10)))
}

// secretPath is part of a webhook URL that is its secret, as a Slack
// webhook's is: it must never appear in an error.
const secretPath = "/services/T0SECRET/B0SECRET/xoxSECRETtoken"

var notice = Notice{Alert: "alt_0123456789abcd", Event: "alert.opened", State: "open", Signal: "auth.failed_sign_ins",
	Title: "Failed sign-ins", Level: "critical", Value: 1204, Unit: "count", OpenedAt: time.Date(2026, 11, 1, 14, 30, 0, 0, time.UTC),
	Meaning: "Someone is guessing."}

// The JSON body, signed with the secret: the receiver can check the
// signature over "<t>.<body>" and its time.
func TestWebhookPostsSignedJSON(t *testing.T) {
	rc := &receiver{status: 204}
	srv := httptest.NewServer(rc)
	defer srv.Close()
	at := time.Unix(1793543400, 0)
	w := Webhook{URL: srv.URL + secretPath, Format: FormatJSON, Secret: []byte("a-secret-of-32-characters-or-more"),
		Client: NewClient(WebhookTimeout, false), Now: func() time.Time { return at }}
	if err := w.Post(context.Background(), notice); err != nil {
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
	mac := hmac.New(sha256.New, []byte("a-secret-of-32-characters-or-more"))
	mac.Write([]byte("1793543400."))
	mac.Write(rc.bodies[0])
	want := "t=1793543400,v1=" + hex.EncodeToString(mac.Sum(nil))
	if h := rc.headers[0]; h.Get(SignatureHeader) != want || h.Get("Content-Type") != "application/json" {
		t.Fatalf("headers: %v, want %s", h, want)
	}
	// Without a secret, no signature.
	w.Secret = nil
	if err := w.Post(context.Background(), notice); err != nil || rc.headers[1].Get(SignatureHeader) != "" {
		t.Fatalf("unsigned: %v %v", err, rc.headers[1])
	}
}

// Slack's shape: one text line, its markup characters escaped.
func TestWebhookSlackShape(t *testing.T) {
	w := Webhook{Format: FormatSlack}
	n := notice
	n.Meaning = "A <b>bold</b> & claim."
	body, err := w.Body(n)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(body, &got); err != nil || len(got) != 1 {
		t.Fatalf("%s: %v", body, err)
	}
	text := got["text"]
	if !strings.HasPrefix(text, "*Critical alert*: Failed sign-ins (1,204), opened 1 Nov 2026, 14:30 UTC.") ||
		!strings.Contains(text, "A &lt;b&gt;bold&lt;/b&gt; &amp; claim.") || strings.Contains(text, "<b>") {
		t.Fatalf("text: %s", text)
	}
	n.Event, n.State = "alert.resolved", "resolved"
	body, _ = w.Body(n)
	if !strings.Contains(string(body), "*Resolved*: Failed sign-ins, opened") || strings.Contains(string(body), "1,204") {
		t.Fatalf("resolved: %s", body)
	}
}

// A failure is an error (so the delivery is retried) that never holds the
// URL; a redirect isn't followed; at most a little of an answer is read;
// with the guard on, a loopback receiver is refused before connecting.
func TestWebhookFailures(t *testing.T) {
	rc := &receiver{status: 500}
	srv := httptest.NewServer(rc)
	defer srv.Close()
	w := Webhook{URL: srv.URL + secretPath, Format: FormatJSON, Client: NewClient(WebhookTimeout, false)}
	err := w.Post(context.Background(), notice)
	if err == nil || strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "500") {
		t.Fatalf("500: %v", err)
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		http.Redirect(rw, r, srv.URL+"/elsewhere", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	rc.status = 204
	w.URL = redirect.URL + secretPath
	if err := w.Post(context.Background(), notice); err == nil || !strings.Contains(err.Error(), "307") || len(rc.bodies) != 1 {
		t.Fatalf("redirect followed, or not an error: %v (%d posts)", err, len(rc.bodies))
	}
	guarded := Webhook{URL: srv.URL + secretPath, Format: FormatJSON, Client: NewClient(WebhookTimeout, true)}
	err = guarded.Post(context.Background(), notice)
	if !errors.Is(err, ErrPrivateTarget) || strings.Contains(err.Error(), "SECRET") || len(rc.bodies) != 1 {
		t.Fatalf("guarded loopback: %v (%d posts)", err, len(rc.bodies))
	}
	h := Heartbeat{URL: srv.URL + secretPath, Client: NewClient(HeartbeatTimeout, true)}
	if err := h.Ping(context.Background()); !errors.Is(err, ErrPrivateTarget) {
		t.Fatalf("guarded heartbeat: %v", err)
	}
	// A receiver that never answers: a timeout, without the URL.
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer slow.Close()
	defer close(release)
	w = Webhook{URL: slow.URL + secretPath, Format: FormatJSON, Client: NewClient(50*time.Millisecond, false)}
	if err := w.Post(context.Background(), notice); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("slow: %v", err)
	}
}

func TestFormatValue(t *testing.T) {
	for _, c := range []struct {
		value      float64
		unit, want string
	}{
		{1204, "count", "1,204"}, {1234567, "count", "1,234,567"}, {5.234, "percent", "5.2%"}, {1450.4, "ms", "1,450 ms"},
		{1, "minutes", "1 minute"}, {12.4, "minutes", "12 minutes"}, {34, "per_minute", "34 a minute"}, {0, "count", "0"},
	} {
		if got := FormatValue(c.value, c.unit); got != c.want {
			t.Errorf("%v %s: %q, want %q", c.value, c.unit, got, c.want)
		}
	}
	if got := FormatDuration(2*time.Hour + 5*time.Minute + 30*time.Second); got != "2 hours 5 minutes" {
		t.Errorf("duration: %q", got)
	}
	if got := FormatDuration(20 * time.Second); got != "under a minute" {
		t.Errorf("duration: %q", got)
	}
}
