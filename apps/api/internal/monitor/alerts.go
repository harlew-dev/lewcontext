package monitor

import (
	"slices"
	"time"
)

// Alert rules (docs/api/monitoring.md#alerts, plan A5's decision 2): the
// signals that alert, each with how long it must stay at its level before
// an alert opens. An alert takes its signal's level, so a page and its
// alert always agree; a rule may only narrow it (From, Cap). Rules change,
// like thresholds, by a reviewed commit, and the doc's table with them.
// internal/alerts evaluates them.

// AlertRule makes a signal alert.
type AlertRule struct {
	// Signal is the signal's name (Signals()).
	Signal string
	// For is how long the signal must stay at warn or critical before an
	// alert opens: minutes for rates, none for a discrete fact (a refusal
	// at the admin door). An hourly rule's checks are an hour apart, so it
	// has none.
	For time.Duration
	// Hourly rules are read once an hour (the integrity checks); the
	// others every minute.
	Hourly bool
	// From is the lowest level that alerts (LevelWarn when empty), and Cap
	// the highest an alert takes (LevelCritical when empty): a sign-up
	// cluster alerts only at the signal's critical, as a warning.
	From, Cap Level
	// Title names the alert when the signal's label only reads on its own
	// page ("…of them signing someone in"); empty: the label.
	Title string
	// Meaning is a sentence of what it usually means, for the email and
	// the webhook: fixed text, never data.
	Meaning string
}

// Level is the alert level of a signal at level: LevelOK below From,
// never above Cap.
func (r AlertRule) Level(level Level) Level {
	from, top := r.From, r.Cap
	if from == "" {
		from = LevelWarn
	}
	if top == "" {
		top = LevelCritical
	}
	if level == LevelOK || (from == LevelCritical && level != LevelCritical) {
		return LevelOK
	}
	if top == LevelWarn {
		return LevelWarn
	}
	return level
}

// Minute rules' For durations, from the plan's table.
const (
	errorRateFor = 5 * time.Minute
	latencyFor   = 10 * time.Minute
	sustainedFor = 5 * time.Minute
)

// AlertRules are the rules, in the plan's order. Anyone with an account can
// raise some of these signals on purpose (their own wrong passwords, ten
// junk emails from one address); the alerts' bounds (one open alert per
// signal, the hour's cooldown, digests, reminders only for critical and
// unacknowledged) keep that to a few emails an hour (internal/alerts).
var AlertRules = []AlertRule{
	{Signal: "http.server_error_share", For: errorRateFor,
		Meaning: "Requests are failing with server errors: a release, the database or something the app depends on is failing."},
	{Signal: "http.latency_p95", For: latencyFor,
		Meaning: "Requests are slow for many people: the database, the hashing slots or a slow route."},
	{Signal: "integrity.overdue_deliveries",
		Meaning: "A delivery is 10 minutes overdue: the event worker has stalled or every server is down, so emails aren't going out."},
	{Signal: "mail.failed_deliveries",
		Meaning: "Emails failed after every retry: check the SMTP provider and the Mail page."},
	{Signal: "limits.shared_reached",
		Meaning: "A limit shared by everyone was reached: that feature is refused for everyone until its window ends."},
	{Signal: "auth.stuffing_addresses",
		Meaning: "An address tried the passwords of 10 or more accounts in an hour: credential stuffing."},
	{Signal: "auth.stuffing_successes", Title: "Credential stuffing that got a password right",
		Meaning: "An address trying many accounts' passwords got one right: an account may be taken over."},
	{Signal: "auth.success_after_failures",
		Meaning: "An account signed in after 5 or more wrong passwords in an hour: a guess may have worked."},
	{Signal: "auth.possible_takeovers",
		Meaning: "An account signed in from a new network and then changed its password, two-factor or sessions."},
	{Signal: "admin.door_refusals",
		Meaning: "Someone with the admin key was refused at the admin door: the key, and maybe a password, may have leaked."},
	{Signal: "admin.sessions_moved",
		Meaning: "An admin session was used from another network than it signed in from."},
	{Signal: "integrity.servers_out_of_step",
		Meaning: "Servers run different versions, or code ahead of the database, 30 minutes after a start: a release stopped halfway."},
	{Signal: "errors.new_panics", Cap: LevelWarn,
		Meaning: "The server panicked in a way it hadn't before."},
	{Signal: "errors.new_frequent",
		Meaning: "A new server error happened 10 or more times."},
	{Signal: "db.pool_wait_minutes",
		Meaning: "Requests waited for a database connection in most of the last minutes: the pool is the bottleneck."},
	{Signal: "auth.password_hash_busy", For: sustainedFor,
		Meaning: "Sign-ins were refused with every password-hashing slot busy: heavy load or an attack on sign-in."},
	{Signal: "ips.sign_up_cluster", From: LevelCritical, Cap: LevelWarn,
		Meaning: "One address made 20 or more accounts in a day: the usual shape of a spam farm."},
	{Signal: "integrity.ownerless_workspaces", Hourly: true,
		Meaning: "A workspace has no owner: a membership change or a deletion went wrong."},
	{Signal: "integrity.workspaces_over_quota", Hourly: true,
		Meaning: "A workspace holds more than a quota allows: the quota's lock was bypassed, or a quota was lowered."},
	{Signal: "integrity.admins_without_mfa", Hourly: true,
		Meaning: "An admin has no two-factor authentication outside the setup window: something turned it off."},
}

// FindSignal finds a signal by name; false for any other.
func FindSignal(name string) (Signal, bool) {
	all := Signals()
	i := slices.IndexFunc(all, func(s Signal) bool { return s.Name == name })
	if i < 0 {
		return Signal{}, false
	}
	return all[i], true
}

// FindAlertRule finds the rule of a signal; false when it doesn't alert.
func FindAlertRule(signal string) (AlertRule, bool) {
	i := slices.IndexFunc(AlertRules, func(r AlertRule) bool { return r.Signal == signal })
	if i < 0 {
		return AlertRule{}, false
	}
	return AlertRules[i], true
}

// WithChecks is env keeping the integrity checks run through it, so a
// check's sample IDs can be read after its signal (CheckSamples): the
// alerts keep them on the alert, for its page.
func (env Env) WithChecks() Env {
	return env.withMemo()
}

// CheckSamples are the public IDs (at most store.MaxCheckSamples) the
// integrity check named name found through env (WithChecks); nil when it
// wasn't run, or found none.
func (env Env) CheckSamples(name string) []string {
	if env.checks == nil {
		return nil
	}
	env.checks.mu.Lock()
	defer env.checks.mu.Unlock()
	return env.checks.results[name].Samples
}
