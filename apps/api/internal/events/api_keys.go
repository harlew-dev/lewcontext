package events

// ApiKeyDetails contains only bounded grants and non-secret identifiers.
type ApiKeyDetails struct {
	Key       string   `json:"key"`
	Workspace string   `json:"workspace"`
	Scopes    []string `json:"scopes"`
	Fields    []string `json:"fields,omitempty"`
	Reason    string   `json:"reason,omitempty"`
}

type WorkspaceApiKeyCreated ApiKeyDetails

func (WorkspaceApiKeyCreated) EventName() string { return "workspace.api_key.created" }

type WorkspaceApiKeyUpdated ApiKeyDetails

func (WorkspaceApiKeyUpdated) EventName() string { return "workspace.api_key.updated" }

type WorkspaceApiKeyRevoked ApiKeyDetails

func (WorkspaceApiKeyRevoked) EventName() string { return "workspace.api_key.revoked" }

type PersonalApiKeyCreated ApiKeyDetails

func (PersonalApiKeyCreated) EventName() string { return "auth.api_key.created" }

type PersonalApiKeyUpdated ApiKeyDetails

func (PersonalApiKeyUpdated) EventName() string { return "auth.api_key.updated" }

type PersonalApiKeyRevoked ApiKeyDetails

func (PersonalApiKeyRevoked) EventName() string { return "auth.api_key.revoked" }
