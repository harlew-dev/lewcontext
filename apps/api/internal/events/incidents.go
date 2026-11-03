package events

// AdminAlertCommented: an admin posted to an alert's incident cycle.
type AdminAlertCommented struct {
	Admin   string `json:"admin"`
	Cycle   string `json:"cycle"`
	Message string `json:"message"`
	Alert   string `json:"alert"`
}

func (AdminAlertCommented) EventName() string { return "admin.alert.commented" }

// AdminErrorCommented: an admin posted to an error group's incident cycle.
type AdminErrorCommented struct {
	Admin   string `json:"admin"`
	Cycle   string `json:"cycle"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

func (AdminErrorCommented) EventName() string { return "admin.error.commented" }

// AdminAlertRedacted: an operator removed text, retaining a tombstone.
type AdminAlertRedacted struct {
	Admin   string `json:"admin"`
	Cycle   string `json:"cycle"`
	Message string `json:"message"`
	Alert   string `json:"alert"`
	Reason  string `json:"reason"`
}

func (AdminAlertRedacted) EventName() string { return "admin.alert.redacted" }

// AdminErrorRedacted: an operator removed text, retaining a tombstone.
type AdminErrorRedacted struct {
	Admin   string `json:"admin"`
	Cycle   string `json:"cycle"`
	Message string `json:"message"`
	Error   string `json:"error"`
	Reason  string `json:"reason"`
}

func (AdminErrorRedacted) EventName() string { return "admin.error.redacted" }
