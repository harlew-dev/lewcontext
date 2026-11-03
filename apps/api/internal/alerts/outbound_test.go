package alerts

import (
	"encoding/json"
	"io"
	"net/http"
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

// Slack's shape: one text line, its markup characters escaped.
func TestWebhookSlackShape(t *testing.T) {
	n := notice
	n.Meaning = "A <b>bold</b> & claim."
	body, err := Body(n, FormatSlack)
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
	body, _ = Body(n, FormatSlack)
	if !strings.Contains(string(body), "*Resolved*: Failed sign-ins, opened") || strings.Contains(string(body), "1,204") {
		t.Fatalf("resolved: %s", body)
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
