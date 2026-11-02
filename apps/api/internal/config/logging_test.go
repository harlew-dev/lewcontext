package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

// Text at info with every request logged by default, JSON in production;
// each setting takes only the values it knows, in Load as in LoadLogging.
func TestLogging(t *testing.T) {
	setEnv(t, nil)
	c, err := Load()
	if err != nil || c.LogFormat != "text" || c.LogLevel != slog.LevelInfo || c.AccessLog != AccessLogAll {
		t.Fatalf("default: %+v %v", c.Logging, err)
	}
	setEnv(t, map[string]string{"APP_ENV": "production"})
	if l, err := LoadLogging(); err != nil || l.LogFormat != "json" {
		t.Fatalf("production: %+v %v", l, err)
	}
	setEnv(t, map[string]string{"APP_ENV": "production", "LOG_FORMAT": "text", "LOG_LEVEL": "debug", "ACCESS_LOG": "errors"})
	if l, err := LoadLogging(); err != nil || l.LogFormat != "text" || l.LogLevel != slog.LevelDebug || l.AccessLog != AccessLogErrors {
		t.Fatalf("set: %+v %v", l, err)
	}
	for level, want := range map[string]slog.Level{"warn": slog.LevelWarn, "error": slog.LevelError, "info": slog.LevelInfo} {
		setEnv(t, map[string]string{"LOG_LEVEL": level, "ACCESS_LOG": "off"})
		if l, err := LoadLogging(); err != nil || l.LogLevel != want || l.AccessLog != AccessLogOff {
			t.Fatalf("%s: %+v %v", level, l, err)
		}
	}
	for name, value := range map[string]string{"LOG_FORMAT": "JSON", "LOG_LEVEL": "verbose", "ACCESS_LOG": "some"} {
		setEnv(t, map[string]string{name: value})
		if _, err := LoadLogging(); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s=%s: %v", name, value, err)
		}
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("Load with %s=%s: %v", name, value, err)
		}
	}
}

// SHUTDOWN_DELAY: none by default, 5 s in production, a duration up to a
// minute.
func TestShutdownDelay(t *testing.T) {
	setEnv(t, nil)
	if c, err := Load(); err != nil || c.ShutdownDelay != 0 {
		t.Fatalf("default: %v %v", c.ShutdownDelay, err)
	}
	setEnv(t, map[string]string{"APP_ENV": "production"})
	if d, err := loadShutdownDelay(true); err != nil || d != 5*time.Second {
		t.Fatalf("production: %v %v", d, err)
	}
	for value, want := range map[string]time.Duration{"0s": 0, "15s": 15 * time.Second, "60s": time.Minute, "1m": time.Minute, "500ms": 500 * time.Millisecond} {
		setEnv(t, map[string]string{"SHUTDOWN_DELAY": value})
		if c, err := Load(); err != nil || c.ShutdownDelay != want {
			t.Errorf("%s: %v %v", value, c.ShutdownDelay, err)
		}
	}
	for _, value := range []string{"5", "-1s", "61s", "2m", "soon"} {
		setEnv(t, map[string]string{"SHUTDOWN_DELAY": value})
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SHUTDOWN_DELAY") {
			t.Errorf("%s accepted: %v", value, err)
		}
	}
}
