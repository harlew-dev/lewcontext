package store

import (
	"fmt"
	"lewcontext/api/internal/events"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode"
)

// Store methods start with a verb (docs/naming.md#store-methods-internalstore):
// the data verbs, or a domain verb that says more than create, update or
// delete. A new domain verb is added below with its reason, after the user
// agrees (docs/checks.md).

// storeVerbs are the data verbs every resource uses.
var storeVerbs = []string{"Index", "Find", "Create", "Update", "Delete", "Check"}

// domainVerbs are the other verbs a store method may start with, each with
// what it's for. Longer verbs come before their prefixes (StepUp, not Step).
var domainVerbs = map[string]string{
	"Accept":    "accepting an invitation, which joins its workspace",
	"Allow":     "a rate limit's counter: whether one more request is allowed",
	"Answer":    "answering an MFA challenge with a code",
	"Block":     "putting an email address on the block list",
	"Change":    "changing a password, which also ends other sessions",
	"Cleanup":   "removing expired rows the requests left behind",
	"Complete":  "completing a sign-up with its emailed code",
	"Count":     "counting rows for a number shown elsewhere: what pruning would delete, what revoke-all would sign out, the deliveries waiting, the monitoring signals",
	"Disable":   "turning MFA off",
	"Enable":    "turning MFA on, with the first recovery codes",
	"Flag":      "flagging an admin session that moved network",
	"Grant":     "granting admin from the CLI",
	"Issue":     "issuing a single-use emailed link or code",
	"Leave":     "a member leaving a workspace",
	"Prune":     "deleting old rows from log tables",
	"Record":    "recording what happened without changing app data: an event, the error collector's groups, a minute's metrics, a server's start and stop",
	"Remove":    "removing a member from a workspace",
	"Rename":    "renaming a workspace",
	"Resend":    "sending a pending sign-up's code again",
	"Requeue":   "an admin queuing failed deliveries again (the worker's own Retry is events.Queue's)",
	"Reset":     "resetting a password by link, or MFA from the CLI or the dashboard",
	"Revoke":    "revoking an invitation, admin from the CLI, or sessions from the dashboard or CLI",
	"Setup":     "existing; to fix: starting MFA setup (should be SetUp, a verb)",
	"StepUp":    "opening an admin session's step-up window",
	"Summarize": "summing rows into totals: the admin Events page's chart counts, the metrics' hours, the monitoring pages' charts",
	"Suspend":   "an admin suspending an account",
	"Unblock":   "taking an email address off the block list",
	"Unsuspend": "an admin lifting an account's suspension",
	"Verify":    "verifying an email address",
}

// badStoreMethods returns the names that don't start with a verb, each with
// the rule they break. queue holds the methods of an interface defined
// elsewhere, which keep its names.
func badStoreMethods(names, queue []string) []string {
	var bad []string
	for _, name := range names {
		if slices.Contains(queue, name) || startsWithVerb(name) {
			continue
		}
		bad = append(bad, fmt.Sprintf("store method %q: store methods start with a verb (%s, or a listed domain verb); see docs/naming.md#store-methods-internalstore", name, strings.Join(storeVerbs, ", ")))
	}
	return bad
}

// startsWithVerb reports whether name is a verb, or a verb and then a new
// word (CreateProject, not Creates or Indexer).
func startsWithVerb(name string) bool {
	verbs := slices.Clone(storeVerbs)
	for verb := range domainVerbs {
		verbs = append(verbs, verb)
	}
	for _, verb := range verbs {
		rest, ok := strings.CutPrefix(name, verb)
		if ok && (rest == "" || unicode.IsUpper(rune(rest[0]))) {
			return true
		}
	}
	return false
}

// badIndexResult returns why an Index method's results aren't a list, or
// "". An Index returns a page (Page[T], a cursor page: a struct with Data
// and Next), or a whole list ([]T) where nothing pages it, and an error.
func badIndexResult(name string, results []reflect.Type) string {
	if !strings.HasPrefix(name, "Index") {
		return ""
	}
	errorType := reflect.TypeFor[error]()
	if len(results) != 2 || results[1] != errorType {
		return fmt.Sprintf("store method %q: Index methods return a list and an error; see docs/naming.md#store-methods-internalstore", name)
	}
	list := results[0]
	if list.Kind() == reflect.Slice {
		return ""
	}
	if list.Kind() == reflect.Struct {
		data, hasData := list.FieldByName("Data")
		_, hasNext := list.FieldByName("Next")
		_, hasPages := list.FieldByName("Pages")
		if hasData && data.Type.Kind() == reflect.Slice && (hasNext || hasPages) {
			return ""
		}
	}
	return fmt.Sprintf("store method %q: Index methods return one page of a list (Page[T], a cursor page) or a list, not %s; see docs/naming.md#store-methods-internalstore", name, list)
}

// The *Store implements events.Queue, whose methods keep the interface's
// names.
var _ events.Queue = (*Store)(nil)

func TestStoreMethodNames(t *testing.T) {
	queueType := reflect.TypeFor[events.Queue]()
	var queue []string
	for method := range queueType.Methods() {
		queue = append(queue, method.Name)
	}
	storeType := reflect.TypeFor[*Store]()
	var names []string
	for method := range storeType.Methods() {
		names = append(names, method.Name)
		var results []reflect.Type
		for i := range method.Type.NumOut() {
			results = append(results, method.Type.Out(i))
		}
		if problem := badIndexResult(method.Name, results); problem != "" {
			t.Error(problem)
		}
	}
	if len(names) < 40 {
		t.Fatalf("found only %d store methods", len(names))
	}
	for _, problem := range badStoreMethods(names, queue) {
		t.Error(problem)
	}
	// A listed verb no method uses any more comes off the list.
	for verb := range domainVerbs {
		used := slices.ContainsFunc(names, func(name string) bool {
			rest, ok := strings.CutPrefix(name, verb)
			return ok && (rest == "" || unicode.IsUpper(rune(rest[0])))
		})
		if !used {
			t.Errorf("domain verb %s is listed, but no store method uses it: take it off domainVerbs", verb)
		}
	}
}

// The check itself: verbs pass, nouns and lookalikes don't.
func TestBadStoreMethods(t *testing.T) {
	good := []string{"IndexProjects", "FindUser", "CreateProject", "DeleteAccount", "CheckPasswordReset", "RenameWorkspace", "Allow", "StepUpAdminSession", "Claim"}
	if bad := badStoreMethods(good, []string{"Claim"}); len(bad) > 0 {
		t.Fatalf("refused: %v", bad)
	}
	for _, name := range []string{"ProjectList", "Projects", "Session", "Indexer", "Finds", "GetUser", "ListProjects", "Claim"} {
		bad := badStoreMethods([]string{name}, nil)
		if len(bad) != 1 || !strings.Contains(bad[0], "docs/naming.md#store-methods-internalstore") {
			t.Errorf("%s accepted: %v", name, bad)
		}
	}
	page := reflect.TypeFor[Page[Project]]()
	cursor := reflect.TypeFor[AdminEventPage]()
	errorType := reflect.TypeFor[error]()
	for name, results := range map[string][]reflect.Type{
		"IndexProjects":  {page, errorType},
		"IndexAdmins":    {reflect.TypeFor[[]Admin](), errorType},
		"IndexEvents":    {cursor, errorType},
		"FindAnything":   {reflect.TypeFor[int](), reflect.TypeFor[int]()},
		"IndexWithCount": {page, reflect.TypeFor[int](), errorType},
	} {
		want := !strings.HasPrefix(name, "IndexWith")
		if got := badIndexResult(name, results) == ""; got != want {
			t.Errorf("%s: accepted %v, want %v", name, got, want)
		}
	}
	for _, results := range [][]reflect.Type{{reflect.TypeFor[Project](), errorType}, {page}} {
		if badIndexResult("IndexProjects", results) == "" {
			t.Errorf("IndexProjects returning %v accepted", results)
		}
	}
}
