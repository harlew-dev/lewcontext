package account

import (
	"encoding/json"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/store"
	"net/http"
	"time"
)

// DataExport is "Download my data", POST /api/auth/export
// (docs/compliance.md#download-my-data): everything the app keeps about the
// signed-in account that it may see, as a JSON file, after its password
// (LimitAuth and auth:per-account, as deleting the account checks it), at
// most export:per-user a day, recorded as auth.data_exported. Synchronous:
// the quotas bound its size, and nothing is stored or emailed. A suspended
// account has no session, so it can't ask; the operator's admin export
// can.
type DataExport struct{ *httpapi.Server }

// dataExportInput confirms the export with the account's password.
type dataExportInput struct {
	Password string `json:"password"`
}

func (h DataExport) Store(w http.ResponseWriter, r *http.Request) {
	var in dataExportInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !h.PasswordConfirmed(w, r, in.Password, events.PasswordStepExportData) {
		return
	}
	user := httpapi.Session(r).User
	if !h.Allow(w, r, httpapi.ExportPerUser, httpapi.ExportPerUser.Key(user.PublicID)) {
		return
	}
	export, err := h.Repo.FindAccountExport(r.Context(), user.ID)
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	// Recorded before a byte is sent: an export that can't be recorded
	// isn't served.
	if err := h.Repo.Record(r.Context(), events.DataExported{User: user.PublicID}, store.EventScope{UserID: user.ID}); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	writeDataExport(w, export)
}

// writeDataExport answers with export as a JSON file to download.
func writeDataExport(w http.ResponseWriter, export store.AccountExport) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="lewcontext-export-`+export.ExportedAt.Format(time.DateOnly)+`.json"`)
	w.WriteHeader(200)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(export)
}
