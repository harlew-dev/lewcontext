package admin

import (
	"context"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/monitor"
	"lewcontext/api/internal/store"
	"net/http"
)

// IPAddresses is one address's page on the Monitoring pages,
// /api/admin/ips/{ip} (docs/api/monitoring.md#one-address): what an
// address, or an IPv6 /64, did over a window. Read within the Monitoring
// pages' bounds, and recorded as admin.ip.viewed with the address looked
// at: looking up an address is following someone.
type IPAddresses struct{ *httpapi.Server }

// Show reads one address over ?window=1h|24h|7d (24h by default). The
// address is an IP address or an IPv6 /64 (its slash escaped, %2F); any
// other is 404.
func (h IPAddresses) Show(w http.ResponseWriter, r *http.Request) {
	ip, network, ok := monitor.ParseAddress(r.PathValue("ip"))
	if !ok {
		httpapi.Fail(w, 404, "Address not found.")
		return
	}
	window, now, ok := monitoringWindow(w, r)
	if !ok {
		return
	}
	admin := httpapi.Admin(r).User.PublicID
	env := monitor.Env{Store: h.Repo, Limits: httpapi.RateLimits, Now: now}
	view, err := bounded(r.Context(), h.Server, admin, func(ctx context.Context) (monitor.AddressView, error) {
		return monitor.ShowAddress(ctx, env, ip, network, window)
	})
	if failBounded(w, r, err) {
		return
	}
	e := events.AdminIPViewed{Admin: admin, Address: ip, Window: window.Name}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 200, view)
}
