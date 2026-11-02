package events

// Mail kinds share one sent fact, while keeping delivery volumes distinct.
const (
	EmailChangeMailCode     = "code"
	EmailChangeMailExisting = "existing_account"
	EmailChangeMailPrevious = "previous_notice"
	EmailChangeMailCurrent  = "current_notice"
)

// EmailChangeRequested queues proof of the candidate inbox, or an informational
// notice for an already-registered destination. Never identifies its owner.
type EmailChangeRequested struct {
	User                 string `json:"user"`
	Email                string `json:"email"`
	VerificationRequired bool   `json:"verificationRequired"`
}

func (EmailChangeRequested) EventName() string { return "auth.email_change.requested" }

// EmailChangeSent is admin-only delivery evidence, excluded from user exports.
type EmailChangeSent struct {
	User            string `json:"user"`
	Request         string `json:"request"`
	ExistingAccount bool   `json:"existingAccount"`
	Kind            string `json:"kind"`
}

func (EmailChangeSent) EventName() string { return "auth.email_change.sent" }

// EmailChangeChecked records an issued code's comparison, never the code.
type EmailChangeChecked struct {
	User    string `json:"user"`
	OK      bool   `json:"ok"`
	Attempt int    `json:"attempt"`
}

func (EmailChangeChecked) EventName() string { return "auth.email_change.checked" }

// EmailChangeCancelled records explicit cancellation of an existing request.
type EmailChangeCancelled struct {
	User string `json:"user"`
}

func (EmailChangeCancelled) EventName() string { return "auth.email_change.cancelled" }

// EmailChanged captures immutable notice destinations in the change transaction.
type EmailChanged struct {
	User                 string `json:"user"`
	Email                string `json:"email"`
	PreviousEmail        string `json:"previousEmail"`
	VerifiedBy           string `json:"verifiedBy"`
	SessionsRevoked      int    `json:"sessionsRevoked"`
	AdminSessionsRevoked int    `json:"adminSessionsRevoked"`
}

func (EmailChanged) EventName() string { return "auth.email_changed" }
