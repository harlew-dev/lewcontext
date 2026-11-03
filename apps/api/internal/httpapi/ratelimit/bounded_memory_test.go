package ratelimit

import (
	"testing"
	"time"
)

func TestAnonymousCountersHaveAHardCapacity(t *testing.T) {
	m := NewBoundedMemory(2)
	now := time.Now()
	m.now = func() time.Time { return now }
	for _, key := range []string{"ingress:normalized-a", "failed:normalized-a"} {
		if hits, _, ok := m.Count(key, 5*time.Minute, false); !ok || hits != 0 {
			t.Fatal("reservation failed")
		}
	}
	if _, _, ok := m.Count("ingress:normalized-b", time.Minute, true); ok {
		t.Fatal("capacity was not enforced")
	}
	for range 10 {
		if _, _, ok := m.Count("failed:normalized-a", 5*time.Minute, true); !ok {
			t.Fatal("saturation evicted an existing window")
		}
	}
	if hits, _, _ := m.Count("failed:normalized-a", time.Minute, false); hits != 10 {
		t.Fatal("checks incremented failures or cleared a live block")
	}
	if len(m.windows) != 2 {
		t.Fatal("counter growth exceeded capacity")
	}
	now = now.Add(6 * time.Minute)
	if hits, _, ok := m.Count("ingress:normalized-b", time.Minute, true); !ok || hits != 1 {
		t.Fatal("expired capacity was not reclaimed")
	}
}
