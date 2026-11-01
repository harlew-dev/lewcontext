package events

// Who unblocked an address (EmailUnblocked.By): its owner, from the block
// link in an email, or the operator, with admin unblock.
const (
	UnblockedByLink = "link"
	UnblockedByCLI  = "cli"
)

// EmailBlocked: an address without an account was put on the block list
// from a block link, so it gets no more email and can't be used to sign up.
type EmailBlocked struct {
	Email string `json:"email"`
}

func (EmailBlocked) EventName() string { return "email.blocked" }

// EmailUnblocked: an address was taken off the block list, By its link or
// the command line.
type EmailUnblocked struct {
	Email string `json:"email"`
	By    string `json:"by"`
}

func (EmailUnblocked) EventName() string { return "email.unblocked" }
