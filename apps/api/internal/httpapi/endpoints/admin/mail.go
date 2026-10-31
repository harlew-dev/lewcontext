package admin

import (
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
	"time"
)

// Mail is the admin dashboard's Mail summary,
// /api/admin/mail, a singleton; the deliveries list is
// Deliveries'. Every read is recorded (admin.mail.viewed,
// admin.delivery.listed).
type Mail struct{ *httpapi.Server }

// mailWindows are the summary's windows, by their query value.
var mailWindows = map[string]time.Duration{
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
}

// defaultMailWindow is the summary's window when none is asked for.
const defaultMailWindow = "24h"

// adminMailDriver is how mail leaves, from the config: never the SMTP
// username or password.
type adminMailDriver struct {
	Driver      string `json:"driver"`
	Host        string `json:"host,omitempty"`
	Port        int    `json:"port,omitempty"`
	TLS         string `json:"tls,omitempty"`
	FromAddress string `json:"fromAddress,omitempty"`
	FromName    string `json:"fromName,omitempty"`
}

// adminMail is the Mail page's summary.
type adminMail struct {
	Window string          `json:"window"`
	Driver adminMailDriver `json:"driver"`
	store.AdminMailSummary
}

func (h Mail) driver() adminMailDriver {
	c := h.Config.Mail
	d := adminMailDriver{Driver: c.MailDriver}
	if c.MailDriver == config.MailSMTP {
		d.Host, d.Port, d.TLS = c.SMTP.Host, c.SMTP.Port, c.SMTP.TLS
	}
	if c.MailDriver != config.MailOff {
		d.FromAddress, d.FromName = c.MailFromAddress, c.MailFromName
	}
	return d
}

func (h Mail) Show(w http.ResponseWriter, r *http.Request) {
	params, ok := input.Query(r, "window")
	if !ok {
		httpapi.Fail(w, 400, "Invalid query parameters.")
		return
	}
	window := params["window"]
	if window == "" {
		window = defaultMailWindow
	}
	length, ok := mailWindows[window]
	if !ok {
		httpapi.Fail(w, 400, "Check the window.")
		return
	}
	summary, err := h.Repo.FindAdminMailSummary(r.Context(), time.Now().Add(-length), listeners(h.Server))
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	e := events.AdminMailViewed{Admin: httpapi.Admin(r).User.PublicID, Window: window}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, err)
		return
	}
	httpapi.JSON(w, 200, adminMail{Window: window, Driver: h.driver(), AdminMailSummary: summary})
}
