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

// ipBlocks is a server's copy of the live blocks, and how many requests
// they refused (the ip_block_refused_total metric, once /metrics exists).
type ipBlocks struct {
	live    atomic.Pointer[[]store.LiveIPBlock]
	refused atomic.Int64
}

// ReloadIPBlocks replaces this server's copy of the live blocks with the
// database's. On an error the old copy stays (an expired block in it stops
// applying at its end regardless).
func (s *Server) ReloadIPBlocks(ctx context.Context) error {
	live, err := s.Repo.IndexLiveIPBlocks(ctx)
	if err != nil {
		return err
	}
	s.ipBlocks.live.Store(&live)
	return nil
}

// RunIPBlocks reloads the live blocks every interval until ctx ends, logging
// a failed reload. Start it once per server, after a first ReloadIPBlocks.
func (s *Server) RunIPBlocks(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reload, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := s.ReloadIPBlocks(reload); err != nil {
				slog.Error("reloading IP blocks failed; the previous list still applies", "error", err)
			}
			cancel()
		}
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
		if now.Before(block.ExpiresAt) && block.Network.Contains(addr) {
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
