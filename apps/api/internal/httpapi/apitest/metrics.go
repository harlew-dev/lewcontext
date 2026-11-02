package apitest

import (
	"bytes"
	"lewcontext/api/internal/observe"
	"strings"
	"testing"
)

// Metric is a sample's value in the process's /metrics (observe.Metrics),
// such as Metric(t, "ip_block_refused_total"), or "" when it has none.
func Metric(t *testing.T, sample string) string {
	t.Helper()
	var out bytes.Buffer
	if err := observe.Metrics.WritePrometheus(&out); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if value, ok := strings.CutPrefix(line, sample+" "); ok {
			return value
		}
	}
	return ""
}
