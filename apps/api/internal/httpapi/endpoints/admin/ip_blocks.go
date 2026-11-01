package admin

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// IPBlocks are the admin dashboard's /api/admin/ip-blocks
// (docs/api/admin.md#ip-blocks): client addresses and ranges every server
// answers 403 (httpapi's ip_blocks.go), each for at most 30 days.
type IPBlocks struct{ *httpapi.Server }

// ipBlockLifetimes are the lengths a block may have, by the name the API
// takes: every block ends, after 30 days at most (store.MaxIPBlockLifetime).
var ipBlockLifetimes = map[string]time.Duration{
	"1h":  time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": store.MaxIPBlockLifetime,
}

// Narrowest prefixes a block may have: at most a /16 of IPv4 (65,536
// addresses) or a /32 of IPv6 (an ISP's allocation), as the Events IP filter.
const (
	minIPv4Bits = 16
	minIPv6Bits = 32
)

// Index lists the live blocks, newest first, recorded as
// admin.ip_block.listed. GET /api/admin/ip-blocks.
func (h IPBlocks) Index(w http.ResponseWriter, r *http.Request) {
	if _, ok := input.Query(r); !ok {
		httpapi.Fail(w, 400, "Invalid query parameters.")
		return
	}
	blocks, err := h.Repo.IndexIPBlocks(r.Context())
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	if err := h.Repo.Record(r.Context(), events.AdminIPBlockListed{Admin: httpapi.Admin(r).User.PublicID}, store.EventScope{}); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"data": blocks})
}

// blockInput is Store's body: the address or CIDR range, how long
// (ipBlockLifetimes), and why.
type blockInput struct {
	Network   string `json:"network"`
	ExpiresIn string `json:"expiresIn"`
	Reason    string `json:"reason"`
}

// parseNetwork reads an address (a /32 or /128) or a CIDR range, written
// as its first address (192.0.2.0/24, not 192.0.2.7/24), and returns it,
// or why it can't be blocked.
func parseNetwork(raw string) (netip.Prefix, string) {
	raw = strings.TrimSpace(raw)
	var network netip.Prefix
	if strings.Contains(raw, "/") {
		p, err := netip.ParsePrefix(raw)
		if err != nil {
			return network, "Enter an address, or a range such as 192.0.2.0/24."
		}
		if p != p.Masked() {
			return network, "Write the range from its first address: " + p.Masked().String() + "."
		}
		network = p
	} else {
		addr, err := netip.ParseAddr(raw)
		if err != nil || addr.Zone() != "" {
			return network, "Enter an address, or a range such as 192.0.2.0/24."
		}
		network = netip.PrefixFrom(addr, addr.BitLen())
	}
	switch {
	case network.Addr().Is4In6():
		return network, "Write an IPv4 address as IPv4 (192.0.2.7), not mapped into IPv6."
	case network.Addr().Is4() && network.Bits() < minIPv4Bits:
		return network, "A range can be at most a /16 for IPv4."
	case network.Addr().Is6() && network.Bits() < minIPv6Bits:
		return network, "A range can be at most a /32 for IPv6."
	}
	return network, ""
}

// refusal says why the admin may not block network, or "": never a range
// holding their own address (they'd lock themselves out of the app, though
// not the dashboard) or overlapping one of TRUSTED_PROXIES (every request
// through the proxy would be refused).
func (h IPBlocks) refusal(r *http.Request, network netip.Prefix) string {
	if own, err := netip.ParseAddr(httpapi.ClientIP(r)); err == nil && network.Contains(own.Unmap().WithZone("")) {
		return "That includes your own address."
	}
	for _, proxy := range h.Config.TrustedProxies {
		if network.Overlaps(proxy) {
			return "That overlaps a trusted proxy (TRUSTED_PROXIES): it would block everyone behind it."
		}
	}
	return ""
}

// Store blocks an address or range for 1 hour to 30 days, recording
// admin.ip.blocked, and applies it on this server at once (every other
// within httpapi.IPBlocksReload). 201 with the block; 422 on network or
// expiresIn; 409 when it's already blocked or the list is full.
// POST /api/admin/ip-blocks.
func (h IPBlocks) Store(w http.ResponseWriter, r *http.Request) {
	var in blockInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	network, problem := parseNetwork(in.Network)
	if problem == "" {
		problem = h.refusal(r, network)
	}
	if problem != "" {
		httpapi.FailField(w, 422, "network", problem)
		return
	}
	lifetime, ok := ipBlockLifetimes[in.ExpiresIn]
	if !ok {
		httpapi.FailField(w, 422, "expiresIn", "Pick how long the block lasts.")
		return
	}
	action, ok := adminAction(w, r, in.Reason)
	if !ok {
		return
	}
	block, err := h.Repo.CreateIPBlock(r.Context(), store.NewIPBlock{Network: network, For: lifetime}, action)
	switch {
	case errors.Is(err, store.ErrIPBlockExists):
		httpapi.FailField(w, 409, "network", "That network is already blocked.")
	case errors.Is(err, store.ErrTooManyIPBlocks):
		httpapi.Fail(w, 409, "1,000 blocks are live: unblock some, or block at the edge instead.")
	case err != nil:
		httpapi.InternalError(w, r, err)
	default:
		h.reload(r)
		httpapi.JSON(w, 201, block)
	}
}

// Destroy lifts a live block before it ends, recording admin.ip.unblocked,
// and applies it on this server at once. 404 when there's none (it may have
// expired). DELETE /api/admin/ip-blocks/{block}.
func (h IPBlocks) Destroy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("block")
	if !input.PublicID("ipb", id) {
		httpapi.Fail(w, 404, "Block not found.")
		return
	}
	var in destroyInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	action, ok := adminAction(w, r, in.Reason)
	if !ok {
		return
	}
	err := h.Repo.DeleteIPBlock(r.Context(), id, action)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		httpapi.Fail(w, 404, "Block not found.")
	case err != nil:
		httpapi.InternalError(w, r, err)
	default:
		h.reload(r)
		w.WriteHeader(204)
	}
}

// reload applies a change on this server now; the others reload within
// httpapi.IPBlocksReload. A failure is only logged: the change is saved.
func (h IPBlocks) reload(r *http.Request) {
	if err := h.ReloadIPBlocks(r.Context()); err != nil {
		slog.ErrorContext(r.Context(), "reloading IP blocks after a change failed", "error", err)
	}
}
