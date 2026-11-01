package httpapi

import (
	"context"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/store"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"time"
)

// IP blocks (docs/api/admin.md#ip-blocks): client addresses and ranges an
// admin blocked, answered 403 before anything else looks at the request.
// Each server keeps the live ones in memory, reloaded every IPBlocksReload
// (one small query), so the check costs a prefix comparison per block, and
// none at all while nothing is blocked. The address is ClientAddress's
// (ClientIP), never a header read here. Never blocked: the health checks,
// and the admin door to a request holding the current key (or the admin
// build's assets pass) from an address ADMIN_ALLOWED_IPS allows, so a
// mistaken block can't lock admins out of undoing it; anything else, a
// right key from outside the allowlist or the previous key included, gets
// the block's answer, so a blocked visitor can't tell the admin door, or a
// key, from anything else. Blocks are for a few abusive addresses: a flood
// is better stopped at the edge (docs/deployment.md).

// IPBlocksReload is how often each server reloads the live IP blocks: a
// new block applies on every server within it.
const IPBlocksReload = 30 * time.Second

// IPBlocked is the API's answer to a blocked address; pages get the same
// words as plain text.
const IPBlocked = "Access from your network has been blocked."

// IPBlocksFirstRetry is how soon a server tries again when it hasn't loaded
// the live blocks yet (the database was down at startup): it doubles on
// each failure, up to IPBlocksReload, so a server never runs long without
// the blocks once the database is back.
const IPBlocksFirstRetry = time.Second

// ipBlocks is a server's copy of the live blocks, when it last loaded them
// and how many requests they refused (the ip_block_refused_total metric,
// once /metrics exists).
type ipBlocks struct {
	live atomic.Pointer[[]liveBlock]
	// loadedAt is the Unix time in nanoseconds of the last load that
	// succeeded; 0 until the first.
	loadedAt atomic.Int64
	refused  atomic.Int64
}

// liveBlock is a live block as this server enforces it.
type liveBlock struct {
	network   netip.Prefix
	expiresAt time.Time
}

// ReloadIPBlocks replaces this server's copy of the live blocks with the
// database's. On an error the old copy stays (an expired block in it stops
// applying at its end regardless); before the first success there is none,
// so nothing is blocked, which RunIPBlocks retries soon to end. A row whose
// network doesn't parse is skipped with an error logged, never the whole
// list.
func (s *Server) ReloadIPBlocks(ctx context.Context) error {
	rows, err := s.Repo.IndexLiveIPBlocks(ctx)
	if err != nil {
		return err
	}
	live := parseLiveBlocks(rows)
	s.ipBlocks.live.Store(&live)
	s.ipBlocks.loadedAt.Store(time.Now().UnixNano())
	return nil
}

// parseLiveBlocks turns the database's live blocks into this server's list,
// skipping (and logging) any whose network doesn't parse, so one bad row
// can't leave every other block unenforced.
func parseLiveBlocks(rows []store.LiveIPBlock) []liveBlock {
	live := make([]liveBlock, 0, len(rows))
	for _, row := range rows {
		network, err := netip.ParsePrefix(row.Network)
		if err != nil {
			slog.Error("an IP block's network doesn't parse; it is skipped", "network", row.Network, "error", err)
			continue
		}
		live = append(live, liveBlock{network: network, expiresAt: row.ExpiresAt})
	}
	return live
}

// IPBlocksLoadedAt is when this server last loaded the live blocks, zero
// if it never has (then nothing is blocked on it).
//
// TODO(metrics): export it on /metrics as the
// ip_blocks_loaded_timestamp_seconds gauge, beside ip_block_refused_total,
// once /metrics exists (at the merge), so an alert can fire on a server
// whose list has gone stale.
func (s *Server) IPBlocksLoadedAt() time.Time {
	at := s.ipBlocks.loadedAt.Load()
	if at == 0 {
		return time.Time{}
	}
	return time.Unix(0, at)
}

// RunIPBlocks reloads the live blocks every interval until ctx ends. Until
// a load has succeeded (the database was down when the server started), it
// retries sooner: after IPBlocksFirstRetry (or every, if shorter), doubling
// up to every. A failure is logged with when the list was last loaded.
// Start it once per server, after a first ReloadIPBlocks.
func (s *Server) RunIPBlocks(ctx context.Context, every time.Duration) {
	runReloads(ctx, !s.IPBlocksLoadedAt().IsZero(), every, IPBlocksFirstRetry, s.reloadIPBlocksLogged)
}

// reloadIPBlocksLogged is one of RunIPBlocks' reloads: it logs a failure
// with when the list was last loaded (or that the server runs without
// one), and the first success after a failure.
func (s *Server) reloadIPBlocksLogged(ctx context.Context) error {
	reload, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := s.ReloadIPBlocks(reload)
	if err != nil {
		if loaded := s.IPBlocksLoadedAt(); loaded.IsZero() {
			slog.Error("loading IP blocks failed; nothing is blocked on this server until it succeeds", "error", err)
		} else {
			slog.Error("reloading IP blocks failed; the previous list still applies", "error", err, "lastLoaded", loaded.UTC().Format(time.RFC3339))
		}
	}
	return err
}

// runReloads calls reload every `every` until ctx ends. While loaded is
// false (no reload has succeeded yet) it waits less: first (or every, if
// shorter), doubling after each failure up to every. A failure after a
// success waits the full interval, as the old list still applies. The
// first success after failures is logged.
func runReloads(ctx context.Context, loaded bool, every, first time.Duration, reload func(context.Context) error) {
	wait := every
	if !loaded {
		wait = min(first, every)
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	failing := !loaded
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		err := reload(ctx)
		switch {
		case err == nil:
			if failing {
				slog.Info("IP blocks loaded")
			}
			loaded, failing, wait = true, false, every
		case !loaded:
			failing, wait = true, min(wait*2, every)
		default:
			failing, wait = true, every
		}
		timer.Reset(wait)
	}
}

// IPBlockRefusals is how many requests this server refused for a blocked
// address.
func (s *Server) IPBlockRefusals() int64 {
	return s.ipBlocks.refused.Load()
}

// addressBlocked reports whether ip (ClientIP) is inside a live block.
func (s *Server) addressBlocked(ip string) bool {
	live := s.ipBlocks.live.Load()
	if live == nil || len(*live) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	addr, now := addr.Unmap().WithZone(""), time.Now()
	for _, block := range *live {
		if now.Before(block.expiresAt) && block.network.Contains(addr) {
			return true
		}
	}
	return false
}

// ipBlockExempt reports whether r is never blocked: the health checks, and
// the admin door to a request that would get through it: the current key
// (never the previous one) or a valid assets pass, from an address
// ADMIN_ALLOWED_IPS allows (adminAddressAllowed, which records a refusal as
// the door does). Anything else, a right key from outside the allowlist
// or the previous key included, gets the block's answer, exactly as a
// wrong key does, so a blocked visitor learns nothing about the keys
// (access-hardening's "looks exactly like a wrong key"). A session under
// the previous key is blocked like any other until it moves to the current
// key.
func (s *Server) ipBlockExempt(r *http.Request) bool {
	path := r.URL.Path
	switch {
	case path == "/api/health" || path == "/api/health/live":
		return true
	case strings.HasPrefix(path, "/api/admin/"):
		return s.adminDoorOpens(r, r.Header.Get("X-Admin-Key"))
	case strings.HasPrefix(path, "/admin-assets/"):
		return s.adminAssetsPassValid(r) && s.adminAddressAllowed(r)
	case strings.HasPrefix(path, "/admin/"):
		key, _, _ := strings.Cut(strings.TrimPrefix(path, "/admin/"), "/")
		return s.adminDoorOpens(r, key)
	}
	return false
}

// adminDoorOpens reports whether key is the current admin key and r comes
// from an address the admin door allows. The address is checked only once
// the key matched, as at the door, so a wrong key records nothing.
func (s *Server) adminDoorOpens(r *http.Request, key string) bool {
	return s.matchAdminKey(key) == auth.AdminKeyCurrent && s.adminAddressAllowed(r)
}

// refuseBlockedAddress answers a request from a blocked address with 403,
// and reports whether it did: JSON for the API, a plain page otherwise.
// Its access line's route is "ip blocked".
func (s *Server) refuseBlockedAddress(w http.ResponseWriter, r *http.Request, ip string) bool {
	if !s.addressBlocked(ip) || s.ipBlockExempt(r) {
		return false
	}
	s.ipBlocks.refused.Add(1)
	observe.SetRoute(r.Context(), "ip blocked")
	if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		apiHeaders(w.Header())
		Fail(w, 403, IPBlocked)
		return true
	}
	apiHeaders(w.Header())
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(403)
	_, _ = w.Write([]byte(IPBlocked + "\n"))
	return true
}
