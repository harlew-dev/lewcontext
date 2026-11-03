package config

import (
	"strings"
	"testing"
)

func TestPolicies(t *testing.T) {
	setEnv(t, nil)
	if c, err := Load(); err != nil || c.Policies {
		t.Fatalf("default: %v %v", c.Policies, err)
	}
	setEnv(t, map[string]string{"POLICIES": "on"})
	if c, err := Load(); err != nil || !c.Policies {
		t.Fatalf("on: %v %v", c.Policies, err)
	}
	setEnv(t, map[string]string{"POLICIES": "off"})
	if c, err := Load(); err != nil || c.Policies {
		t.Fatalf("off: %v %v", c.Policies, err)
	}
	for _, value := range []string{"true", "1", "ON"} {
		setEnv(t, map[string]string{"POLICIES": value})
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "POLICIES") {
			t.Errorf("%q accepted (%v)", value, err)
		}
	}
}
