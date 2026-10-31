package config

import (
	"strings"
	"testing"
)

func TestEmailVerification(t *testing.T) {
	// On by default, existing accounts optional.
	setEnv(t, smtpEnv(map[string]string{"EMAIL_VERIFICATION": "", "EMAIL_VERIFICATION_EXISTING": ""}))
	c, err := Load()
	if err != nil || !c.Verify || c.RequireExisting || c.VerificationMode() != "optional" {
		t.Fatalf("default: %+v %v", c.EmailVerification, err)
	}
	setEnv(t, smtpEnv(map[string]string{"EMAIL_VERIFICATION": "on", "EMAIL_VERIFICATION_EXISTING": "required"}))
	if c, err := Load(); err != nil || !c.RequireExisting || c.VerificationMode() != "required" {
		t.Fatalf("required: %+v %v", c.EmailVerification, err)
	}
	setEnv(t, smtpEnv(map[string]string{"EMAIL_VERIFICATION": "off"}))
	if c, err := Load(); err != nil || c.Verify || c.VerificationMode() != "off" {
		t.Fatalf("off: %+v %v", c.EmailVerification, err)
	}
	refused := map[string]map[string]string{
		"codes need mail":            {"MAIL_DRIVER": "off", "EMAIL_VERIFICATION": "on"},
		"required needs it on":       smtpEnv(map[string]string{"EMAIL_VERIFICATION": "off", "EMAIL_VERIFICATION_EXISTING": "required"}),
		"unknown EMAIL_VERIFICATION": smtpEnv(map[string]string{"EMAIL_VERIFICATION": "true"}),
		"unknown EXISTING":           smtpEnv(map[string]string{"EMAIL_VERIFICATION_EXISTING": "yes"}),
	}
	for name, vars := range refused {
		setEnv(t, vars)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EMAIL_VERIFICATION") {
			t.Errorf("%s: accepted (%v)", name, err)
		}
	}
}
