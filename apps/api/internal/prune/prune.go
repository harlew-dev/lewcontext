// Package prune deletes old rows from tables that grow without limit, such as
// the events audit trail, for the prune command (cmd/prune). It previews
// unless told to delete. Each target is a file here that registers a Pruner,
// with its queries in internal/store/prune_<target>.go; this file holds what
// they share: the list, the age parsing and the batched run.
package prune

import (
	"context"
	"fmt"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Pruner is one table the command can prune. Each target lives in its own
// file (events.go) and registers itself; its queries are a store.Prunable in
// internal/store/prune_<target>.go.
type Pruner struct {
	Name        string
	Description string
	DefaultAge  time.Duration
	Table       store.Prunable
	// KeptReason follows the count of rows kept on purpose, e.g. "with
	// pending deliveries".
	KeptReason string
	// Minimises makes a confirmed run minimise the old rows
	// (store.PseudonymiseEvents) instead of deleting them: the pseudonymise
	// target. Table then says which rows are left to minimise.
	Minimises bool
}

const day = 24 * time.Hour

var pruners = map[string]Pruner{}

// register adds a target; each target's file calls it from init.
func register(p Pruner) {
	if _, taken := pruners[p.Name]; taken || p.Name == "" || p.DefaultAge < MinAge || p.Table.Model == nil {
		panic(fmt.Sprintf("prune: target %q is a duplicate or incomplete", p.Name))
	}
	pruners[p.Name] = p
}

// Pruners lists the targets by name.
func Pruners() []Pruner {
	list := make([]Pruner, 0, len(pruners))
	for _, p := range pruners {
		list = append(list, p)
	}
	slices.SortFunc(list, func(a, b Pruner) int { return strings.Compare(a.Name, b.Name) })
	return list
}

// Find returns the target with a name.
func Find(name string) (Pruner, bool) {
	p, ok := pruners[name]
	return p, ok
}

// MinAge is the youngest rows a run may delete, so a typo such as "0d"
// can't empty a table.
const MinAge = time.Hour

var agePattern = regexp.MustCompile(`^([0-9]{1,5})([dh])$`)

// ParseAge reads an -older-than value: a whole number of days or hours,
// such as "90d" or "36h", of at least an hour.
func ParseAge(value string) (time.Duration, error) {
	m := agePattern.FindStringSubmatch(value)
	if m == nil {
		return 0, fmt.Errorf("-older-than %q: use days or hours, such as 90d or 36h", value)
	}
	n, _ := strconv.Atoi(m[1])
	unit := time.Hour
	if m[2] == "d" {
		unit = 24 * time.Hour
	}
	age := time.Duration(n) * unit
	if age < MinAge {
		return 0, fmt.Errorf("-older-than %q: the minimum is 1h", value)
	}
	return age, nil
}

// FormatAge writes an age the way ParseAge reads it.
func FormatAge(age time.Duration) string {
	if age%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", age/(24*time.Hour))
	}
	return fmt.Sprintf("%dh", age/time.Hour)
}

// Options for a run. Zero values use the defaults.
type Options struct {
	Age     time.Duration // the pruner's DefaultAge
	Confirm bool          // false previews
	Now     time.Time     // time.Now()
	Batch   int           // rows per DELETE: 5000 (per minimising transaction: 1000)
	// Pseudonym is auth.Keys.EmailPseudonym, which a minimising target
	// needs (from APP_KEY).
	Pseudonym func(string) string
}

// Report is what a run found or did.
type Report struct {
	Pruner    Pruner
	Age       time.Duration
	Before    time.Time
	Confirmed bool
	Deleted   int // or, in a preview, would delete
	Kept      int
}

func (r Report) String() string {
	verb := "would delete"
	switch {
	case r.Pruner.Minimises && r.Confirmed:
		verb = "minimised"
	case r.Pruner.Minimises:
		verb = "would minimise"
	case r.Confirmed:
		verb = "deleted"
	}
	text := fmt.Sprintf("%s: %s %d older than %s (%s)", r.Pruner.Name, verb, r.Deleted, r.Before.UTC().Format("2006-01-02 15:04 UTC"), FormatAge(r.Age))
	if r.Kept > 0 {
		text += fmt.Sprintf("; kept %d %s", r.Kept, r.Pruner.KeptReason)
	}
	return text
}

// Run previews or prunes. The cutoff is fixed when it starts, so rows created
// meanwhile are never in scope. Deleting goes in batches, each its own short
// statement; when ctx ends between batches it stops and the report counts
// what was deleted so far, alongside ctx's error. A confirmed run that
// finishes records prune.completed (the evidence retention runs).
func Run(ctx context.Context, s *store.Store, p Pruner, o Options) (Report, error) {
	r, err := run(ctx, s, p, o)
	if err != nil || !r.Confirmed {
		return r, err
	}
	e := events.PruneCompleted{Target: p.Name, Deleted: r.Deleted, Kept: r.Kept, Cutoff: r.Before.UTC().Format(time.RFC3339)}
	return r, s.Record(ctx, e, store.EventScope{})
}

// run is Run without the record.
func run(ctx context.Context, s *store.Store, p Pruner, o Options) (Report, error) {
	if o.Age == 0 {
		o.Age = p.DefaultAge
	}
	if o.Age < MinAge {
		return Report{}, fmt.Errorf("prune %s: the minimum age is %s", p.Name, FormatAge(MinAge))
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	if p.Minimises && o.Pseudonym == nil {
		return Report{}, fmt.Errorf("prune %s: needs the email pseudonyms' key (APP_KEY)", p.Name)
	}
	if o.Batch == 0 {
		o.Batch = 5000
		if p.Minimises {
			o.Batch = 1000
		}
	}
	r := Report{Pruner: p, Age: o.Age, Before: o.Now.Add(-o.Age), Confirmed: o.Confirm}
	prunable, kept, err := s.CountPrunable(ctx, p.Table, r.Before)
	r.Kept = kept
	if err != nil || !o.Confirm {
		r.Deleted = prunable
		return r, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		var n int
		var err error
		if p.Minimises {
			n, err = s.PseudonymiseEvents(ctx, r.Before, o.Batch, o.Pseudonym)
		} else {
			n, err = s.Prune(ctx, p.Table, r.Before, o.Batch)
		}
		r.Deleted += n
		// A short batch means nothing older is left (rows another run holds
		// are that run's to delete).
		if err != nil || n < o.Batch {
			return r, err
		}
	}
}
