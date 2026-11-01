package seed

import (
	"context"
	"fmt"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/factory"
	"strings"
	"time"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
)

// The events seeder fills the audit trail with fake history, to check the
// admin Events page's list and charts at volume (EXPLAIN, docs/testing.md).
// Most events fall in the last 7 days, a thin tail goes back 90; sign-ins
// dominate, and a few addresses try passwords in bursts. Payloads look like
// the store's (public IDs, never internal ones) but name nobody real: the
// accounts and workspaces they name don't exist.

// Event counts: the default, and the most one run inserts.
const (
	defaultEvents = 10_000
	maxEvents     = 2_000_000
)

// eventsChunk is how many events are built and inserted at a time.
const eventsChunk = 10_000

// eventWeights make some names more common than others (per thousand);
// the rest weigh 2.
var eventWeights = map[string]int{
	"auth.login_attempted":          300,
	"auth.logged_in":                150,
	"auth.logged_out":               60,
	"auth.mfa.checked":              60,
	"auth.password_checked":         20,
	"auth.registration.requested":   25,
	"auth.registration.sent":        25,
	"auth.registration.checked":     25,
	"auth.registered":               20,
	"auth.password_reset.requested": 15,
	"auth.password_reset.sent":      10,
	"project.created":               40,
	"project.updated":               60,
	"project.deleted":               10,
	"workspace.created":             15,
	"workspace.invitation.created":  15,
	"workspace.invitation.sent":     15,
	"workspace.invitation.accepted": 10,
	"rate_limit.reached":            15,
	"admin.event.listed":            30,
	"admin.user.listed":             20,
	"admin.user.viewed":             15,
}

// Share of events in the attackers' bursts, and in the tail older than a
// week, in percent.
const (
	burstPercent = 5
	tailPercent  = 5
)

// seedEvents inserts fake events.
func seedEvents(ctx context.Context, tx bun.Tx, f *gofakeit.Faker, o Options) (string, error) {
	count := orDefault(o.Count, defaultEvents)
	now := time.Now()
	pick := weightedNames()
	attackers := []string{f.IPv4Address(), f.IPv4Address(), f.IPv4Address()}
	bursts := []time.Time{
		now.Add(-time.Duration(f.IntRange(2, 160)) * time.Hour),
		now.Add(-time.Duration(f.IntRange(2, 160)) * time.Hour),
		now.Add(-time.Duration(f.IntRange(1, 20)) * time.Hour),
	}
	fake := func(i int, e *factory.Event) {
		if f.IntN(100) < burstPercent {
			burst := bursts[i%len(bursts)]
			e.Name, e.Payload = "auth.login_attempted", loginPayload(f, f.IntN(4) > 0)
			e.CreatedAt = burst.Add(time.Duration(f.IntN(3600)) * time.Second)
			e.IP = attackers[f.IntN(len(attackers))]
			return
		}
		e.Name = pick[f.IntN(len(pick))]
		e.Payload = fakePayload(f, e.Name)
		e.CreatedAt = fakeTime(f, now)
		e.IP = f.IPv4Address()
		if strings.HasSuffix(e.Name, ".sent") {
			e.IP, e.UserAgent = "", "" // recorded by a listener, outside a request
		}
	}
	for done := 0; done < count; done += eventsChunk {
		n := min(eventsChunk, count-done)
		offset := done
		if _, err := factory.Events.Sequence(func(i int, e *factory.Event) { fake(offset+i, e) }).Create(ctx, tx, f, n); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("Added %d fake events over the last 90 days, most in the last 7, with bursts of failed sign-ins from %s", count, strings.Join(attackers, ", ")), nil
}

// weightedNames lists each catalogue name as often as its weight.
func weightedNames() []string {
	var names []string
	for _, name := range events.Names() {
		weight, ok := eventWeights[name]
		if !ok {
			weight = 2
		}
		for range weight {
			names = append(names, name)
		}
	}
	return names
}

// fakeTime is a moment in the last 7 days, or now and then in the 83
// before them.
func fakeTime(f *gofakeit.Faker, now time.Time) time.Time {
	week := now.AddDate(0, 0, -7)
	if f.IntN(100) < tailPercent {
		return f.DateRange(now.AddDate(0, 0, -90), week)
	}
	return f.DateRange(week, now)
}

// fakeID is a public ID that names nothing.
func fakeID(f *gofakeit.Faker, prefix string) string {
	const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	id := make([]byte, 14)
	for i := range id {
		id[i] = base62[f.IntN(len(base62))]
	}
	return prefix + "_" + string(id)
}

// loginPayload is a sign-in attempt; failed ones mostly guess passwords.
func loginPayload(f *gofakeit.Faker, failed bool) map[string]any {
	result := events.LoginSucceeded
	if failed {
		result = events.LoginWrongPassword
		if f.IntN(3) == 0 {
			result = events.LoginUnknownAccount
		}
	} else if f.IntN(6) == 0 {
		result = "mfa_required"
	}
	payload := map[string]any{"email": fakeEmail(f), "result": result}
	if result != events.LoginUnknownAccount {
		payload["user"] = fakeID(f, "usr")
	}
	return payload
}

func fakeEmail(f *gofakeit.Faker) string {
	return strings.ToLower(f.Username()) + "@example.test"
}

// fakePayload is a plausible payload for name.
func fakePayload(f *gofakeit.Faker, name string) map[string]any {
	user, workspace := fakeID(f, "usr"), fakeID(f, "wsp")
	ok := f.IntN(10) > 0
	switch name {
	case "auth.login_attempted":
		return loginPayload(f, f.IntN(10) < 3)
	case "auth.mfa.checked":
		return map[string]any{"user": user, "ok": ok, "method": "totp", "step": "login"}
	case "auth.password_checked":
		return map[string]any{"user": user, "ok": ok, "step": "verify"}
	case "auth.registration.requested", "auth.password_reset.requested":
		return map[string]any{"email": fakeEmail(f)}
	case "auth.registration.checked":
		return map[string]any{"email": fakeEmail(f), "ok": ok, "attempt": f.IntRange(1, 3)}
	case "auth.registration.sent", "auth.password_reset.sent":
		return map[string]any{"email": fakeEmail(f), "user": user, "request": fakeID(f, "evt")}
	case "auth.registered":
		return map[string]any{"user": user, "name": f.Name(), "email": fakeEmail(f), "verifiedBy": "code"}
	case "admin.login_attempted":
		return map[string]any{"email": fakeEmail(f), "result": []string{"mfa_required", "wrong_password", "not_admin"}[f.IntN(3)]}
	case "admin.access_refused":
		return map[string]any{"reason": events.AdminRefusedIP}
	case "rate_limit.reached":
		return map[string]any{"limit": []string{"auth:per-ip:block", "auth:per-account:block", "register:per-ip:block"}[f.IntN(3)], "window": 900}
	case "email.blocked", "email.unblocked":
		return map[string]any{"email": fakeEmail(f)}
	}
	switch {
	case strings.HasPrefix(name, "project."):
		return map[string]any{"project": fakeID(f, "prj"), "workspace": workspace, "name": f.ProductName()}
	case strings.HasPrefix(name, "workspace.invitation."):
		return map[string]any{"invitation": fakeID(f, "inv"), "workspace": workspace, "email": fakeEmail(f), "role": "member"}
	case strings.HasPrefix(name, "workspace."):
		return map[string]any{"workspace": workspace, "user": user, "name": f.Company()}
	case events.AdminRead(name):
		return map[string]any{"admin": user}
	}
	return map[string]any{"user": user}
}
