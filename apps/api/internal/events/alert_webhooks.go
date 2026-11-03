package events

// Alert webhooks and heartbeats are managed by operators in the dashboard
// (docs/api/monitoring.md). None of these events holds a receiver's URL,
// host or signing key, only references and the names of what changed.

// AlertWebhookKinds are the alert event kinds a webhook may choose, in the
// order the dashboard lists them: the suffix of the alert.* event name.
var AlertWebhookKinds = []string{"opened", "escalated", "recovered", "reopened", "resolved", "reminded"}

// AdminAlertWebhookCreated: an operator added a webhook or heartbeat.
type AdminAlertWebhookCreated struct {
	Admin   string `json:"admin"`
	Webhook string `json:"webhook"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Reason  string `json:"reason"`
}

func (AdminAlertWebhookCreated) EventName() string { return "admin.alert_webhook.created" }

// AdminAlertWebhookUpdated: an operator changed one. Fields names what
// changed (name, signals, events, enabled, url, format) and Operation is
// "update" or "rotate_key"; never a value of a URL or a key.
type AdminAlertWebhookUpdated struct {
	Admin     string   `json:"admin"`
	Webhook   string   `json:"webhook"`
	Operation string   `json:"operation"`
	Fields    []string `json:"fields"`
	Revision  int      `json:"revision"`
	Reason    string   `json:"reason"`
}

func (AdminAlertWebhookUpdated) EventName() string { return "admin.alert_webhook.updated" }

// AdminAlertWebhookDeleted: an operator removed one; its unsent notices skip.
type AdminAlertWebhookDeleted struct {
	Admin   string `json:"admin"`
	Webhook string `json:"webhook"`
	Kind    string `json:"kind"`
	Reason  string `json:"reason"`
}

func (AdminAlertWebhookDeleted) EventName() string { return "admin.alert_webhook.deleted" }

// AdminAlertWebhookListed: the Subscriptions tab read the webhooks.
type AdminAlertWebhookListed struct {
	Admin string `json:"admin"`
}

func (AdminAlertWebhookListed) EventName() string { return "admin.alert_webhook.listed" }

// AdminAlertWebhookViewed: an operator read one webhook's signing key (inside
// a step-up window); Section is "signing_key". The key is never recorded.
type AdminAlertWebhookViewed struct {
	Admin   string `json:"admin"`
	Webhook string `json:"webhook"`
	Section string `json:"section"`
}

func (AdminAlertWebhookViewed) EventName() string { return "admin.alert_webhook.viewed" }

// AdminAlertRuleListed: an admin read the alert catalogue with their own
// preferences.
type AdminAlertRuleListed struct {
	Admin string `json:"admin"`
}

func (AdminAlertRuleListed) EventName() string { return "admin.alert_rule.listed" }
