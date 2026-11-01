package store

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"net/netip"
	"strings"
	"time"
)

// Minimising old events (docs/compliance.md#retention): once an event is
// 30 days old, the pseudonymise prune target cuts its IP to the network
// (an IPv4 /24, an IPv6 /48) and replaces the email addresses in its
// payload with pseudonyms (auth.Keys.EmailPseudonym, keyed by APP_KEY), and
// marks it minimised_at, so a run never looks at it again. A pseudonym
// still matches itself, so old events about one address can be found
// together, and an Events search for the address finds them; the address
// itself can't be read back without APP_KEY.

// Minimised networks: how much of an address an old event keeps.
const (
	minimisedIPv4Bits = 24
	minimisedIPv6Bits = 48
)

// PrunablePseudonymise is the events the pseudonymise target hasn't
// minimised yet, keeping (for now) those with a listener still to run: a
// listener may need the address to send its email.
var PrunablePseudonymise = Prunable{
	Model: (*eventRecord)(nil),
	Age:   "created_at",
	Scope: func(q *bun.SelectQuery) *bun.SelectQuery { return q.Where("e.minimised_at IS NULL") },
	Keep:  withPendingDeliveries,
}

// minimisable is an event row as the pseudonymise target reads it.
type minimisable struct {
	bun.BaseModel `bun:"table:events,alias:e"`
	ID            int64
	IP            string // text(e.ip): "198.51.100.7/32", or "" for none
	Payload       json.RawMessage
}

// PseudonymiseEvents minimises up to limit events created before before
// (the oldest first) and says how many it minimised, in one transaction.
// Rows another run holds are skipped (SKIP LOCKED), and a row is minimised
// once: minimised_at leaves it out of every later run. pseudonym is
// auth.Keys.EmailPseudonym.
func (s *Store) PseudonymiseEvents(ctx context.Context, before time.Time, limit int, pseudonym func(string) string) (int, error) {
	var count int
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var rows []minimisable
		err := tx.NewSelect().Model(&rows).
			Column("e.id", "e.payload").
			ColumnExpr("coalesce(text(e.ip), '') AS ip").
			Where("e.minimised_at IS NULL AND e.created_at < ?", before).
			WhereGroup(" AND NOT ", withPendingDeliveries).
			OrderExpr("e.created_at, e.id").
			Limit(int64(limit)).
			For("UPDATE OF e SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			payload, err := minimisePayload(row.Payload, pseudonym)
			if err != nil {
				return err
			}
			q := tx.NewUpdate().Model((*eventRecord)(nil)).
				Set("payload = ?", string(payload)).
				Set("minimised_at = now()").
				Where("e.id = ?", row.ID)
			if row.IP != "" {
				q = q.Set("ip = ?::inet", minimiseIP(row.IP))
			}
			if _, err := q.Exec(ctx); err != nil {
				return err
			}
		}
		count = len(rows)
		return nil
	})
	return count, err
}

// minimiseIP cuts an address or range (as PostgreSQL prints an inet) to
// its IPv4 /24 or IPv6 /48 network; a range already as wide is kept, and
// anything that doesn't parse becomes the widest network of all, which
// says nothing.
func minimiseIP(text string) string {
	prefix, err := netip.ParsePrefix(text)
	if err != nil {
		addr, addrErr := netip.ParseAddr(text)
		if addrErr != nil {
			return "0.0.0.0/0"
		}
		prefix = netip.PrefixFrom(addr, addr.BitLen())
	}
	addr, bits := prefix.Addr(), prefix.Bits()
	if addr.Is4In6() {
		addr, bits = addr.Unmap(), max(bits-96, 0)
	}
	addr = addr.WithZone("")
	keep := minimisedIPv6Bits
	if addr.Is4() {
		keep = minimisedIPv4Bits
	}
	return netip.PrefixFrom(addr, min(bits, keep)).Masked().String()
}

// minimisePayload replaces the email addresses in an event's payload with
// pseudonyms: its email, and on the admin reads the email searched for
// (filters.email) and a search (q) holding an "@"; and cuts an IP searched
// for (filters.ip) to its network. Everything else stays as it was,
// numbers included.
func minimisePayload(raw json.RawMessage, pseudonym func(string) string) ([]byte, error) {
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	minimiseEmail(payload, "email", pseudonym)
	if q, ok := payload["q"].(string); ok && strings.Contains(q, "@") {
		minimiseEmail(payload, "q", pseudonym)
	}
	if filters, ok := payload["filters"].(map[string]any); ok {
		minimiseEmail(filters, "email", pseudonym)
		if ip, ok := filters["ip"].(string); ok && ip != "" {
			filters["ip"] = minimiseIP(ip)
		}
	}
	return json.Marshal(payload)
}

// minimiseEmail replaces m[key] with its pseudonym, unless it's empty, a
// pseudonym already or redacted (erasure's "[deleted]").
func minimiseEmail(m map[string]any, key string, pseudonym func(string) string) {
	value, ok := m[key].(string)
	if !ok || value == "" || value == Redacted || strings.HasPrefix(value, auth.EmailPseudonymPrefix) {
		return
	}
	m[key] = pseudonym(value)
}
