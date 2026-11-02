package admin_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// blockedAPI is the API's answer to a blocked address.
const blockedAPI = `{"error":"Access from your network has been blocked."}`

// from sends a request to h as if from ip, with the admin key when key is
// set (X-Admin-Key) and the cookies given.
func from(h http.Handler, ip, method, target, key string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.RemoteAddr = netip.AddrPortFrom(netip.MustParseAddr(ip), 1234).String()
	r.Header.Set("User-Agent", apitest.UserAgent)
	if key != "" {
		r.Header.Set("X-Admin-Key", key)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Blocking an address or a range: the list, each read recorded; what may
// be blocked (an address or a range of at most a /16 or /32, written from
// its first address, never your own address nor a trusted proxy's, for 1
// hour to 30 days); 409 for one already blocked; unblocking; and the
// events.
func TestAdminIPBlocks(t *testing.T) {
	_, db := server(t)
	s := serverWith(t, db, func(c *config.Config) {
		c.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.1.2.0/24")}
	})
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	block := func(network, expiresIn string, want int) *httptest.ResponseRecorder {
		t.Helper()
		return adminCall(t, h, "POST", "/api/admin/ip-blocks", reasonBody(reason, "network", network, "expiresIn", expiresIn), session.CSRFToken, want, session.Cookie())
	}
	refused := map[string]string{
		"not an address":     "Enter an address",
		"203.0.113.7/24":     "203.0.113.0/24",
		"203.0.0.0/15":       "at most a /16",
		"2001:db8::/31":      "at most a /32",
		"::ffff:203.0.113.7": "not mapped",
		"fe80::1%eth0":       "Enter an address",
		"127.0.0.0/16":       "your own address",
		"127.0.0.1":          "your own address",
		"10.1.0.0/16":        "trusted proxy",
		"10.1.2.3":           "trusted proxy",
	}
	for network, message := range refused {
		if w := block(network, "24h", 422); apitest.ErrorField(t, w) != "network" || !strings.Contains(w.Body.String(), message) {
			t.Errorf("%s: %s", network, w.Body.String())
		}
	}
	for _, expiresIn := range []string{"", "31d", "forever", "0h"} {
		if w := block("203.0.113.7", expiresIn, 422); apitest.ErrorField(t, w) != "expiresIn" {
			t.Errorf("expiresIn %q: %s", expiresIn, w.Body.String())
		}
	}
	if len(apitest.EventsNamed(t, db, "admin.ip.blocked")) != 0 {
		t.Fatal("a refusal was recorded")
	}

	// An address, a /16 and an IPv6 /64: 201 with the block.
	single := decodeAdmin[store.IPBlock](t, block(" 203.0.113.7 ", "1h", 201).Body.Bytes())
	if single.Network != "203.0.113.7/32" || single.CreatedBy == nil || single.CreatedBy.PublicID != operator.User.PublicID || single.ExpiresAt.Sub(single.CreatedAt) != time.Hour {
		t.Fatalf("the block: %+v", single)
	}
	block("198.51.0.0/16", "30d", 201)
	block("2001:db8:1:2::/64", "7d", 201)
	if w := block("203.0.113.7/32", "24h", 409); !strings.Contains(w.Body.String(), "already blocked") {
		t.Fatalf("again: %s", w.Body.String())
	}
	e := apitest.EventsNamed(t, db, "admin.ip.blocked")[0]
	if e.Payload["network"] != "203.0.113.7/32" || e.Payload["block"] != single.PublicID || e.Payload["reason"] != reason || e.Payload["expiresAt"] == nil {
		t.Fatalf("admin.ip.blocked: %+v", e.Payload)
	}

	// The list: newest first, recorded once per read, no expired block.
	apitest.Must(factory.IPBlocks.State(factory.BlockOf("192.0.2.0/24")).State(factory.ExpiredBlock).CreateOne(ctx, db, f))(t)
	list := decodeAdmin[struct{ Data []store.IPBlock }](t, adminCall(t, h, "GET", "/api/admin/ip-blocks", "", "", 200, session.Cookie()).Body.Bytes())
	if len(list.Data) != 3 || list.Data[0].Network != "2001:db8:1:2::/64" || list.Data[2].Reason != reason {
		t.Fatalf("the list: %+v", list.Data)
	}
	adminCall(t, h, "GET", "/api/admin/ip-blocks?page=2", "", "", 400, session.Cookie())
	if read := apitest.OnlyEvent(t, db, "admin.ip_block.listed"); read.Payload["admin"] != operator.User.PublicID || read.UserID.Valid {
		t.Fatalf("admin.ip_block.listed: %+v", read)
	}

	// Unblocking: 204, recorded; then 404, and 404 for an expired one.
	adminCall(t, h, "DELETE", "/api/admin/ip-blocks/"+single.PublicID, reasonBody(reason), session.CSRFToken, 204, session.Cookie())
	adminCall(t, h, "DELETE", "/api/admin/ip-blocks/"+single.PublicID, reasonBody(reason), session.CSRFToken, 404, session.Cookie())
	adminCall(t, h, "DELETE", "/api/admin/ip-blocks/not-a-block", reasonBody(reason), session.CSRFToken, 404, session.Cookie())
	if e := apitest.OnlyEvent(t, db, "admin.ip.unblocked"); e.Payload["network"] != "203.0.113.7/32" || e.Payload["block"] != single.PublicID {
		t.Fatalf("admin.ip.unblocked: %+v", e.Payload)
	}
	// The cleanup deletes expired blocks.
	apitest.Must(0, s.Repo.Cleanup(ctx, s.EmailPseudonym))(t)
	if n := apitest.Must(db.NewSelect().TableExpr("ip_blocks").Count(ctx))(t); n != 2 {
		t.Fatalf("blocks after the cleanup: %d", n)
	}
	apitest.CheckPayloadIDs(t, db)
}

// A blocked address gets 403 on the API (JSON) and on pages (plain), and
// so does an /admin URL without the key: a blocked visitor can't tell the
// admin door from any other URL. The health checks, and the admin door
// with the current key or the assets pass from an address the allowlist
// allows, are never blocked, so a mistaken block can't lock admins out of
// undoing it; a right key from outside the allowlist, or the previous key,
// gets exactly what a wrong key gets. The
// block applies on the server that made it at once, on every other within
// a reload, and ends at its expiry; an IPv6 range covers its addresses.
func TestIPBlocksAreEnforced(t *testing.T) {
	_, db := server(t)
	s := serverWith(t, db, func(*config.Config) {})
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	_, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	user := apitest.SignedIn(t, db, f)
	const blocked, free = "198.51.100.23", "198.51.101.1"
	// Before: everything as usual from the address.
	if w := from(h, blocked, "GET", "/api/auth/me", "", user.Cookie()); w.Code != 200 {
		t.Fatalf("before the block: %d", w.Code)
	}
	adminCall(t, h, "POST", "/api/admin/ip-blocks", reasonBody(reason, "network", "198.51.100.0/24", "expiresIn", "1h"), session.CSRFToken, 201, session.Cookie())
	before := s.IPBlockRefusals()
	beforeMetric, _ := strconv.Atoi(apitest.Metric(t, "ip_block_refused_total"))
	// The server loaded the list as it made the block: /metrics says when.
	if loaded, _ := strconv.ParseFloat(apitest.Metric(t, "ip_blocks_loaded_timestamp_seconds"), 64); time.Since(time.Unix(int64(loaded), 0)) > time.Minute {
		t.Fatalf("ip_blocks_loaded_timestamp_seconds: %v", loaded)
	}

	// The API: 403 JSON, signed in or not, whatever the route.
	for _, target := range []string{"/api/auth/me", "/api/workspaces", "/api/nowhere", "/api"} {
		w := from(h, blocked, "GET", target, "", user.Cookie())
		if w.Code != 403 || strings.TrimSpace(w.Body.String()) != blockedAPI || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %s %v", target, w.Code, w.Body.String(), w.Header())
		}
	}
	// Pages, and the admin door without its key: the same plain 403.
	nowhere := from(h, blocked, "GET", "/nowhere", "")
	if nowhere.Code != 403 || !strings.Contains(nowhere.Body.String(), "Access from your network has been blocked.") {
		t.Fatalf("a page: %d %s", nowhere.Code, nowhere.Body.String())
	}
	for _, target := range []string{"/", "/app/settings", "/admin", "/admin/wrong-key", "/admin/wrong-key/users", "/admin-assets/x.js"} {
		sameAnswer(t, target, from(h, blocked, "GET", target, ""), nowhere)
	}
	sameAnswer(t, "an admin API call without the key", from(h, blocked, "GET", "/api/admin/me", "", session.Cookie()), from(h, blocked, "GET", "/api/nowhere", ""))
	sameAnswer(t, "an admin API call with a wrong key", from(h, blocked, "GET", "/api/admin/me", "wrong-key", session.Cookie()), from(h, blocked, "GET", "/api/nowhere", ""))
	// Never blocked: health, and the admin door with the key.
	for _, target := range []string{"/api/health", "/api/health/live"} {
		if w := from(h, blocked, "GET", target, ""); w.Code != 200 {
			t.Fatalf("%s: %d", target, w.Code)
		}
	}
	if w := from(h, blocked, "GET", "/api/admin/me", factory.AdminRouteKey, session.Cookie()); w.Code != 200 {
		t.Fatalf("the admin API with the key: %d %s", w.Code, w.Body.String())
	}
	page := from(h, blocked, "GET", "/admin/"+factory.AdminRouteKey+"/users", "", session.Cookie())
	if page.Code != 200 {
		t.Fatalf("an admin page with the key: %d", page.Code)
	}
	pass := apitest.CookieNamed(page, "lewcontext_admin_assets")
	if w := from(h, blocked, "GET", "/admin-assets/x.js", "", pass); w.Code == 403 {
		t.Fatalf("an admin file with the pass was blocked")
	}
	if w := from(h, blocked, "GET", "/admin/"+factory.AdminRouteKey, ""); w.Code != 200 {
		t.Fatalf("the admin sign-in page with the key: %d", w.Code)
	}
	// Other addresses: as usual.
	if w := from(h, free, "GET", "/api/auth/me", "", user.Cookie()); w.Code != 200 {
		t.Fatalf("another address: %d", w.Code)
	}
	if s.IPBlockRefusals() <= before {
		t.Fatal("refusals weren't counted")
	}
	// ip_block_refused_total counts them too (no labels: never the address).
	if after, _ := strconv.Atoi(apitest.Metric(t, "ip_block_refused_total")); after < beforeMetric+int(s.IPBlockRefusals()-before) {
		t.Fatalf("ip_block_refused_total: %d before, %d after", beforeMetric, after)
	}

	// Only what would open the door is exempt: the current key, or a pass,
	// from inside ADMIN_ALLOWED_IPS. From a blocked address, the current
	// key from outside the allowlist and the previous key without a
	// session get exactly what a wrong key gets (the block's answer), on
	// the API, pages and files alike; the refusal is still recorded.
	current, previous := "rotated-"+factory.AdminRouteKey, factory.AdminRouteKey
	guarded := serverWith(t, db, func(c *config.Config) {
		c.AdminRouteKey, c.AdminRouteKeyPrevious = current, previous
		c.AdminAllowedIPs = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	})
	apitest.Must(factory.IPBlocks.State(factory.BlockOf("203.0.113.0/24")).CreateOne(ctx, db, f))(t)
	apitest.Must(0, guarded.ReloadIPBlocks(ctx))(t)
	gh := router.New(guarded)
	wrongAPI := from(gh, blocked, "GET", "/api/admin/me", "wrong-key")
	sameAnswer(t, "the blocked API with a wrong key", wrongAPI, from(gh, blocked, "GET", "/api/nowhere", ""))
	sameAnswer(t, "the current key from outside the allowlist", from(gh, blocked, "GET", "/api/admin/me", current), wrongAPI)
	sameAnswer(t, "the previous key without a session", from(gh, blocked, "GET", "/api/admin/me", previous), wrongAPI)
	wrongPage := from(gh, blocked, "GET", "/admin/wrong-key/users", "")
	for name, target := range map[string]string{
		"the sign-in page under the current key from outside the allowlist": "/admin/" + current,
		"a page under the current key from outside the allowlist":           "/admin/" + current + "/users",
		"the sign-in page under the previous key":                           "/admin/" + previous,
		"a page under the previous key without a session":                   "/admin/" + previous + "/users",
	} {
		sameAnswer(t, name, from(gh, blocked, "GET", target, ""), wrongPage)
	}
	inside := "203.0.113.7"
	guardedPass := passFrom(t, fromAddress(gh, inside), current)
	sameAnswer(t, "a pass from outside the allowlist", from(gh, blocked, "GET", apitest.AdminAsset, "", guardedPass), from(gh, blocked, "GET", apitest.AdminAsset, ""))
	// Recorded by the block's check, before the API's security middleware:
	// with the browser all the same.
	if refused := eventually(t, db, "admin.access_refused", 1); len(refused) == 0 || refused[0].IP != blocked || refused[0].UserAgent != apitest.UserAgent {
		t.Fatalf("the refusal wasn't recorded, or without its browser: %+v", refused)
	}
	// From a blocked address inside the allowlist, the current key and the
	// pass still open the door; the previous key without a session doesn't.
	if w := from(gh, inside, "GET", "/api/admin/me", current); w.Code != 401 {
		t.Fatalf("the current key from inside the allowlist: %d %s", w.Code, w.Body.String())
	}
	if w := from(gh, inside, "GET", apitest.AdminAsset, "", guardedPass); w.Code != 200 {
		t.Fatalf("a pass from inside the allowlist: %d", w.Code)
	}
	sameAnswer(t, "the previous key inside the allowlist", from(gh, inside, "GET", "/api/admin/me", previous), from(gh, inside, "GET", "/api/admin/me", "wrong-key"))

	// Another server sees it at its next reload (every 30 seconds; here 20
	// milliseconds), and stops at its expiry.
	other := serverWith(t, db, func(*config.Config) {})
	otherHandler := router.New(other)
	if w := from(otherHandler, blocked, "GET", "/api/auth/me", "", user.Cookie()); w.Code != 200 {
		t.Fatalf("before its reload: %d", w.Code)
	}
	if !other.IPBlocksLoadedAt().IsZero() {
		t.Fatal("loaded before any load")
	}
	running, stop := context.WithCancel(ctx)
	defer stop()
	go other.RunIPBlocks(running, 20*time.Millisecond)
	waitFor(t, func() bool { return from(otherHandler, blocked, "GET", "/api/auth/me", "", user.Cookie()).Code == 403 })
	if loaded := other.IPBlocksLoadedAt(); time.Since(loaded) > time.Minute {
		t.Fatalf("when it loaded: %v", loaded)
	}
	apitest.Must(db.NewUpdate().TableExpr("ip_blocks").Set("created_at = now() - interval '2 seconds'").Set("expires_at = now() - interval '1 second'").Where("TRUE").Exec(ctx))(t)
	waitFor(t, func() bool { return from(otherHandler, blocked, "GET", "/api/auth/me", "", user.Cookie()).Code == 200 })

	// IPv6: a /64 covers its addresses, and nothing outside it.
	adminCall(t, h, "POST", "/api/admin/ip-blocks", reasonBody(reason, "network", "2001:db8:1:2::/64", "expiresIn", "1h"), session.CSRFToken, 201, session.Cookie())
	if w := from(h, "2001:db8:1:2::abcd", "GET", "/api/workspaces", ""); w.Code != 403 {
		t.Fatalf("inside the /64: %d", w.Code)
	}
	if w := from(h, "2001:db8:1:3::1", "GET", "/api/workspaces", ""); w.Code == 403 {
		t.Fatal("outside the /64 was blocked")
	}
	// Unblocking applies on this server at once.
	id := decodeAdmin[struct{ Data []store.IPBlock }](t, adminCall(t, h, "GET", "/api/admin/ip-blocks", "", "", 200, session.Cookie()).Body.Bytes()).Data[0].PublicID
	adminCall(t, h, "DELETE", "/api/admin/ip-blocks/"+id, reasonBody(reason), session.CSRFToken, 204, session.Cookie())
	if w := from(h, "2001:db8:1:2::abcd", "GET", "/api/workspaces", ""); w.Code == 403 {
		t.Fatal("still blocked after unblocking")
	}
}

// waitFor polls ok for up to 2 seconds.
func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The blocked answer comes from the address ClientAddress worked out, never
// a header from someone who isn't a trusted proxy: a forged
// CF-Connecting-IP neither blocks nor unblocks anyone.
func TestIPBlocksUseTheClientAddress(t *testing.T) {
	_, db := server(t)
	s := serverWith(t, db, func(c *config.Config) {
		c.ClientIPHeader = "CF-Connecting-IP"
		c.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	})
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	apitest.Must(factory.IPBlocks.State(factory.BlockOf("198.51.100.0/24")).CreateOne(ctx, db, f))(t)
	apitest.Must(0, s.ReloadIPBlocks(ctx))(t)
	send := func(peer, header string) int {
		r := httptest.NewRequest("GET", "/api/workspaces", nil)
		r.RemoteAddr = peer + ":1234"
		r.Header.Set("CF-Connecting-IP", header)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if send("10.0.0.1", "198.51.100.9") != 403 {
		t.Fatal("the proxy's client address wasn't blocked")
	}
	if send("10.0.0.1", "203.0.113.1") == 403 {
		t.Fatal("another client behind the proxy was blocked")
	}
	if send("203.0.113.1", "198.51.100.9") == 403 {
		t.Fatal("a forged header blocked someone")
	}
	if send("198.51.100.9", "203.0.113.1") != 403 {
		t.Fatal("a forged header unblocked someone")
	}
}

// manyBlocks makes n live blocks of distinct /24s in 10.0.0.0/8, made at
// made.
func manyBlocks(t *testing.T, db bun.IDB, f *gofakeit.Faker, n int, made time.Time) {
	t.Helper()
	i := 0
	distinct := func(b *factory.IPBlock) {
		b.Network, b.CreatedAt = fmt.Sprintf("10.%d.%d.0/24", i/256, i%256), made
		i++
	}
	apitest.Must(factory.IPBlocks.State(distinct).Create(context.Background(), db, f, n))(t)
}

// The cap of 1,000 live blocks holds when admins block at once: the live
// ones are counted under an advisory lock, so of ten blocks made together
// at 998 exactly two get in. If more were ever live, every server enforces
// the same 1,000: the oldest.
func TestIPBlockCap(t *testing.T) {
	_, db := server(t)
	s := serverWith(t, db, func(*config.Config) {})
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator, _ := operatorSignedIn(t, db, f)
	manyBlocks(t, db, f, store.MaxIPBlocks-2, time.Now().Add(-time.Hour))
	a := store.AdminAction{Admin: store.User{ID: operator.User.ID, PublicID: operator.User.PublicID, Name: operator.User.Name, Email: operator.User.Email}, Reason: reason}
	results := make(chan error, 10)
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			network := netip.MustParsePrefix(fmt.Sprintf("203.0.113.%d/32", i+1))
			_, err := s.Repo.CreateIPBlock(ctx, store.NewIPBlock{Network: network, For: time.Hour}, a)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	made, refused := 0, 0
	for err := range results {
		switch {
		case err == nil:
			made++
		case errors.Is(err, store.ErrTooManyIPBlocks):
			refused++
		default:
			t.Fatal(err)
		}
	}
	live := apitest.Must(db.NewSelect().TableExpr("ip_blocks").Where("expires_at > now()").Count(ctx))(t)
	if made != 2 || refused != 8 || live != store.MaxIPBlocks {
		t.Fatalf("made %d, refused %d, live %d", made, refused, live)
	}

	// Past the cap (a row made around CreateIPBlock): the oldest 1,000, so
	// the newest of the two just made is left out.
	oldest := apitest.Must(factory.IPBlocks.State(factory.BlockOf("192.0.2.0/24")).State(func(b *factory.IPBlock) {
		b.CreatedAt = time.Now().Add(-2 * time.Hour)
	}).CreateOne(ctx, db, f))(t)
	loaded := apitest.Must(s.Repo.IndexLiveIPBlocks(ctx))(t)
	if len(loaded) != store.MaxIPBlocks || loaded[0].Network != oldest.Network {
		t.Fatalf("the live list: %d, first %+v", len(loaded), loaded[0])
	}
	newest := 0
	for _, block := range loaded {
		if strings.HasPrefix(block.Network, "203.0.113.") {
			newest++
		}
	}
	if newest != 1 {
		t.Fatalf("of the two newest blocks, %d were loaded, want the older one", newest)
	}
}
