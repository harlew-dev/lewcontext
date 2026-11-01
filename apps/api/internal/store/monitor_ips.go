package store

import (
	"context"
	"lewcontext/api/internal/events"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

// The IP addresses page's queries (docs/api/monitoring.md#ip-addresses):
// the addresses events came from in a window, those making many accounts,
// how much of the traffic one address holds, and what one address did.
// Every one reads events in a window: all of them through
// events_created_idx, by name through events_name_created_idx, one
// address's (or one IPv6 /64's) through events_ip_created_idx.
//
// The admin dashboard's own reads (admin.<resource>.listed and .viewed)
// are left out of the lists: they're the dashboard's traffic, not the
// app's, and would put the admin's own address on top. One address's page
// counts everything recorded from it.

// SignUpCluster: an address (an IPv6 /64) making this many accounts in a
// day is a warning: the usual shape of a spam farm.
const SignUpCluster = 5

// maxTopAddressRows is how many addresses a top list keeps for each of its
// columns: the top 10 by each, so sorting by any column shows its top 10.
const maxTopAddressRows = 10

// hostAddress is an event's address on its own; ipNetwork groups an IPv6
// address by its /64.
const hostAddress = "host(e.ip)"

// AddressRow is an address's activity in a window: the events recorded
// from it, its failed sign-ins, accounts made and limit windows reached,
// and how many accounts acted or signed in from it. Network: grouped as
// the signals count (an IPv6 address by its /64), or each address on its
// own; the IPv4 rows are the same either way.
type AddressRow struct {
	IP            string `json:"ip" bun:"ip"`
	Network       bool   `json:"network" bun:"network"`
	Events        int64  `json:"events" bun:"events"`
	FailedSignIns int64  `json:"failedSignIns" bun:"failed_sign_ins"`
	SignUps       int64  `json:"signUps" bun:"sign_ups"`
	LimitsReached int64  `json:"limitsReached" bun:"limits_reached"`
	Accounts      int64  `json:"accounts" bun:"accounts"`
}

// addressColumns are a top list's columns, each ranked on its own.
var addressColumns = []string{"events", "failed_sign_ins", "sign_ups", "limits_reached", "accounts"}

// IndexAddressActivity lists the addresses events came from in [from, to),
// both ways at once (grouped as the signals count, and each address on its
// own: one scan, two grouping sets), keeping the top maxTopAddressRows of
// each grouping by each column. Admin reads are left out.
func (s *Store) IndexAddressActivity(ctx context.Context, from, to time.Time) ([]AddressRow, error) {
	grouped := s.DB.NewSelect().Model((*eventRecord)(nil)).
		ColumnExpr("CASE WHEN grouping("+hostAddress+") = 0 THEN "+hostAddress+" ELSE "+ipNetwork+" END AS ip").
		ColumnExpr("grouping("+hostAddress+") = 1 AS network").
		ColumnExpr("count(*) AS events").
		ColumnExpr("count(*) FILTER (WHERE e.name = 'auth.login_attempted' AND e.payload->>'result' IN (?)) AS failed_sign_ins", bun.In(signInFailed)).
		ColumnExpr("count(*) FILTER (WHERE e.name = 'auth.registered') AS sign_ups").
		ColumnExpr("count(*) FILTER (WHERE e.name = 'rate_limit.reached') AS limits_reached").
		ColumnExpr("count(DISTINCT coalesce(e.user_id, e.actor_id)) AS accounts").
		Where("e.created_at >= ?", from).
		Where("e.created_at < ?", to).
		Where("e.ip IS NOT NULL").
		Where("e.name NOT IN (?)", bun.In(events.AdminReadNames())).
		GroupExpr("GROUPING SETS ((" + hostAddress + "), (" + ipNetwork + "))")
	// Each column ranked within its grouping (fixed names, never input).
	ranked := s.DB.NewSelect().TableExpr("(?) AS g", grouped).ColumnExpr("g.*")
	top := make([]string, len(addressColumns))
	for i, c := range addressColumns {
		ranked = ranked.ColumnExpr("row_number() OVER (PARTITION BY g.network ORDER BY g." + c + " DESC, g.ip) AS rank_" + c)
		top[i] = "r.rank_" + c + " <= ?"
	}
	args := make([]any, len(addressColumns))
	for i := range args {
		args[i] = maxTopAddressRows
	}
	rows := []AddressRow{}
	err := s.DB.NewSelect().TableExpr("(?) AS r", ranked).
		Column("r.ip", "r.network", "r.events", "r.failed_sign_ins", "r.sign_ups", "r.limits_reached", "r.accounts").
		Where(strings.Join(top, " OR "), args...).
		OrderExpr("r.network DESC, r.events DESC, r.ip").
		Scan(ctx, &rows)
	return rows, err
}

// AddressShare is how much of the traffic one address holds: the events
// recorded from any address in a window, and from the busiest one.
type AddressShare struct {
	Events  int64  `json:"events"`
	Busiest int64  `json:"busiest"`
	IP      string `json:"ip"`
}

// FindBusiestAddress counts the events recorded from an address in
// [from, to), and those from the busiest address (exactly, not by network:
// a proxy's address is one address), admin reads left out. When the
// proxy's client IP isn't configured, every event comes from the proxy.
func (s *Store) FindBusiestAddress(ctx context.Context, from, to time.Time) (AddressShare, error) {
	var out AddressShare
	base := func() *bun.SelectQuery {
		return s.DB.NewSelect().Model((*eventRecord)(nil)).
			Where("e.created_at >= ?", from).
			Where("e.created_at < ?", to).
			Where("e.ip IS NOT NULL").
			Where("e.name NOT IN (?)", bun.In(events.AdminReadNames()))
	}
	n, err := base().Count(ctx)
	if err != nil || n == 0 {
		return out, err
	}
	out.Events = int64(n)
	err = base().ColumnExpr(hostAddress+" AS ip, count(*) AS busiest").
		GroupExpr("1").OrderExpr("busiest DESC, ip").Limit(1).
		Scan(ctx, &out.IP, &out.Busiest)
	return out, err
}

// SignUpClusterRow is an address (an IPv6 /64) that made SignUpCluster or
// more accounts in a period.
type SignUpClusterRow struct {
	Period   time.Time `json:"period" bun:"period"`
	IP       string    `json:"ip" bun:"ip"`
	Accounts int64     `json:"accounts" bun:"accounts"`
}

// signUpClusters groups auth.registered by period and address, keeping
// those with at least min accounts.
func (s *Store) signUpClusters(sp MonitorSpan, min int) *bun.SelectQuery {
	period, args := sp.period()
	return s.eventsIn(sp.From, sp.To, "auth.registered").
		ColumnExpr(period+" AS period", args...).
		ColumnExpr(ipNetwork+" AS ip").
		ColumnExpr("count(*) AS accounts").
		Where("e.ip IS NOT NULL").
		GroupExpr("1, 2").
		Having("count(*) >= ?", min)
}

// IndexSignUpClusters lists the addresses that made SignUpCluster or more
// accounts in a period, most first.
func (s *Store) IndexSignUpClusters(ctx context.Context, sp MonitorSpan) ([]SignUpClusterRow, error) {
	rows := []SignUpClusterRow{}
	err := s.signUpClusters(sp, SignUpCluster).OrderExpr("accounts DESC, period DESC, ip").Limit(maxReviewRows).Scan(ctx, &rows)
	return rows, err
}

// CountLargestSignUpCluster counts the most accounts made from one address
// (an IPv6 /64) in a period of sp.
func (s *Store) CountLargestSignUpCluster(ctx context.Context, sp MonitorSpan) (int64, error) {
	var n int64
	err := s.DB.NewSelect().TableExpr("(?) AS g", s.signUpClusters(sp, 1)).
		ColumnExpr("coalesce(max(g.accounts), 0)").Scan(ctx, &n)
	return n, err
}

// AddressActivity is what one address (or one IPv6 /64) did in a window:
// when it was first and last seen, its events by name, the accounts seen
// from it, the limits it reached, and, for a /64, its addresses.
type AddressActivity struct {
	Events    int64            `json:"events"`
	FirstSeen *time.Time       `json:"firstSeen"`
	LastSeen  *time.Time       `json:"lastSeen"`
	ByName    []NameCount      `json:"byName"`
	Accounts  []AddressAccount `json:"accounts"`
	// UnknownEmails is how many different emails with no account it tried
	// to sign in with: a count, never the emails (its events show them).
	UnknownEmails int64          `json:"unknownEmails"`
	Limits        []LimitReached `json:"limits"`
	Addresses     []AddressCount `json:"addresses"`
}

// AddressAccount is an account seen from an address: signed in, made
// there, or failing to sign in.
type AddressAccount struct {
	Account    AccountRef `json:"account" bun:"-"`
	User       string     `json:"-" bun:"account"`
	SignedIn   int64      `json:"signedIn" bun:"signed_in"`
	Registered int64      `json:"registered" bun:"registered"`
	Failed     int64      `json:"failed" bun:"failed"`
}

// fromAddress selects events from ip (an address, or a CIDR network) in
// [from, to): events_ip_created_idx finds them.
func (s *Store) fromAddress(ip string, from, to time.Time) *bun.SelectQuery {
	return s.DB.NewSelect().Model((*eventRecord)(nil)).
		Where("e.ip IS NOT NULL AND e.ip <<= ?::inet", ip).
		Where("e.created_at >= ?", from).
		Where("e.created_at < ?", to)
}

// FindAddressActivity reads what ip (an address, or an IPv6 /64 as CIDR)
// did in [from, to). Everything recorded from it counts, admin reads
// included.
func (s *Store) FindAddressActivity(ctx context.Context, ip string, from, to time.Time) (AddressActivity, error) {
	out := AddressActivity{ByName: []NameCount{}, Accounts: []AddressAccount{}, Limits: []LimitReached{}, Addresses: []AddressCount{}}
	var first, last *time.Time
	err := s.fromAddress(ip, from, to).
		ColumnExpr("count(*), min(e.created_at), max(e.created_at)").
		Scan(ctx, &out.Events, &first, &last)
	if err != nil || out.Events == 0 {
		return out, err
	}
	out.FirstSeen, out.LastSeen = first, last
	steps := []func() error{
		func() error {
			return s.fromAddress(ip, from, to).ColumnExpr("e.name, count(*) AS count").
				GroupExpr("e.name").OrderExpr("count DESC, e.name").Scan(ctx, &out.ByName)
		},
		func() (err error) { out.Accounts, err = s.addressAccounts(ctx, ip, from, to); return },
		func() error {
			return s.fromAddress(ip, from, to).
				ColumnExpr("count(DISTINCT lower(e.payload->>'email'))").
				Where("e.name = 'auth.login_attempted'").
				Where("e.payload->>'result' = 'unknown_account'").
				Scan(ctx, &out.UnknownEmails)
		},
		func() error {
			return s.fromAddress(ip, from, to).
				ColumnExpr(reachedLimit+" AS limit_name, count(*) AS windows, max(e.created_at) AS last_reached").
				Where("e.name = 'rate_limit.reached'").
				GroupExpr("1").OrderExpr("windows DESC, 1").Scan(ctx, &out.Limits)
		},
		func() error {
			return s.fromAddress(ip, from, to).ColumnExpr(hostAddress+" AS ip, count(*) AS count").
				GroupExpr("1").OrderExpr("count DESC, ip").Limit(maxReviewRows).Scan(ctx, &out.Addresses)
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return out, err
		}
	}
	return out, nil
}

// addressAccounts lists the accounts seen from ip in [from, to): signed in
// (auth.logged_in), made there (auth.registered), or failing to sign in
// (auth.login_attempted naming an account), most events first.
func (s *Store) addressAccounts(ctx context.Context, ip string, from, to time.Time) ([]AddressAccount, error) {
	rows := []AddressAccount{}
	err := s.fromAddress(ip, from, to).
		ColumnExpr("e.payload->>'user' AS account").
		ColumnExpr("count(*) FILTER (WHERE e.name = 'auth.logged_in') AS signed_in").
		ColumnExpr("count(*) FILTER (WHERE e.name = 'auth.registered') AS registered").
		ColumnExpr("count(*) FILTER (WHERE e.name = 'auth.login_attempted' AND e.payload->>'result' IN (?)) AS failed", bun.In(signInFailed)).
		Where("e.name IN ('auth.logged_in', 'auth.registered', 'auth.login_attempted')").
		Where("e.payload->>'user' IS NOT NULL").
		GroupExpr("1").
		Having("count(*) FILTER (WHERE e.name <> 'auth.login_attempted' OR e.payload->>'result' IN (?)) > 0", bun.In(signInFailed)).
		OrderExpr("count(*) DESC, 1").
		Limit(maxReviewRows).
		Scan(ctx, &rows)
	if err != nil {
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
