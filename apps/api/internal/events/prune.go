package events

// PruneCompleted: a confirmed prune run finished a target
// (docs/api/database.md#pruning), the evidence that retention runs:
// Target is its name, Deleted how many rows it deleted (or, for the
// pseudonymise target, minimised), Kept how many old rows it kept on
// purpose, Cutoff the age line. No scope or actor: the command runs it.
type PruneCompleted struct {
	Target  string `json:"target"`
	Deleted int    `json:"deleted"`
	Kept    int    `json:"kept"`
	Cutoff  string `json:"cutoff"`
}

func (PruneCompleted) EventName() string { return "prune.completed" }
