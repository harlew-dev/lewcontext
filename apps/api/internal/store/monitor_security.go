package store

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// The Security page's queries (docs/api/monitoring.md#security): sign-in
// attempts and the attacks they show, second factors, password checks,
// password resets, sign-ups, the admin door and sessions. Every one reads
// events by name in a window (events_name_created_idx), or the sessions
// table, which the cleanup keeps to live sessions.

// MonitorSpan is a review list's window, [From, To), grouped in periods of
// Per from From: an hour for "in an hour", or the whole span for a
// signal's own window.
type MonitorSpan struct {
	From, To time.Time
	Per      time.Duration
}

// period is the span's grouping column: date_bin of Per from From.
func (sp MonitorSpan) period() (string, []any) {
	return "date_bin(?::interval, e.created_at, ?)", []any{fmt.Sprintf("%d seconds", int(sp.Per.Seconds())), sp.From}
}

// Review list thresholds (docs/api/monitoring.md#security), and how many
// rows a list returns at most.
const (
	// StuffingEmails: an address trying this many distinct emails in a
	// period is replaying a list (credential stuffing).
	StuffingEmails = 10
	// GuessingAddresses: failures for one email from this many distinct
	// addresses in a period are a distributed guess.
	GuessingAddresses = 5
	// FailuresBeforeSuccess: a sign-in after this many failures in a
	// period may be the guess that worked.
	FailuresBeforeSuccess = 5
	// FailedCodes, FailedPasswordChecks: an account failing this many
	// second-factor codes, or password checks, in a period.
	FailedCodes          = 3
	FailedPasswordChecks = 3
	// LowOnBackupCodes: an account with this many recovery codes or fewer
	// left after using one.
	LowOnBackupCodes = 2
	// TakeoverLookback: how far back a sign-in's network is looked for in
	// the account's own sign-ins, through events_logged_in_idx.
	TakeoverLookback = 30 * 24 * time.Hour
	// maxReviewRows is a review list's length.
	maxReviewRows = 20
)

// takeoverChanges are what someone holding an account changes first: its
// password, its second factor, its recovery codes, its other sessions.
var takeoverChanges = []string{"auth.password_changed", "auth.mfa.disabled", "auth.recovery_codes.created", "auth.other_sessions_revoked"}

// StuffingRow is an address (an IPv6 /64) that tried StuffingEmails or more
// distinct emails in a period.
type StuffingRow struct {
	Period    time.Time `json:"period" bun:"period"`
	IP        string    `json:"ip" bun:"ip"`
	Emails    int64     `json:"emails" bun:"emails"`
	Attempts  int64     `json:"attempts" bun:"attempts"`
	Successes int64     `json:"successes" bun:"successes"`
}

func (s *Store) stuffing(sp MonitorSpan) *bun.SelectQuery {
	period, args := sp.period()
	return s.eventsIn(sp.From, sp.To, "auth.login_attempted").
		ColumnExpr(period+" AS period", args...).
		ColumnExpr(ipNetwork+" AS ip").
		ColumnExpr("count(DISTINCT lower(e.payload->>'email')) AS emails").
		ColumnExpr("count(*) AS attempts").
		ColumnExpr("count(*) FILTER (WHERE e.payload->>'result' IN (?)) AS successes", bun.In(signInSucceeded)).
		Where("e.ip IS NOT NULL").
		GroupExpr("1, 2").
		Having("count(DISTINCT lower(e.payload->>'email')) >= ?", StuffingEmails)
}

// IndexStuffing lists the addresses that tried StuffingEmails or more
// emails in a period, most emails first.
func (s *Store) IndexStuffing(ctx context.Context, sp MonitorSpan) ([]StuffingRow, error) {
	rows := []StuffingRow{}
	err := s.stuffing(sp).OrderExpr("emails DESC, period DESC, ip").Limit(maxReviewRows).Scan(ctx, &rows)
	return rows, err
}

// CountStuffing counts the addresses IndexStuffing finds, and those of
// them with a successful sign-in.
func (s *Store) CountStuffing(ctx context.Context, sp MonitorSpan) (addresses, succeeded int64, err error) {
	err = s.DB.NewSelect().TableExpr("(?) AS g", s.stuffing(sp)).
		ColumnExpr("count(DISTINCT g.ip)").
		ColumnExpr("count(DISTINCT g.ip) FILTER (WHERE g.successes > 0)").
		Scan(ctx, &addresses, &succeeded)
	return addresses, succeeded, err
}

// GuessingRow is an email that failed to sign in from GuessingAddresses
// or more distinct addresses in a period.
type GuessingRow struct {
	Period    time.Time `json:"period" bun:"period"`
	Email     string    `json:"email" bun:"email"`
	Addresses int64     `json:"addresses" bun:"addresses"`
	Failures  int64     `json:"failures" bun:"failures"`
}

func (s *Store) guessing(sp MonitorSpan) *bun.SelectQuery {
	period, args := sp.period()
	return s.eventsIn(sp.From, sp.To, "auth.login_attempted").
		ColumnExpr(period+" AS period", args...).
		ColumnExpr("lower(e.payload->>'email') AS email").
		ColumnExpr("count(DISTINCT "+ipNetwork+") AS addresses").
		ColumnExpr("count(*) AS failures").
		Where("e.payload->>'result' IN (?)", bun.In(signInFailed)).
		Where("e.ip IS NOT NULL").
		GroupExpr("1, 2").
		Having("count(DISTINCT "+ipNetwork+") >= ?", GuessingAddresses)
}

// IndexGuessing lists the emails failing from GuessingAddresses or more
// addresses in a period, most addresses first.
func (s *Store) IndexGuessing(ctx context.Context, sp MonitorSpan) ([]GuessingRow, error) {
	rows := []GuessingRow{}
	err := s.guessing(sp).OrderExpr("addresses DESC, period DESC, email").Limit(maxReviewRows).Scan(ctx, &rows)
	return rows, err
}

// CountGuessing counts the emails IndexGuessing finds.
func (s *Store) CountGuessing(ctx context.Context, sp MonitorSpan) (int64, error) {
	return s.countGroups(ctx, s.guessing(sp))
}

// SignInShare is how many sign-in attempts there were in a window, and how
// many of them named an email with no account.
type SignInShare struct {
	Attempts int64 `json:"attempts"`
	Unknown  int64 `json:"unknown"`
}

// CountUnknownAccounts counts the sign-in attempts in [from, to), and
// those for an email with no account.
func (s *Store) CountUnknownAccounts(ctx context.Context, from, to time.Time) (SignInShare, error) {
	var out SignInShare
	err := s.eventsIn(from, to, "auth.login_attempted").
		ColumnExpr("count(*)").
		ColumnExpr("count(*) FILTER (WHERE e.payload->>'result' = 'unknown_account')").
		Scan(ctx, &out.Attempts, &out.Unknown)
	return out, err
}

// CountFailedSignIns counts the failed sign-in attempts (a wrong password
// or an unknown account) in [from, to).
func (s *Store) CountFailedSignIns(ctx context.Context, from, to time.Time) (int64, error) {
	return countEvents(ctx, s.eventsIn(from, to, "auth.login_attempted").Where("e.payload->>'result' IN (?)", bun.In(signInFailed)))
}

// SuccessAfterFailuresRow is an account signed in (the right password,
// even if a code was still needed) after FailuresBeforeSuccess or more
// wrong passwords in a period.
type SuccessAfterFailuresRow struct {
	Period     time.Time  `json:"period" bun:"period"`
	Account    AccountRef `json:"account" bun:"-"`
	User       string     `json:"-" bun:"account"`
	Failures   int64      `json:"failures" bun:"failures"`
	SignedInAt time.Time  `json:"signedInAt" bun:"signed_in_at"`
}

func (s *Store) successAfterFailures(sp MonitorSpan) *bun.SelectQuery {
	period, args := sp.period()
	return s.eventsIn(sp.From, sp.To, "auth.login_attempted").
		ColumnExpr(period+" AS period", args...).
		ColumnExpr("e.payload->>'user' AS account").
		ColumnExpr("count(*) FILTER (WHERE e.payload->>'result' = 'wrong_password') AS failures").
		ColumnExpr("max(e.created_at) FILTER (WHERE e.payload->>'result' IN (?)) AS signed_in_at", bun.In(signInSucceeded)).
		Where("e.payload->>'user' IS NOT NULL").
		GroupExpr("1, 2").
		Having("count(*) FILTER (WHERE e.payload->>'result' = 'wrong_password') >= ?", FailuresBeforeSuccess).
		Having("max(e.created_at) FILTER (WHERE e.payload->>'result' IN (?)) > min(e.created_at) FILTER (WHERE e.payload->>'result' = 'wrong_password')", bun.In(signInSucceeded))
}

// IndexSuccessAfterFailures lists the accounts signed in after
// FailuresBeforeSuccess or more failures in a period, newest first.
func (s *Store) IndexSuccessAfterFailures(ctx context.Context, sp MonitorSpan) ([]SuccessAfterFailuresRow, error) {
	rows := []SuccessAfterFailuresRow{}
	if err := s.successAfterFailures(sp).OrderExpr("signed_in_at DESC").Limit(maxReviewRows).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.User
	}
	refs, err := s.accountRefsByPublicID(ctx, ids)
	for i := range rows {
		rows[i].Account = refs[rows[i].User]
	}
	return rows, err
}

// CountSuccessAfterFailures counts the accounts IndexSuccessAfterFailures
// finds.
func (s *Store) CountSuccessAfterFailures(ctx context.Context, sp MonitorSpan) (int64, error) {
	return s.countGroups(ctx, s.successAfterFailures(sp))
}

// TakeoverRow is a change someone holding an account makes first (its
// password, second factor, recovery codes or other sessions), within an
// hour of a sign-in from a network the account hadn't signed in from in
// TakeoverLookback, though it had signed in in that time.
type TakeoverRow struct {
	ChangedAt  time.Time  `json:"changedAt" bun:"changed_at"`
	Change     string     `json:"change" bun:"change"`
	Event      string     `json:"event" bun:"event"`
	Account    AccountRef `json:"account" bun:"-"`
	UserID     int64      `json:"-" bun:"user_id"`
	SignedInAt time.Time  `json:"signedInAt" bun:"signed_in_at"`
	Network    string     `json:"network" bun:"network"`
}

func (s *Store) takeovers(from, to time.Time) *bun.SelectQuery {
	lookback := fmt.Sprintf("%d seconds", int(TakeoverLookback.Seconds()))
	earlier := func(alias string) *bun.SelectQuery {
		return s.DB.NewSelect().TableExpr("events AS "+alias).ColumnExpr("1").
			Where(alias+".name = 'auth.logged_in'").
			Where(alias+".user_id = l.user_id").
			Where(alias+".created_at < l.created_at").
			Where(alias+".created_at >= l.created_at - ?::interval", lookback).
			Where(alias + ".ip IS NOT NULL")
	}
	signIn := s.DB.NewSelect().TableExpr("events AS l").
		ColumnExpr("l.created_at").
		ColumnExpr("text("+fmt.Sprintf(signInNetwork, "l")+") AS network").
		Where("l.name = 'auth.logged_in'").
		Where("l.user_id = e.user_id").
		Where("l.created_at <= e.created_at").
		Where("l.created_at >= e.created_at - interval '1 hour'").
		Where("l.ip IS NOT NULL").
		// It had a baseline: signed in before, in the lookback,
		Where("EXISTS (?)", earlier("b")).
		// but never from this network.
		Where("NOT EXISTS (?)", earlier("p").Where("p.ip <<= "+fmt.Sprintf(signInNetwork, "l"))).
		OrderExpr("l.created_at DESC").
		Limit(1)
	return s.eventsIn(from, to, takeoverChanges...).
		ColumnExpr("e.created_at AS changed_at, e.name AS change, e.public_id AS event, e.user_id").
		ColumnExpr("si.created_at AS signed_in_at, si.network").
		Join("JOIN LATERAL (?) AS si ON true", signIn).
		Where("e.user_id IS NOT NULL")
}

// IndexTakeovers lists possible takeovers in [from, to), newest first.
func (s *Store) IndexTakeovers(ctx context.Context, from, to time.Time) ([]TakeoverRow, error) {
	rows := []TakeoverRow{}
	if err := s.takeovers(from, to).OrderExpr("e.created_at DESC").Limit(maxReviewRows).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.UserID
	}
	refs, err := s.accountRefs(ctx, ids)
	for i := range rows {
		rows[i].Account = refs[rows[i].UserID]
	}
	return rows, err
}

// CountTakeovers counts the changes IndexTakeovers finds.
func (s *Store) CountTakeovers(ctx context.Context, from, to time.Time) (int64, error) {
	return s.countGroups(ctx, s.takeovers(from, to))
}

// FailedChecksRow is an account that failed a check (second-factor codes,
// or its password) at least so many times in a period.
type FailedChecksRow struct {
	Period   time.Time  `json:"period" bun:"period"`
	Account  AccountRef `json:"account" bun:"-"`
	User     string     `json:"-" bun:"account"`
	Failures int64      `json:"failures" bun:"failures"`
}

// failedChecks groups failed checks recorded as name (with ok false) by
// account and period, keeping those with min or more.
func (s *Store) failedChecks(sp MonitorSpan, name string, min int) *bun.SelectQuery {
	period, args := sp.period()
	return s.eventsIn(sp.From, sp.To, name).
		ColumnExpr(period+" AS period", args...).
		ColumnExpr("e.payload->>'user' AS account").
		ColumnExpr("count(*) AS failures").
		Where("e.payload->>'ok' = 'false'").
		Where("e.payload->>'user' IS NOT NULL").
		GroupExpr("1, 2").
		Having("count(*) >= ?", min)
}

// Failed checks a review list counts: second-factor codes and passwords.
const (
	CheckSecondFactor = "auth.mfa.checked"
	CheckPassword     = "auth.password_checked"
)

// failedCheckMinimum is how many failures of each check put an account on
// its list.
var failedCheckMinimum = map[string]int{CheckSecondFactor: FailedCodes, CheckPassword: FailedPasswordChecks}

// IndexFailedChecks lists the accounts failing check (CheckSecondFactor,
// CheckPassword) its minimum times or more in a period, most first.
func (s *Store) IndexFailedChecks(ctx context.Context, check string, sp MonitorSpan) ([]FailedChecksRow, error) {
	rows := []FailedChecksRow{}
	if err := s.failedChecks(sp, check, failedCheckMinimum[check]).OrderExpr("failures DESC, period DESC").Limit(maxReviewRows).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.User
	}
	refs, err := s.accountRefsByPublicID(ctx, ids)
	for i := range rows {
		rows[i].Account = refs[rows[i].User]
	}
	return rows, err
}

// CountFailedChecks counts the accounts IndexFailedChecks finds.
func (s *Store) CountFailedChecks(ctx context.Context, check string, sp MonitorSpan) (int64, error) {
	return s.countGroups(ctx, s.failedChecks(sp, check, failedCheckMinimum[check]))
}

// BackupCodes is how recovery codes were used in a window: codes that
// worked, and accounts whose last use left LowOnBackupCodes or fewer.
type BackupCodes struct {
	Used        int64 `json:"used"`
	LowAccounts int64 `json:"lowAccounts"`
}

// CountBackupCodes counts recovery codes used in [from, to), and the
// accounts left with LowOnBackupCodes or fewer.
func (s *Store) CountBackupCodes(ctx context.Context, from, to time.Time) (BackupCodes, error) {
	var out BackupCodes
	used := s.eventsIn(from, to, "auth.mfa.checked").
		Where("e.payload->>'method' = 'recovery_code'").
		Where("e.payload->>'ok' = 'true'")
	var err error
	if out.Used, err = countEvents(ctx, used); err != nil {
		return out, err
	}
	last := s.eventsIn(from, to, "auth.mfa.checked").
		DistinctOn("e.payload->>'user'").
		ColumnExpr("(e.payload->>'recoveryCodesLeft')::int AS left_after").
		Where("e.payload->>'method' = 'recovery_code'").
		Where("e.payload->>'ok' = 'true'").
		Where("e.payload->>'recoveryCodesLeft' IS NOT NULL").
		OrderExpr("e.payload->>'user', e.created_at DESC")
	err = s.DB.NewSelect().TableExpr("(?) AS g", last).ColumnExpr("count(*)").
		Where("g.left_after <= ?", LowOnBackupCodes).Scan(ctx, &out.LowAccounts)
	return out, err
}

// AddressCount is how many events came from an address (an IPv6 /64).
type AddressCount struct {
	IP    string `json:"ip" bun:"ip"`
	Count int64  `json:"count" bun:"count"`
}

// maxTopAddresses is how many addresses a top list holds.
const maxTopAddresses = 10

// IndexTopAddresses lists the addresses that recorded the most events
// named name in [from, to), most first.
func (s *Store) IndexTopAddresses(ctx context.Context, name string, from, to time.Time) ([]AddressCount, error) {
	rows := []AddressCount{}
	err := s.eventsIn(from, to, name).
		ColumnExpr(ipNetwork+" AS ip").
		ColumnExpr("count(*) AS count").
		Where("e.ip IS NOT NULL").
		GroupExpr("1").
		OrderExpr("count DESC, ip").
		Limit(maxTopAddresses).
		Scan(ctx, &rows)
	return rows, err
}

// Sign-in attempts per bucket, by result.
var signInResults = []string{"succeeded", "mfa_required", "wrong_password", "unknown_account"}

// SummarizeSignIns counts sign-in attempts per bucket of w, by result.
func (s *Store) SummarizeSignIns(ctx context.Context, w SummaryWindow) (MonitorSeries, error) {
	return s.bucketedCounts(ctx, w, []string{"auth.login_attempted"}, signInResults, "e.payload->>'result'")
}

// SummarizeEventNames counts events named names per bucket of w, a series
// per name in that order.
func (s *Store) SummarizeEventNames(ctx context.Context, w SummaryWindow, names ...string) (MonitorSeries, error) {
	return s.bucketedCounts(ctx, w, names, names, "e.name")
}

// CountEvents counts the events named names in [from, to).
func (s *Store) CountEvents(ctx context.Context, from, to time.Time, names ...string) (int64, error) {
	return countEvents(ctx, s.eventsIn(from, to, names...))
}

// Admin door outcomes: a failed guess at the door, and a refusal of
// credentials that were right (or of the key from outside the allowlist):
// someone holds the key, and maybe a real password.
var (
	DoorFailed  = []string{"wrong_password", "unknown_account"}
	DoorRefused = []string{"not_admin", "mfa_off"}
)

// doorKey names an admin door event's outcome: its result, or the event's
// last segment (access_refused, moved).
const doorKey = "CASE WHEN e.name = 'admin.login_attempted' THEN e.payload->>'result' WHEN e.name = 'admin.access_refused' THEN 'access_refused' ELSE 'session_moved' END"

// doorNames are the admin door's events a page reads.
var doorNames = []string{"admin.login_attempted", "admin.access_refused", "admin.session.moved"}

// DoorOutcomes are the admin door's outcomes in order: failed guesses,
// refusals, moved sessions.
var DoorOutcomes = []string{"wrong_password", "unknown_account", "not_admin", "mfa_off", "access_refused", "session_moved"}

// CountDoor counts the admin door's outcomes in [from, to), by outcome
// (DoorOutcomes); sign-ins that went on to the code aren't counted.
func (s *Store) CountDoor(ctx context.Context, from, to time.Time) (map[string]int64, error) {
	var rows []struct {
		Key   string `bun:"key"`
		Count int64  `bun:"count"`
	}
	err := s.eventsIn(from, to, doorNames...).
		ColumnExpr(doorKey+" AS key").
		ColumnExpr("count(*) AS count").
		GroupExpr("1").
		Scan(ctx, &rows)
	out := map[string]int64{}
	for _, r := range rows {
		out[r.Key] = r.Count
	}
	return out, err
}

// DoorEvent is one of the admin door's events in a review list: when, its
// outcome, the address, and the event to open. Never the email typed.
type DoorEvent struct {
	At      time.Time `json:"at" bun:"at"`
	Outcome string    `json:"outcome" bun:"outcome"`
	IP      *string   `json:"ip" bun:"ip"`
	Event   string    `json:"event" bun:"event"`
}

// IndexDoorEvents lists the admin door's failures, refusals and moved
// sessions in [from, to), newest first.
func (s *Store) IndexDoorEvents(ctx context.Context, from, to time.Time) ([]DoorEvent, error) {
	rows := []DoorEvent{}
	err := s.eventsIn(from, to, doorNames...).
		ColumnExpr("e.created_at AS at, "+doorKey+" AS outcome, host(e.ip) AS ip, e.public_id AS event").
		Where("e.name <> 'admin.login_attempted' OR e.payload->>'result' IN (?)", bun.In(append(append([]string{}, DoorFailed...), DoorRefused...))).
		OrderExpr("e.created_at DESC").
		Limit(maxReviewRows).
		Scan(ctx, &rows)
	return rows, err
}

// The admin door's chart series: failed guesses, refusals (of real
// credentials, or of the key from outside the allowlist), moved sessions.
// Three, not one per outcome: a chart stacks five at most.
var DoorSeries = []string{"failed", "refused", "moved"}

// doorSeriesKey puts an admin door event in its DoorSeries; a sign-in that
// went on to its code is in none.
const doorSeriesKey = "CASE WHEN e.name = 'admin.access_refused' THEN 'refused' WHEN e.name = 'admin.session.moved' THEN 'moved' WHEN e.payload->>'result' IN (?) THEN 'failed' WHEN e.payload->>'result' IN (?) THEN 'refused' END"

// SummarizeDoor counts the admin door's failures, refusals and moved
// sessions per bucket of w (DoorSeries).
func (s *Store) SummarizeDoor(ctx context.Context, w SummaryWindow) (MonitorSeries, error) {
	return s.bucketedCounts(ctx, w, doorNames, DoorSeries, doorSeriesKey, bun.In(DoorFailed), bun.In(DoorRefused))
}

// MaxSessionsPerAccount is how many sessions an account keeps at most
// (CreateSession drops the oldest past it).
const MaxSessionsPerAccount = 5

// SessionCounts are the live app sessions, and the accounts holding the
// most an account may.
type SessionCounts struct {
	Live          int64 `json:"live"`
	AccountsAtCap int64 `json:"accountsAtCap"`
}

// CountSessions counts live app sessions (not expired, not idle), and the
// accounts at MaxSessionsPerAccount.
func (s *Store) CountSessions(ctx context.Context) (SessionCounts, error) {
	var out SessionCounts
	live := func() *bun.SelectQuery {
		return s.DB.NewSelect().Model((*sessionRecord)(nil)).
			Where("s.expires_at > now()").
			Where("s.last_seen_at > now() - interval '30 minutes'")
	}
	n, err := live().Count(ctx)
	out.Live = int64(n)
	if err != nil {
		return out, err
	}
	out.AccountsAtCap, err = s.countGroups(ctx, live().ColumnExpr("s.user_id").GroupExpr("s.user_id").Having("count(*) >= ?", MaxSessionsPerAccount))
	return out, err
}
