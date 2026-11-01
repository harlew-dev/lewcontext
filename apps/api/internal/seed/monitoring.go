package seed

import (
	"context"
	"fmt"
	"lewcontext/api/internal/factory"
	"time"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
)

// The monitoring seeder plants the attacks the admin Monitoring pages look
// for (docs/api/monitoring.md), so every signal there has something to
// show in development: ordinary sign-ins over the last day, then in the
// last hour credential stuffing from one address (one of its guesses
// working), a password guessed from many addresses, an account signed in
// after a run of failures, failed codes, a password reset flood, a shared
// limit reached, failures and a refusal at the admin door, and an account
// taken over; then part 2's patterns (monitoring_abuse.go). Addresses are
// from the documentation ranges (192.0.2.0/24,
// 198.51.100.0/24, 203.0.113.0/24), every email is @example.test, and every
// payload holds "seeded": true. It never touches rate_limits: a counter
// there would really refuse requests. Insert-only; refused in production.

// The seeded attackers' addresses, documented so a demo can find them.
const (
	SeedStuffingIP = "203.0.113.66"
	seedResetIP    = "198.51.100.23"
	seedDoorIP     = "198.51.100.200"
	seedTakeoverIP = "203.0.113.99"
)

// seedMonitoring inserts the patterns. Count is ignored.
func seedMonitoring(ctx context.Context, tx bun.Tx, f *gofakeit.Faker, o Options) (string, error) {
	now := time.Now()
	var planted []factory.Event
	add := func(name string, ago time.Duration, ip string, payload map[string]any, scope ...func(*factory.Event)) {
		payload["seeded"] = true
		e := factory.Event{Name: name, Payload: payload, IP: ip, CreatedAt: now.Add(-ago)}
		for _, s := range scope {
			s(&e)
		}
		planted = append(planted, e)
	}
	minutes := func(n int) time.Duration { return time.Duration(n) * time.Minute }
	docIP := func() string { return fmt.Sprintf("192.0.2.%d", f.IntRange(1, 254)) }

	// A day of ordinary sign-ins, a few of them typos.
	for range 300 {
		result := "succeeded"
		if f.IntN(10) == 0 {
			result = "wrong_password"
		}
		add("auth.login_attempted", time.Duration(f.IntRange(1, 24*60))*time.Minute, docIP(),
			map[string]any{"email": fakeEmail(f), "user": fakeID(f, "usr"), "result": result})
	}
	for range 120 {
		add("auth.logged_in", time.Duration(f.IntRange(1, 24*60))*time.Minute, docIP(), map[string]any{"user": fakeID(f, "usr")})
	}
	// Credential stuffing: one address replays a list of 40 emails, and
	// one of them works.
	for i := range 40 {
		result := "unknown_account"
		if i%3 == 0 {
			result = "wrong_password"
		}
		add("auth.login_attempted", minutes(10+i/2), SeedStuffingIP, map[string]any{"email": fmt.Sprintf("list%02d@example.test", i), "result": result})
	}
	add("auth.login_attempted", minutes(9), SeedStuffingIP, map[string]any{"email": "list07@example.test", "user": fakeID(f, "usr"), "result": "succeeded"})
	add("rate_limit.reached", minutes(12), SeedStuffingIP, map[string]any{"limit": "auth:per-ip:block", "window": 900})
	// One password guessed from six addresses.
	for i := range 6 {
		add("auth.login_attempted", minutes(20+i), fmt.Sprintf("198.51.100.%d", 40+i), map[string]any{"email": "dana@example.test", "user": fakeID(f, "usr"), "result": "wrong_password"})
	}
	// Six wrong passwords, then the right one: the attack that worked.
	guessed := fakeID(f, "usr")
	for i := range 6 {
		add("auth.login_attempted", minutes(35-i), "192.0.2.150", map[string]any{"email": "sam@example.test", "user": guessed, "result": "wrong_password"})
	}
	add("auth.login_attempted", minutes(28), "192.0.2.150", map[string]any{"email": "sam@example.test", "user": guessed, "result": "mfa_required"})
	// Codes failing for that account.
	for i := range 4 {
		add("auth.mfa.checked", minutes(27-i), "192.0.2.150", map[string]any{"user": guessed, "ok": false, "method": "totp", "step": "login"})
	}
	// A reset flood from one address, and its limit reached.
	for i := range 30 {
		add("auth.password_reset.requested", minutes(5+i*4), seedResetIP, map[string]any{"email": fmt.Sprintf("reset%02d@example.test", i%12)})
	}
	for i := range 8 {
		add("auth.password_reset.sent", minutes(4+i*14), "", map[string]any{"email": fmt.Sprintf("reset%02d@example.test", i), "user": fakeID(f, "usr"), "request": fakeID(f, "evt")})
	}
	add("rate_limit.reached", minutes(50), seedResetIP, map[string]any{"limit": "password-reset:per-ip:block", "window": 3600})
	// Sign-ups shut off for everyone for a while.
	add("rate_limit.reached", minutes(30), docIP(), map[string]any{"limit": "register:global:block", "window": 3600})
	// The admin door: wrong passwords, then a real password that isn't an
	// admin's.
	for i := range 3 {
		add("admin.login_attempted", time.Duration(2+i)*time.Hour, seedDoorIP, map[string]any{"email": "ops@example.test", "result": "wrong_password"})
	}
	add("admin.login_attempted", 90*time.Minute, seedDoorIP, map[string]any{"email": "dana@example.test", "user": fakeID(f, "usr"), "result": "not_admin"})

	// An account taken over: it signs in from home, then from a network it
	// never used, and changes its password within the hour.
	victim, err := factory.Users.State(func(u *factory.User) {
		u.Name, u.PasswordHash = "Taylor Reed", "unusable:"+f.LetterN(32)
	}).CreateOne(ctx, tx, f)
	if err != nil {
		return "", err
	}
	ofVictim := func(e *factory.Event) { e.UserID, e.ActorID = victim.ID, victim.ID }
	add("auth.logged_in", 10*24*time.Hour, "198.51.100.140", map[string]any{"user": victim.PublicID}, ofVictim)
	add("auth.logged_in", minutes(40), seedTakeoverIP, map[string]any{"user": victim.PublicID}, ofVictim)
	add("auth.password_changed", minutes(25), seedTakeoverIP, map[string]any{"user": victim.PublicID, "otherSessionsRevoked": 2}, ofVictim)

	// Part 2: the IP addresses, Accounts, Workspaces and Usage pages.
	abused, err := seedAbuse(abuse{ctx: ctx, tx: tx, f: f, now: now, add: add})
	if err != nil {
		return "", err
	}

	if _, err := factory.Events.Sequence(func(i int, e *factory.Event) { *e = planted[i] }).Create(ctx, tx, f, len(planted)); err != nil {
		return "", err
	}
	return fmt.Sprintf("Planted %d fake events: credential stuffing from %s, a guessed password, a reset flood from %s, register:global reached, admin door failures from %s, %s taken over from %s; %s",
		len(planted), SeedStuffingIP, seedResetIP, seedDoorIP, victim.Email, seedTakeoverIP, abused), nil
}
