package events

// WorkspaceCreated: a user created a workspace and became its owner (the
// event's actor).
type WorkspaceCreated struct {
	Workspace string `json:"workspace"`
	Name      string `json:"name"`
}

func (WorkspaceCreated) EventName() string { return "workspace.created" }

// WorkspaceRenamed: a workspace's name changed.
type WorkspaceRenamed struct {
	Workspace string  `json:"workspace"`
	Changes   Changes `json:"changes"`
}

func (WorkspaceRenamed) EventName() string { return "workspace.renamed" }
