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

// WorkspaceMemberUpdated: someone (the actor) changed a member's role.
type WorkspaceMemberUpdated struct {
	Workspace string  `json:"workspace"`
	User      string  `json:"user"`
	Changes   Changes `json:"changes"`
}

func (WorkspaceMemberUpdated) EventName() string { return "workspace.member.updated" }

// WorkspaceMemberRemoved: someone (the actor) removed a member, who had role.
// The account stays.
type WorkspaceMemberRemoved struct {
	Workspace string `json:"workspace"`
	User      string `json:"user"`
	Role      string `json:"role"`
}

func (WorkspaceMemberRemoved) EventName() string { return "workspace.member.removed" }

// WorkspaceMemberLeft: a member (the actor) left the workspace.
type WorkspaceMemberLeft struct {
	Workspace string `json:"workspace"`
	Role      string `json:"role"`
}

func (WorkspaceMemberLeft) EventName() string { return "workspace.member.left" }
