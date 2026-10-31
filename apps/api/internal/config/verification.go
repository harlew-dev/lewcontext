package config

import "errors"

// EmailVerification is whether accounts prove they own their email with a
// code (EMAIL_VERIFICATION, on by default). The zero value is off, so a
// Config built by hand (the tests) signs up as before.
type EmailVerification struct {
	// Verify makes a new sign-up enter a code sent to its email before an
	// account exists, and lets unverified accounts verify from Account
	// settings.
	Verify bool
	// RequireExisting (EMAIL_VERIFICATION_EXISTING=required) makes an
	// account that was never verified verify before it can do anything
	// after signing in.
	RequireExisting bool
}

// VerificationMode is what the web app is told: off, optional or required.
func (v EmailVerification) VerificationMode() string {
	switch {
	case !v.Verify:
		return "off"
	case v.RequireExisting:
		return "required"
	}
	return "optional"
}

// loadEmailVerification reads EMAIL_VERIFICATION and
// EMAIL_VERIFICATION_EXISTING. Verifying sends codes, so it needs mail.
func (c *Config) loadEmailVerification() error {
	switch env("EMAIL_VERIFICATION", "on") {
	case "on":
		c.Verify = true
	case "off":
	default:
		return errors.New("EMAIL_VERIFICATION must be on or off")
	}
	switch env("EMAIL_VERIFICATION_EXISTING", "optional") {
	case "optional":
	case "required":
		c.RequireExisting = true
	default:
		return errors.New("EMAIL_VERIFICATION_EXISTING must be optional or required")
	}
	if c.RequireExisting && !c.Verify {
		return errors.New("EMAIL_VERIFICATION_EXISTING=required needs EMAIL_VERIFICATION=on")
	}
	if c.Verify && c.MailDriver == MailOff {
		return errors.New("EMAIL_VERIFICATION=on sends codes by email, but MAIL_DRIVER is off: set EMAIL_VERIFICATION=off to run without email")
	}
	return nil
}
