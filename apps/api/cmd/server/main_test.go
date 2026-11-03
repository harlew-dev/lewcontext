package main

import (
	"bytes"
	"lewcontext/api/internal/observe"
	"log/slog"
	"strings"
	"testing"
)

// A LOG_LEVEL above info hides the audit lines, and the warning says so
// at every such level, error included; at info and below there's none.
func TestAuditLinesHiddenWarning(t *testing.T) {
	for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		var out bytes.Buffer
		warnIfAuditLinesHidden(observe.NewLogger(&out, observe.FormatJSON, level), level)
		warned := strings.Contains(out.String(), "audit lines")
		if warned != (level > slog.LevelInfo) {
			t.Errorf("LOG_LEVEL %v: warned %v: %q", level, warned, out.String())
		}
	}
}
