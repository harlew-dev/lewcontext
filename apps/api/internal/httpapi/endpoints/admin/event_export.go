package admin

import (
	"context"
	"encoding/csv"
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/store"
	"net/http"
	"strings"
	"time"
	"unicode"
)

// EventExport is the Events list as CSV (GET /api/admin/events/export,
// docs/api/admin.md#events): the filters the page uses, the newest
// MaxEventExportRows rows, for an auditor or an incident. A read, so any
// admin may; recorded as admin.event.exported with the filters and how
// many rows it held. Its query takes one of the dashboard's summary slots
// (httpapi.Summaries), so two exports can't hold the database together,
// and has their deadline (Summaries.Deadline): past it the answer is 503,
// narrow the filters. The slot is let go before the file is written, so a
// slow download holds nothing.
type EventExport struct{ *httpapi.Server }

// MaxEventExportRows is the most rows one export holds; past it the answer
// says so (X-Export-Truncated: true), and a narrower window gets the rest.
const MaxEventExportRows = 10_000

// eventExportBusy answers an export that found no free slot, and
// eventExportTooSlow one whose query ran past the deadline.
const (
	eventExportBusy    = "Exports are busy. Try again in a moment."
	eventExportTooSlow = "The export took too long. Narrow the filters and try again."
)

// eventExportColumns are the CSV's header.
var eventExportColumns = []string{"time", "event", "name", "failed", "actor", "actor_email", "user", "user_email", "workspace", "workspace_name", "ip", "payload"}

func (h EventExport) Show(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	q, ok := adminEventQuery(r, now, h.EmailPseudonym)
	if !ok || q.summary || q.used.Before != "" || r.URL.Query().Has("perPage") {
		httpapi.Fail(w, 400, "Check the filters.")
		return
	}
	admin := httpapi.Admin(r).User.PublicID
	page, err := h.read(r.Context(), admin, q.filter)
	switch {
	case errors.Is(err, errEventExportBusy):
		w.Header().Set("Retry-After", "1")
		httpapi.Fail(w, 503, eventExportBusy)
		return
	case errors.Is(err, errEventExportTooSlow):
		httpapi.Fail(w, 503, eventExportTooSlow)
		return
	case errors.Is(err, store.ErrNoCursor):
		httpapi.Fail(w, 400, "Check the filters.")
		return
	case err != nil:
		httpapi.InternalError(w, r, err)
		return
	}
	// Recorded before a byte is sent: an export that can't be recorded
	// isn't served.
	e := events.AdminEventExported{Admin: admin, Filters: q.used, Rows: len(page.Data)}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="events-`+now.Format("2006-01-02")+`.csv"`)
	if page.Next != nil {
		w.Header().Set("X-Export-Truncated", "true")
	}
	out := csv.NewWriter(w)
	out.UseCRLF = true
	_ = out.Write(eventExportColumns)
	for _, event := range page.Data {
		_ = out.Write(eventExportRow(event))
	}
	out.Flush()
}

var (
	errEventExportBusy    = errors.New("event export busy")
	errEventExportTooSlow = errors.New("event export too slow")
)

// read runs the export's query in a summary slot, within the summaries'
// deadline, and lets the slot go once it has the rows.
func (h EventExport) read(ctx context.Context, admin string, filter store.AdminEventFilter) (store.AdminEventPage, error) {
	release, ok := h.Summaries.Acquire(ctx, admin)
	if !ok {
		if err := ctx.Err(); err != nil {
			return store.AdminEventPage{}, err
		}
		return store.AdminEventPage{}, errEventExportBusy
	}
	defer release()
	bounded, cancel := context.WithTimeout(ctx, h.Summaries.Deadline)
	defer cancel()
	page, err := h.Repo.IndexAdminEvents(bounded, filter, MaxEventExportRows)
	if err != nil && ctx.Err() == nil && bounded.Err() != nil {
		return store.AdminEventPage{}, errEventExportTooSlow
	}
	return page, err
}

// eventExportRow is one event as a CSV row, every cell made safe for a
// spreadsheet (csvCell).
func eventExportRow(e store.AdminEvent) []string {
	ip := ""
	if e.IP != nil {
		ip = *e.IP
	}
	failed := "false"
	if e.Failed {
		failed = "true"
	}
	actor, actorEmail := exportRef(e.Actor)
	user, userEmail := exportRef(e.User)
	workspace, workspaceName := "", ""
	if e.Workspace != nil {
		workspace, workspaceName = e.Workspace.PublicID, e.Workspace.Name
	}
	row := []string{e.CreatedAt.UTC().Format(time.RFC3339Nano), e.PublicID, e.Name, failed, actor, actorEmail, user, userEmail, workspace, workspaceName, ip, string(e.Payload)}
	for i, cell := range row {
		row[i] = csvCell(cell)
	}
	return row
}

// exportRef is an account's public ID and email, or blanks.
func exportRef(ref *store.AdminRef) (string, string) {
	if ref == nil {
		return "", ""
	}
	return ref.PublicID, ref.Email
}

// csvCell keeps a spreadsheet from reading a cell as a formula, as the
// Admins page's CSV does: a cell starting (after any spaces) with =, +, -
// or @, or with a tab or carriage return, starts with an apostrophe.
// Quoting is encoding/csv's.
func csvCell(value string) string {
	if strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") {
		return "'" + value
	}
	trimmed := strings.TrimLeftFunc(value, unicode.IsSpace)
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}
