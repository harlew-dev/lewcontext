package alerts

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Webhook formats (ALERT_WEBHOOK_FORMAT).
const (
	FormatJSON  = "json"
	FormatSlack = "slack"
)

// WebhookTimeout bounds a webhook post (the plan's 10 s).
const WebhookTimeout = 10 * time.Second

// SignatureHeader carries the body's signature when a secret is set:
// t=<unix seconds>,v1=<hex HMAC-SHA256 of "<t>.<body>">, so the receiver
// can check it came from us and isn't an old body played again (it
// compares t with its clock).
const SignatureHeader = "X-Lewcontext-Signature"

// DeliveryHeader carries the ID of the event being posted (evt_…), the same
// on every retry of it, so a receiver can drop a body it has seen.
const DeliveryHeader = "X-Lewcontext-Delivery"

// Webhook posts alert events to ALERT_WEBHOOK_URL.
type Webhook struct {
	URL    string
	Format string
	// Secret signs each body when set (ALERT_WEBHOOK_SECRET).
	Secret []byte
	Client *http.Client
	// Now is the clock for the signature's time: time.Now when nil.
	Now func() time.Time
}

// Notice is an alert event as it leaves the system: by the webhook (and,
// in words, by email). Fixed text and numbers only (decision 6): the
// signal's name and title, its level, value and unit, its times and what
// it usually means. Never the admin key, a link carrying it, an address,
// a name or a sample ID.
type Notice struct {
	Alert      string     `json:"alert"`
	Event      string     `json:"event"`
	State      string     `json:"state"`
	Signal     string     `json:"signal"`
	Title      string     `json:"title"`
	Level      string     `json:"level"`
	Value      float64    `json:"value"`
	Unit       string     `json:"unit"`
	OpenedAt   time.Time  `json:"openedAt"`
	ResolvedAt *time.Time `json:"resolvedAt"`
	Meaning    string     `json:"meaning"`
}

// Body is the request body n is posted with, in w's format.
func (w Webhook) Body(n Notice) ([]byte, error) {
	if w.Format == FormatSlack {
		return json.Marshal(map[string]string{"text": slackText(n)})
	}
	return json.Marshal(n)
}

// slackText is n as a line for Slack's incoming webhooks (and the tools
// taking their shape), its words escaped as Slack asks.
func slackText(n Notice) string {
	what := map[string]string{
		"alert.opened":    LevelWord(n.Level) + " alert",
		"alert.escalated": "Now critical",
		"alert.reminded":  "Still open",
		"alert.resolved":  "Resolved",
	}[n.Event]
	line := "*" + slackEscape(what) + "*: " + slackEscape(n.Title)
	if n.Event != "alert.resolved" {
		line += " (" + slackEscape(FormatValue(n.Value, n.Unit)) + ")"
	}
	line += ", opened " + FormatTime(n.OpenedAt) + ". " + slackEscape(n.Meaning)
	if n.Event != "alert.resolved" {
		line += " Open the admin dashboard for the details."
	}
	return line
}

// slackEscape escapes the three characters Slack's text treats as markup.
func slackEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// Post sends n, identified by delivery (an event's ID). A failure (the receiver down, a non-2xx answer) is an
// error, so the delivery is retried like an email's.
func (w Webhook) Post(ctx context.Context, n Notice, delivery string) error {
	body, err := w.Body(n)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return withoutURL(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "lewcontext-alerts")
	if delivery != "" {
		req.Header.Set(DeliveryHeader, delivery)
	}
	if len(w.Secret) > 0 {
		now := time.Now
		if w.Now != nil {
			now = w.Now
		}
		req.Header.Set(SignatureHeader, Sign(w.Secret, now(), body))
	}
	return send(w.Client, req)
}

// Sign is the signature header's value for body at t.
func Sign(secret []byte, t time.Time, body []byte) string {
	stamp := strconv.FormatInt(t.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(stamp + "."))
	mac.Write(body)
	return "t=" + stamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

// HeartbeatTimeout bounds a heartbeat ping.
const HeartbeatTimeout = 5 * time.Second

// Heartbeat is ALERT_HEARTBEAT_URL, a dead man's switch (decision 7): the
// leader GETs it after each evaluation that worked, and the service behind
// it (Healthchecks.io, Better Stack, Cronitor) alerts when the pings stop:
// every server down, the database unreachable, or the loop wedged.
type Heartbeat struct {
	URL    string
	Client *http.Client
}

// Ping GETs the URL; its error never holds it.
func (h *Heartbeat) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL, nil)
	if err != nil {
		return withoutURL(err)
	}
	req.Header.Set("User-Agent", "lewcontext-alerts")
	return send(h.Client, req)
}
