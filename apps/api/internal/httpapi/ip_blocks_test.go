package httpapi

import (
	"context"
	"errors"
	"lewcontext/api/internal/store"
	"net/netip"
	"sync"
	"testing"
	"time"
)

// One row whose network doesn't parse is skipped, never the whole list:
// the other blocks still apply.
func TestParseLiveBlocksSkipsABadRow(t *testing.T) {
	end := time.Now().Add(time.Hour)
	live := parseLiveBlocks([]store.LiveIPBlock{
		{Network: "198.51.100.0/24", ExpiresAt: end},
		{Network: "not-a-network", ExpiresAt: end},
		{Network: "2001:db8:1:2::/64", ExpiresAt: end},
	})
	if len(live) != 2 || live[0].network != netip.MustParsePrefix("198.51.100.0/24") || live[1].network != netip.MustParsePrefix("2001:db8:1:2::/64") || !live[1].expiresAt.Equal(end) {
		t.Fatalf("the list: %+v", live)
	}
}

// reloads records when a fake reload ran, failing the first failures
// times.
type reloads struct {
	mu       sync.Mutex
	at       []time.Time
	failures int
}

func (r *reloads) reload(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.at = append(r.at, time.Now())
	if len(r.at) <= r.failures {
		return errors.New("the database is down")
	}
	return nil
}

func (r *reloads) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.at)
}

// Until the blocks have loaded once, a server retries soon (5, 10, 20
// milliseconds here; a second, doubling, in production), not after the
// full interval; once loaded, it waits the interval again.
func TestRunReloadsRetriesSoonUntilLoaded(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	r := &reloads{failures: 3}
	start := time.Now()
	go runReloads(ctx, false, time.Second, 5*time.Millisecond, r.reload)
	deadline := start.Add(900 * time.Millisecond)
	for r.count() < 4 {
		if time.Now().After(deadline) {
			t.Fatalf("not loaded before the interval: %d tries", r.count())
		}
		time.Sleep(time.Millisecond)
	}
	r.mu.Lock()
	gaps := []time.Duration{r.at[1].Sub(r.at[0]), r.at[2].Sub(r.at[1]), r.at[3].Sub(r.at[2])}
	r.mu.Unlock()
	if gaps[1] < gaps[0] || gaps[2] < 15*time.Millisecond {
		t.Fatalf("the retries didn't back off: %v", gaps)
	}
	// Loaded: the next reload waits the full interval.
	time.Sleep(300 * time.Millisecond)
	if n := r.count(); n != 4 {
		t.Fatalf("%d reloads within the interval after loading", n)
	}
}

// A server that loaded the blocks at startup waits the full interval, and
// so does one whose reload fails afterwards (the old list still applies).
func TestRunReloadsAfterALoadWaitsTheInterval(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	r := &reloads{failures: 100}
	go runReloads(ctx, true, 100*time.Millisecond, time.Millisecond, r.reload)
	time.Sleep(50 * time.Millisecond)
	if n := r.count(); n != 0 {
		t.Fatalf("%d reloads before the interval", n)
	}
	for r.count() < 2 {
		time.Sleep(time.Millisecond)
	}
	r.mu.Lock()
	gap := r.at[1].Sub(r.at[0])
	r.mu.Unlock()
	if gap < 90*time.Millisecond {
		t.Fatalf("a failed reload after a load retried after %v", gap)
	}
}
