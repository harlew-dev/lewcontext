package store

// WorkspaceTable is a table holding rows that belong to a workspace, by
// its workspace_id column.
type WorkspaceTable struct {
	// Name is the table's, as in its create migration.
	Name string `json:"table"`
	// Label is what its rows are, for people.
	Label string `json:"label"`
}

// WorkspaceTables are every table with a workspace_id column, in the order
// the Workspaces page lists them (monitoring decision 9). The page counts
// a workspace's rows across all of them, so a new table (tasks, files) is
// counted once it's listed here, and TestEveryWorkspaceTableIsMonitored
// fails until it is. Each needs an index leading with workspace_id, which
// the same test checks: the counts read one workspace at a time.
var WorkspaceTables = []WorkspaceTable{
	{Name: "projects", Label: "Projects"},
	{Name: "workspace_user", Label: "Members"},
	{Name: "workspace_invitations", Label: "Invitations"},
	// Events about the workspace: kept 90 days (make prune), and set to
	// null when it's deleted.
	{Name: "events", Label: "Events"},
}
