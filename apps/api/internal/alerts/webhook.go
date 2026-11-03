package alerts

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Webhook formats, chosen per webhook.
const (
	FormatJSON  = "json"
	FormatSlack = "slack"
)

// WebhookTimeout bounds a webhook post (the plan's 10 s).
const WebhookTimeout = 10 * time.Second

// SignatureHeader carries the body's signature:
// t=<unix seconds>,v1=<hex HMAC-SHA256 of "<t>.<body>">, so the receiver
// can check it came from us and isn't an old body played again (it
// compares t with its clock). For 15 minutes after a key rotation there is
// a second v1= made with the previous key, over the same time and body.
const SignatureHeader = "X-Lewcontext-Signature"

// KeyIDHeader names the key (or keys, comma separated, in the order of the
// v1= signatures) that signed the body: public, non-secret labels (key_…), so
// a receiver holding several keys knows which one to verify with.
const KeyIDHeader = "X-Lewcontext-Key-Id"

// DeliveryHeader carries the ID of the event being posted (evt_…), the same
// on every retry of it, so a receiver can drop a body it has seen.
const DeliveryHeader = "X-Lewcontext-Delivery"

// Webhook posts alert events to the webhooks an operator manages in the
// dashboard. It holds only what it needs to open one: which webhook, and its
// URL, format and keys, are read for each delivery, just before the send.
type Webhook struct {
	Store  *store.Store
	Sealer *auth.DestinationSealer
	// Production and AllowPrivate are the URL rules' configuration.
	Production, AllowPrivate bool
	Client                   *http.Client
	// Now is the clock for the signature's time: time.Now when nil.
	Now func() time.Time
}

// Notice is an alert event as it leaves the system: by the webhook (and,
// in words, by email). Fixed text and numbers only (decision 6): the
// signal's name and title, its level, value and unit, its times and what
// it usually means. Never the admin key, a link carrying it, an address,
// a name or a sample ID.
type Notice struct {
	Snapshot   *events.AlertSnapshot `json:"-"`
	Alert      string                `json:"alert"`
	Event      string                `json:"event"`
	State      string                `json:"state"`
	Signal     string                `json:"signal"`
	Title      string                `json:"title"`
	Level      string                `json:"level"`
	Value      float64               `json:"value"`
	Unit       string                `json:"unit"`
	OpenedAt   time.Time             `json:"openedAt"`
	ResolvedAt *time.Time            `json:"resolvedAt"`
	Meaning    string                `json:"meaning"`
}

// Body is the request body n is posted with, in format.
func Body(n Notice, format string) ([]byte, error) {
	if n.Snapshot != nil {
		body := n.Snapshot.JSONBody
		if format == FormatSlack {
			body = n.Snapshot.SlackBody
		}
		if body != "" {
			return []byte(body), nil
		}
	}
	if format == FormatSlack {
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

// Post sends n to the webhook delivery was queued for. It is skipped (no
// error, so the delivery completes) when that webhook was deleted, disabled,
// or had its URL, format or key replaced since: a notice is never sent to a
// receiver it wasn't queued for. A failure (the receiver down, a non-2xx
// answer) is an error, so the delivery is retried like an email's; an error
// never holds the URL or a key.
func (w Webhook) Post(ctx context.Context, n Notice, d events.Delivered) error {
	dest, active, err := w.Store.FindAlertWebhookTarget(ctx, d.Target, d.TargetRevision, w.Sealer)
	if err != nil {
		return errors.New("alert webhook could not be opened")
	}
	if !active {
		return nil
	}
	if err := config.ValidateAlertURL(dest.URL, w.Production, w.AllowPrivate); err != nil {
		return errors.New("alert webhook failed validation")
	}
	body, err := Body(n, dest.Format)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest.URL, bytes.NewReader(body))
	if err != nil {
		return withoutURL(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "lewcontext-alerts")
	req.Header.Set(DeliveryHeader, d.EventID)
	now := time.Now
	if w.Now != nil {
		now = w.Now
	}
	signature, keyIDs := SignWithKeys(dest, now(), body)
	req.Header.Set(SignatureHeader, signature)
	req.Header.Set(KeyIDHeader, keyIDs)
	return send(w.Client, req)
}

// SignWithKeys is the signature and key ID headers' values for body at t:
// one v1= with the current key and, during a rotation's overlap, a second
// with the previous key over the same time and body. An expired previous key
// is never used, even before it is erased.
func SignWithKeys(d store.AlertDestination, t time.Time, body []byte) (signature, keyIDs string) {
	signature, keyIDs = Sign(d.Key, t, body), d.KeyID
	if len(d.OverlapKey) > 0 && d.OverlapUntil != nil && t.Before(*d.OverlapUntil) {
		old := Sign(d.OverlapKey, t, body)
		signature += old[strings.Index(old, ","):]
		keyIDs += "," + d.OverlapKeyID
	}
	return signature, keyIDs
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

// Heartbeat pings every enabled heartbeat an operator set up, a dead man's
// switch: the leader GETs each after an evaluation that worked, and the
// service behind it (Healthchecks.io, Better Stack, Cronitor) alerts when the
// pings stop: every server down, the database unreachable, or the loop wedged.
// The URLs are read from the database for each ping, so a change takes effect
// on the next turn.
type Heartbeat struct {
	Store                    *store.Store
	Sealer                   *auth.DestinationSealer
	Production, AllowPrivate bool
	Client                   *http.Client
}

// Ping GETs each heartbeat's URL. One failing never stops the others; the
// error says how many failed and never holds a URL.
func (h *Heartbeat) Ping(ctx context.Context) error {
	beats, err := h.Store.IndexAlertHeartbeats(ctx, h.Sealer)
	if err != nil {
		return errors.New("alert heartbeats could not be opened")
	}
	failed := 0
	for _, b := range beats {
		if config.ValidateAlertURL(b.URL, h.Production, h.AllowPrivate) != nil {
			failed++
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.URL, nil)
		if err != nil {
			failed++
			continue
		}
		req.Header.Set("User-Agent", "lewcontext-alerts")
		if send(h.Client, req) != nil {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d heartbeats failed", failed, len(beats))
	}
	return nil
}
