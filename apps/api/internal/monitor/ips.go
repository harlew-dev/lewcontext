package monitor

import (
	"context"
	"lewcontext/api/internal/store"
	"net/netip"
	"time"
)

// The IP addresses page (docs/api/monitoring.md#ip-addresses): which
// addresses are noisy or abusive, and what did one address do?

// PageIPs is the IP addresses page's name.
const PageIPs = "ips"

// Sign-up clusters' critical level: this many accounts from one address
// in a day is a spam farm, not a shared office.
const signUpClusterCritical = 20

// IPsSignals are the IP addresses page's signals.
var IPsSignals = []Signal{
	{
		// The usual shape of a spam farm.
		Name: "ips.sign_up_cluster", Page: PageIPs, Label: "Most accounts from one address", Unit: UnitCount,
		Window: 24 * time.Hour, Warn: store.SignUpCluster, Critical: signUpClusterCritical,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(env.Store.CountLargestSignUpCluster(ctx, sp.whole()))
		},
		Drill: drill("auth.registered"),
	},
	{
		// One address holding most of the traffic: the proxy's client IP
		// probably isn't configured (CLIENT_IP_HEADER, TRUSTED_PROXIES),
		// so every number by address is wrong.
		Name: "ips.busiest_share", Page: PageIPs, Label: "Events from the busiest address", Unit: UnitPercent,
		Window: 24 * time.Hour, Warn: 80, MinSample: 50,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			b, err := env.Store.FindBusiestAddress(ctx, sp.From, sp.To)
			return share(b.Busiest, b.Events), err
		},
	},
}

// ipsSections are the IP addresses page's lists, over its window.
type ipsSections struct {
	// Addresses are the top addresses both ways: Network rows group an
	// IPv6 address by its /64, the others list each address.
	Addresses []store.AddressRow       `json:"addresses"`
	Clusters  []store.SignUpClusterRow `json:"clusters"`
	Blocks    ipBlocksNow              `json:"blocks"`
}

// ipBlocksNow is how many IP blocks (the admin actions', the IP blocks
// page's) are live: every server refuses their requests. A count, so
// reading it isn't the IP blocks page's recorded read.
type ipBlocksNow struct {
	Live int `json:"live"`
}

// ipsPage reads the IP addresses page's lists over w: sign-up clusters by
// the day, counted back from the window's end.
func ipsPage(ctx context.Context, env Env, w Window) (Sections, error) {
	out := &ipsSections{}
	return readSections(ctx, env, out, []section{
		{"addresses", func(ctx context.Context) (err error) {
			out.Addresses, err = env.Store.IndexAddressActivity(ctx, w.From, w.To)
			return
		}},
		{"clusters", func(ctx context.Context) (err error) {
			out.Clusters, err = env.Store.IndexSignUpClusters(ctx, store.MonitorSpan{From: w.From, To: w.To, Per: 24 * time.Hour, Origin: w.To})
			return
		}},
		{"blocks", func(ctx context.Context) error {
			live, err := env.Store.IndexLiveIPBlocks(ctx)
			out.Blocks.Live = len(live)
			return err
		}},
	})
}

// ParseAddress reads one address's page's address: an IP address (IPv4,
// or IPv6 without a zone; an IPv4-mapped IPv6 address is the IPv4 one), or
// an IPv6 /64 as CIDR. It returns it as PostgreSQL's inet reads it, and for
// an IPv6 address its /64, which its page offers. False for anything else.
func ParseAddress(raw string) (ip, network string, ok bool) {
	if addr, err := netip.ParseAddr(raw); err == nil {
		if addr.Zone() != "" {
			return "", "", false
		}
		addr = addr.Unmap()
		if addr.Is6() {
			network = netip.PrefixFrom(addr, 64).Masked().String()
		}
		return addr.String(), network, true
	}
	prefix, err := netip.ParsePrefix(raw)
	if err != nil || !prefix.Addr().Is6() || prefix.Addr().Is4In6() || prefix.Addr().Zone() != "" || prefix.Bits() != 64 {
		return "", "", false
	}
	return prefix.Masked().String(), "", true
}

// AddressView is one address's page: the address (or /64) looked at, an
// IPv6 address's /64, the window, what it did in it, and the live IP
// blocks covering it.
type AddressView struct {
	IP      string         `json:"ip"`
	Network string         `json:"network,omitempty"`
	Window  WindowEcho     `json:"window"`
	Blocks  []AddressBlock `json:"blocks"`
	store.AddressActivity
}

// AddressBlock is a live IP block covering one address's page's address,
// or overlapping its /64: requests from it are refused until ExpiresAt.
type AddressBlock struct {
	Network   string    `json:"network"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// ShowAddress reads what ip (from ParseAddress) did over w, and the live
// blocks covering it.
func ShowAddress(ctx context.Context, env Env, ip, network string, w Window) (AddressView, error) {
	activity, err := env.Store.FindAddressActivity(ctx, ip, w.From, w.To)
	if err != nil {
		return AddressView{}, err
	}
	live, err := env.Store.IndexLiveIPBlocks(ctx)
	if err != nil {
		return AddressView{}, err
	}
	return AddressView{IP: ip, Network: network, Window: w.Echo(), Blocks: blocksCovering(live, ip), AddressActivity: activity}, nil
}

// blocksCovering is the live blocks holding ip (an address, or an IPv6 /64
// as ParseAddress gives it), or holding part of it; a block whose network
// doesn't parse is skipped, as the servers skip it.
func blocksCovering(live []store.LiveIPBlock, ip string) []AddressBlock {
	out := []AddressBlock{}
	looked, err := netip.ParsePrefix(ip)
	if err != nil {
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			return out
		}
		looked = netip.PrefixFrom(addr, addr.BitLen())
	}
	for _, b := range live {
		network, err := netip.ParsePrefix(b.Network)
		if err == nil && network.Overlaps(looked) {
			out = append(out, AddressBlock{Network: network.String(), ExpiresAt: b.ExpiresAt})
		}
	}
	return out
}
