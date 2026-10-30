package events

// ProjectCreated: a project was created, with its first values.
type ProjectCreated struct {
	Project     string `json:"project"`
	Workspace   string `json:"workspace"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Color       string `json:"color"`
}

func (ProjectCreated) EventName() string { return "project.created" }

// ProjectUpdated: a project's fields changed. Only changed fields appear.
type ProjectUpdated struct {
	Project   string  `json:"project"`
	Workspace string  `json:"workspace"`
	Changes   Changes `json:"changes"`
}

func (ProjectUpdated) EventName() string { return "project.updated" }
