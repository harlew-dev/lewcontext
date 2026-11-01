package store

// DataTables are every table the migrations create (bun's own left out),
// as docs/compliance.md's data map lists them: the tables the admin
// Monitoring Data page shows and table_stats_daily records. A table name
// read from PostgreSQL's catalog is shown only when it's here, so nothing
// else in the database (another schema's, one made by hand) ever reaches
// the page. TestEveryTableIsInTheDataMap (internal/database) fails when
// this list, the migrations and the data map disagree.
var DataTables = []string{
	"users",
	"sessions",
	"workspaces",
	"workspace_user",
	"projects",
	"workspace_invitations",
	"events",
	"event_deliveries",
	"rate_limits",
	"password_resets",
	"mfa_totp",
	"mfa_recovery_codes",
	"mfa_challenges",
	"pending_registrations",
	"email_verifications",
	"email_block_links",
	"email_blocks",
	"admins",
	"admin_sessions",
	"user_suspensions",
	"ip_blocks",
	"app_errors",
	"request_minutes",
	"request_hours",
	"metric_minutes",
	"metric_hours",
	"server_instances",
	"table_stats_daily",
}
