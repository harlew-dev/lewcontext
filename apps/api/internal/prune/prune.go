// Package prune deletes old rows from tables that grow without limit, such as
// the events audit trail, for the prune command (cmd/prune). It previews
// unless told to delete. Each target is a file here that registers a Pruner,
// with its queries in internal/store/prune_<target>.go; this file holds what
// they share: the list, the age parsing and the batched run.
package prune

import (
	"context"
	"fmt"
	"forma/api/internal/store"
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
	Batch   int           // rows per DELETE: 5000
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
	if r.Confirmed {
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
// what was deleted so far, alongside ctx's error.
func Run(ctx context.Context, s *store.Store, p Pruner, o Options) (Report, error) {
	if o.Age == 0 {
		o.Age = p.DefaultAge
	}
	if o.Age < MinAge {
		return Report{}, fmt.Errorf("prune %s: the minimum age is %s", p.Name, FormatAge(MinAge))
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	if o.Batch == 0 {
		o.Batch = 5000
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
		n, err := s.Prune(ctx, p.Table, r.Before, o.Batch)
		r.Deleted += n
		// A short batch means nothing older is left (rows another run holds
		// are that run's to delete).
		if err != nil || n < o.Batch {
			return r, err
		}
	}
}
