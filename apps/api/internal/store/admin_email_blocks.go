package store

import (
	"context"
	"github.com/uptrace/bun"
	"time"
)

// The admin dashboard's Email blocks page: every blocked address, across
// the whole app, reachable only behind the admin key and an admin session
// (docs/api/admin.md). It shows the address and two dates, never a link's
// token or digest.

// DefaultAdminEmailBlockSort is the newest block first. The web app mirrors
// the keys in src/types/admin.ts.
const DefaultAdminEmailBlockSort = "-created"

// adminEmailBlockSorts is the sort allowlist; every order ends in the
// primary key.
var adminEmailBlockSorts = map[string]string{
	"created":  "eb.created_at ASC, eb.id ASC",
	"-created": "eb.created_at DESC, eb.id DESC",
	"email":    "eb.email ASC, eb.id ASC",
	"-email":   "eb.email DESC, eb.id DESC",
}

// AdminEmailBlockSortable reports whether key is an allowed sort.
func AdminEmailBlockSortable(key string) bool {
	_, ok := adminEmailBlockSorts[key]
	return ok
}

// AdminEmailBlock is a blocked address as the admin dashboard lists it.
// LinkExpiresAt is when the newest live block link sent to it stops
// working, so its owner can still unblock it themselves; nil when none is
// live (only an admin, or the operator's command line, can then).
type AdminEmailBlock struct {
	ID            int64      `bun:"id" json:"-"`
	Email         string     `bun:"email" json:"email"`
	CreatedAt     time.Time  `bun:"created_at" json:"createdAt"`
	LinkExpiresAt *time.Time `bun:"link_expires_at" json:"linkExpiresAt"`
}

// IndexAdminEmailBlocks returns one page of the block list matching f:
// Search is part of an address, ignoring case, with LIKE's wildcards
// matched literally.
func (s *Store) IndexAdminEmailBlocks(ctx context.Context, f AdminFilter, req PageRequest) (Page[AdminEmailBlock], error) {
	order, ok := adminEmailBlockSorts[f.Sort]
	if !ok {
		order = adminEmailBlockSorts[DefaultAdminEmailBlockSort]
	}
	q := s.DB.NewSelect().Model((*emailBlock)(nil)).
		ColumnExpr("eb.id, eb.email, eb.created_at").
		ColumnExpr("(?) AS link_expires_at", liveBlockLinkExpiry(s.DB)).
		OrderExpr(order)
	if f.Search != "" {
		q = q.Where(`eb.email ILIKE ? ESCAPE '\'`, containsPattern(f.Search))
	}
	return Paginate[AdminEmailBlock](ctx, q, req)
}

// liveBlockLinkExpiry is the subquery giving a blocked address's newest
// live block link's expiry, or null (email_block_links_email_idx).
func liveBlockLinkExpiry(db bun.IDB) *bun.SelectQuery {
	return db.NewSelect().Model((*emailBlockLink)(nil)).
		ColumnExpr("max(ebl.expires_at)").
		Where("ebl.email = eb.email AND ebl.expires_at > now()")
}
