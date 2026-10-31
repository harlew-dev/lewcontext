package events

// How a second factor was given.
const (
	MFAMethodTOTP         = "totp"
	MFAMethodRecoveryCode = "recovery_code"
)

// Steps that check a second factor.
const (
	MFAStepLogin         = "login"
	MFAStepEnable        = "enable"
	MFAStepDisable       = "disable"
	MFAStepRecoveryCodes = "recovery_codes"
)

// Why MFA was turned off: by the user, or reset by the operator (admin
// reset-mfa) for someone who lost their authenticator and recovery codes.
const (
	MFADisabledByUser = "user"
	MFADisabledReset  = "reset"
)

// LoginMFARequired is auth.login_attempted's result when the password was
// right and a second factor is still needed.
const LoginMFARequired = "mfa_required"

// MFAEnabled: a user turned two-factor authentication on, which signed out
// their other sessions. The first recovery codes come with it.
type MFAEnabled struct {
	User                 string `json:"user"`
	OtherSessionsRevoked int    `json:"otherSessionsRevoked"`
}

func (MFAEnabled) EventName() string { return "auth.mfa.enabled" }

// MFADisabled: two-factor authentication was turned off, for Reason.
type MFADisabled struct {
	User   string `json:"user"`
	Reason string `json:"reason"`
}

func (MFADisabled) EventName() string { return "auth.mfa.disabled" }

// MFAChecked: a second factor was checked, for Step, by Method. A recovery
// code also says how many are left. IP is the client's address.
type MFAChecked struct {
	User              string `json:"user"`
	OK                bool   `json:"ok"`
	Method            string `json:"method"`
	Step              string `json:"step"`
	IP                string `json:"ip"`
	RecoveryCodesLeft *int   `json:"recoveryCodesLeft,omitempty"`
}

func (MFAChecked) EventName() string { return "auth.mfa.checked" }

// RecoveryCodesCreated: a user replaced their recovery codes with a new set.
type RecoveryCodesCreated struct {
	User  string `json:"user"`
	Count int    `json:"count"`
}

func (RecoveryCodesCreated) EventName() string { return "auth.recovery_codes.created" }
