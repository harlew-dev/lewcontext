package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/events"
	"net/netip"
	"time"
)

// Client addresses and ranges an admin blocked (docs/api/admin.md#ip-blocks).
// Every server keeps the live ones in memory (httpapi's IP blocks,
// reloaded from IndexLiveIPBlocks every 30 seconds) and answers their
// requests 403. Each expires within MaxIPBlockLifetime and is at most a /16
// (IPv4) or /32 (IPv6) wide (the table's CHECKs; the controller refuses
// wider ones and ranges holding the admin's own address or a trusted
// proxy). The cleanup deletes expired rows.

// MaxIPBlockLifetime is how long a block may last, 30 days: addresses are
// reassigned and shared, so a permanent block eventually hits the wrong
// people. Counted in hours, as the table's CHECK is (interval '720 hours'),
// never calendar days, which are 23 or 25 hours across a DST change in the
// database session's TimeZone. CreateIPBlock adds it in seconds.
const MaxIPBlockLifetime = 720 * time.Hour

// MaxIPBlocks is how many blocks may be live at once, so the list every
// server holds and checks stays small; for more, block at the edge
// (docs/deployment.md).
const MaxIPBlocks = 1000

var (
	// ErrIPBlockExists: the network is already blocked.
	ErrIPBlockExists = errors.New("the network is already blocked")
	// ErrTooManyIPBlocks: MaxIPBlocks are live.
	ErrTooManyIPBlocks = errors.New("too many IP blocks")
)

type ipBlockRecord struct {
	bun.BaseModel `bun:"table:ip_blocks,alias:ipb"`
	ID            int64     `bun:",pk,autoincrement"`
	PublicID      string    `bun:",nullzero"`
	Network       string    `bun:",type:cidr"`
	Reason        string    `bun:",notnull"`
	CreatedBy     int64     `bun:",nullzero"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	ExpiresAt     time.Time `bun:",notnull"`
}

// IPBlock is a live block as the admin dashboard lists it: its network as
// CIDR, why, who blocked it (nil once that account is gone) and when it
// ends.
type IPBlock struct {
	PublicID  string       `json:"id"`
	Network   string       `json:"network"`
	Reason    string       `json:"reason"`
	CreatedBy *AdminPerson `json:"createdBy"`
	CreatedAt time.Time    `json:"createdAt"`
	ExpiresAt time.Time    `json:"expiresAt"`
}

// LiveIPBlock is a live block as a server loads it to enforce: its network
// as CIDR text, which the server parses (skipping one that doesn't), and
// its end.
type LiveIPBlock struct {
	Network   string    `bun:"network"`
	ExpiresAt time.Time `bun:"expires_at"`
}

// NewIPBlock is a block to create: the network (already masked and
// checked) and how long it lasts, at most MaxIPBlockLifetime.
type NewIPBlock struct {
	Network netip.Prefix
	For     time.Duration
}

// CreateIPBlock blocks n.Network until n.For from now (the database's
// clock), recording admin.ip.blocked. An expired block of the same network
// not yet cleaned up is replaced. ErrIPBlockExists when it's already
// blocked, ErrTooManyIPBlocks past MaxIPBlocks: the live ones are counted
// under the IP blocks' advisory lock (database.LockIPBlocks), so blocks
// made at once can't take the list past it.
func (s *Store) CreateIPBlock(ctx context.Context, n NewIPBlock, a AdminAction) (IPBlock, error) {
	var block IPBlock
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.LockIPBlocks(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model((*ipBlockRecord)(nil)).Where("network = ? AND expires_at <= now()", n.Network.String()).Exec(ctx); err != nil {
			return err
		}
		live, err := tx.NewSelect().Model((*ipBlockRecord)(nil)).Where("expires_at > now()").Count(ctx)
		if err != nil {
			return err
		}
		if live >= MaxIPBlocks {
			return ErrTooManyIPBlocks
		}
		row := ipBlockRecord{Network: n.Network.String(), Reason: a.Reason, CreatedBy: a.Admin.ID}
		_, err = tx.NewInsert().Model(&row).
			Value("expires_at", "now() + make_interval(secs => ?)", int(n.For.Seconds())).
			On("CONFLICT (network) DO NOTHING").
			Returning("id, public_id, created_at, expires_at").
			Exec(ctx)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && row.ID == 0) {
			return ErrIPBlockExists
		}
		if err != nil {
			return err
		}
		block = IPBlock{PublicID: row.PublicID, Network: row.Network, Reason: row.Reason, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt,
			CreatedBy: &AdminPerson{PublicID: a.Admin.PublicID, Name: a.Admin.Name, Email: a.Admin.Email}}
		e := events.AdminIPBlocked{Admin: a.Admin.PublicID, Block: row.PublicID, Network: row.Network, ExpiresAt: row.ExpiresAt, Reason: a.Reason}
		return s.record(ctx, tx, e, EventScope{ActorID: a.Admin.ID})
	})
	return block, err
}

// DeleteIPBlock lifts the live block publicID before it expires, recording
// admin.ip.unblocked. sql.ErrNoRows when there is none (or it expired).
func (s *Store) DeleteIPBlock(ctx context.Context, publicID string, a AdminAction) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var row ipBlockRecord
		err := tx.NewDelete().Model(&row).
			Where("public_id = ? AND expires_at > now()", publicID).
			Returning("public_id, host(network) || '/' || masklen(network) AS network").
			Scan(ctx)
		if err != nil {
			return err
		}
		e := events.AdminIPUnblocked{Admin: a.Admin.PublicID, Block: row.PublicID, Network: row.Network, Reason: a.Reason}
		return s.record(ctx, tx, e, EventScope{ActorID: a.Admin.ID})
	})
}

// IndexIPBlocks lists the live blocks, the newest first (at most
// MaxIPBlocks, so the whole list).
func (s *Store) IndexIPBlocks(ctx context.Context) ([]IPBlock, error) {
	var rows []struct {
		PublicID  string         `bun:"public_id"`
		Network   string         `bun:"network"`
		Reason    string         `bun:"reason"`
		CreatedAt time.Time      `bun:"created_at"`
		ExpiresAt time.Time      `bun:"expires_at"`
		ByID      sql.NullString `bun:"by_id"`
		ByName    sql.NullString `bun:"by_name"`
		ByEmail   sql.NullString `bun:"by_email"`
	}
	err := s.DB.NewSelect().Model((*ipBlockRecord)(nil)).
		ColumnExpr("ipb.public_id, host(ipb.network) || '/' || masklen(ipb.network) AS network, ipb.reason, ipb.created_at, ipb.expires_at").
		ColumnExpr("u.public_id AS by_id, u.name AS by_name, u.email AS by_email").
		Join("LEFT JOIN users AS u ON u.id = ipb.created_by").
		Where("ipb.expires_at > now()").
		OrderExpr("ipb.created_at DESC, ipb.id DESC").
		Limit(MaxIPBlocks).
		Scan(ctx, &rows)
	blocks := make([]IPBlock, len(rows))
	for i, row := range rows {
		blocks[i] = IPBlock{PublicID: row.PublicID, Network: row.Network, Reason: row.Reason, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt}
		if row.ByID.Valid {
			blocks[i].CreatedBy = &AdminPerson{PublicID: row.ByID.String, Name: row.ByName.String, Email: row.ByEmail.String}
		}
	}
	return blocks, err
}

// IndexLiveIPBlocks lists every live block's network and end, for a
// server's in-memory list: one small query, every 30 seconds. At most
// MaxIPBlocks, the oldest first, so if more were ever live (inserted
// around CreateIPBlock) every server enforces the same ones.
func (s *Store) IndexLiveIPBlocks(ctx context.Context) ([]LiveIPBlock, error) {
	var live []LiveIPBlock
	err := s.DB.NewSelect().Model((*ipBlockRecord)(nil)).
		ColumnExpr("host(ipb.network) || '/' || masklen(ipb.network) AS network, ipb.expires_at").
		Where("ipb.expires_at > now()").
		OrderExpr("ipb.created_at, ipb.id").
		Limit(MaxIPBlocks).
		Scan(ctx, &live)
	return live, err
}
