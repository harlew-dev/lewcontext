package events

// AdminAlertResolved records references only; discussion text stays in its row.
type AdminAlertResolved struct {
	Admin   string `json:"admin"`
	Alert   string `json:"alert"`
	Cycle   string `json:"cycle"`
	Message string `json:"message"`
}

func (AdminAlertResolved) EventName() string { return "admin.alert.resolved" }
