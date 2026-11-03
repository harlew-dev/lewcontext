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

// WorkspaceDeleted: an owner (the actor) deleted a workspace, with its
// projects and memberships, counted when it was deleted.
type WorkspaceDeleted struct {
	Workspace string `json:"workspace"`
	Name      string `json:"name"`
	Projects  int    `json:"projects"`
	Members   int    `json:"members"`
}

func (WorkspaceDeleted) EventName() string { return "workspace.deleted" }

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

// WorkspaceInvitationCreated: someone (the actor) invited Email to the
// workspace as Role. The mail.workspace_invitation listener sends the link.
type WorkspaceInvitationCreated struct {
	Invitation string `json:"invitation"`
	Workspace  string `json:"workspace"`
	Email      string `json:"email"`
	Role       string `json:"role"`
}

func (WorkspaceInvitationCreated) EventName() string { return "workspace.invitation.created" }

// WorkspaceInvitationSent: an invitation's link was emailed, for the created
// event Request (evt_…). A resend after a retry replaces the earlier link.
type WorkspaceInvitationSent struct {
	Invitation string `json:"invitation"`
	Workspace  string `json:"workspace"`
	Request    string `json:"request"`
}

func (WorkspaceInvitationSent) EventName() string { return "workspace.invitation.sent" }

// WorkspaceInvitationRevoked: someone (the actor) revoked the invitation to
// Email, so its link stops working.
type WorkspaceInvitationRevoked struct {
	Invitation string `json:"invitation"`
	Workspace  string `json:"workspace"`
	Email      string `json:"email"`
}

func (WorkspaceInvitationRevoked) EventName() string { return "workspace.invitation.revoked" }

// WorkspaceInvitationAccepted: the invitee (the actor and user) accepted and
// joined as Role, or kept the role they already had.
type WorkspaceInvitationAccepted struct {
	Invitation string `json:"invitation"`
	Workspace  string `json:"workspace"`
	Role       string `json:"role"`
}

func (WorkspaceInvitationAccepted) EventName() string { return "workspace.invitation.accepted" }
