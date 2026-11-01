package admin

import (
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
// many rows it held. It takes one of the dashboard's summary slots
// (httpapi.Summaries), so two exports can't hold the database together.
type EventExport struct{ *httpapi.Server }

// MaxEventExportRows is the most rows one export holds; past it the answer
// says so (X-Export-Truncated: true), and a narrower window gets the rest.
const MaxEventExportRows = 10_000

// eventExportBusy answers an export that found no free slot.
const eventExportBusy = "Exports are busy. Try again in a moment."

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
	release, ok := h.Summaries.Acquire(r.Context(), admin)
	if !ok {
		w.Header().Set("Retry-After", "1")
		httpapi.Fail(w, 503, eventExportBusy)
		return
	}
	defer release()
	page, err := h.Repo.IndexAdminEvents(r.Context(), q.filter, MaxEventExportRows)
	if errors.Is(err, store.ErrNoCursor) {
		httpapi.Fail(w, 400, "Check the filters.")
		return
	}
	if err != nil {
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
