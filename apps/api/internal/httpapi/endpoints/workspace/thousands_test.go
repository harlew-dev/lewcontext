package workspace

import "testing"

func TestThousands(t *testing.T) {
	for n, want := range map[int]string{0: "0", 20: "20", 999: "999", 1000: "1,000", 1234567: "1,234,567"} {
		if got := thousands(n); got != want {
			t.Errorf("thousands(%d) = %q, want %q", n, got, want)
		}
	}
}
