package store

import (
	"encoding/json"
	"testing"
)

// Minimising cuts the addresses a payload holds by its event's name: what
// one address's page looked at (a host or an IPv6 /64) and the networks an
// admin session moved between, to an IPv4 /24 or IPv6 /48; the same field
// names in other events, and a change's from and to, are left alone.
func TestMinimisePayloadNetworks(t *testing.T) {
	pseudonym := func(string) string { return "hmac:x" }
	cases := []struct {
		name, payload, want string
	}{
		{"admin.ip.viewed", `{"address":"203.0.113.66","window":"1h"}`, `{"address":"203.0.113.0/24","window":"1h"}`},
		{"admin.ip.viewed", `{"address":"2001:db8:1:2::9","window":"7d"}`, `{"address":"2001:db8:1::/48","window":"7d"}`},
		{"admin.ip.viewed", `{"address":"2001:db8:1:2::/64","window":"7d"}`, `{"address":"2001:db8:1::/48","window":"7d"}`},
		{"admin.ip.viewed", `{"address":"::ffff:203.0.113.66","window":"1h"}`, `{"address":"203.0.113.0/24","window":"1h"}`},
		{"admin.session.moved", `{"from":"198.51.100.0/24","to":"2001:db8:5::/48","sessionAge":5}`, `{"from":"198.51.100.0/24","sessionAge":5,"to":"2001:db8:5::/48"}`},
		{"admin.session.moved", `{"from":"198.51.100.9","to":"2001:db8:5:6::/64","sessionAge":5}`, `{"from":"198.51.100.0/24","sessionAge":5,"to":"2001:db8:5::/48"}`},
		{"workspace.updated", `{"changes":{"name":{"from":"203.0.113.66","to":"b"}}}`, `{"changes":{"name":{"from":"203.0.113.66","to":"b"}}}`},
		{"admin.monitoring.viewed", `{"address":"203.0.113.66"}`, `{"address":"203.0.113.66"}`},
	}
	for _, c := range cases {
		got, err := minimisePayload(c.name, json.RawMessage(c.payload), pseudonym)
		if err != nil || string(got) != c.want {
			t.Errorf("%s %s: %s %v, want %s", c.name, c.payload, got, err, c.want)
		}
	}
}
