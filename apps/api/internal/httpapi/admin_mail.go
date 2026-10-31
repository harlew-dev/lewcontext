package httpapi

import (
	"errors"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Mail is the admin dashboard's Mail summary,
// /api/admin/mail, a singleton; the deliveries list is
// Deliveries'. Every read is recorded (admin.mail.viewed,
// admin.delivery.listed).
type Mail struct{ *Server }

// Deliveries are the admin dashboard's /api/admin/deliveries.
type Deliveries struct{ *Server }

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
		Fail(w, 400, "Invalid query parameters.")
		return
	}
	window := params["window"]
	if window == "" {
		window = defaultMailWindow
	}
	length, ok := mailWindows[window]
	if !ok {
		Fail(w, 400, "Check the window.")
		return
	}
	summary, err := h.Repo.FindAdminMailSummary(r.Context(), time.Now().Add(-length), h.listeners())
	if err != nil {
		InternalError(w, err)
		return
	}
	e := events.AdminMailViewed{Admin: Admin(r).User.PublicID, Window: window}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		InternalError(w, err)
		return
	}
	JSON(w, 200, adminMail{Window: window, Driver: h.driver(), AdminMailSummary: summary})
}

// deliveryCursor reads a deliveries cursor, evt_…:listener.
func deliveryCursor(raw string) (event, listener string, ok bool) {
	event, listener, found := strings.Cut(raw, ":")
	return event, listener, found && input.PublicID("evt", event) && listener != "" && len(listener) <= 100
}

// adminDeliveryQuery parses the deliveries list's query string.
func (h Deliveries) query(r *http.Request) (store.AdminDeliveryFilter, int, events.DeliveryFilters, bool) {
	var f store.AdminDeliveryFilter
	params, ok := input.Query(r, "status", "listener", "from", "to", "before", "perPage")
	if !ok {
		return f, 0, events.DeliveryFilters{}, false
	}
	perPage, ok := input.Int(params["perPage"], defaultEventsPerPage, maxEventsPerPage)
	valid := ok
	f.Status, f.Listener = params["status"], params["listener"]
	valid = valid && (f.Status == "" || store.DeliveryStatus(f.Status))
	valid = valid && (f.Listener == "" || slices.Contains(h.listeners(), f.Listener))
	if params["before"] != "" {
		f.BeforeEvent, f.BeforeListener, ok = deliveryCursor(params["before"])
		valid = valid && ok
	}
	f.From, ok = timeFilter(params["from"])
	valid = valid && ok
	f.To, ok = timeFilter(params["to"])
	valid = valid && ok
	used := events.DeliveryFilters{Status: f.Status, Listener: f.Listener, From: f.From, To: f.To, Before: params["before"]}
	return f, perPage, used, valid
}

func (h Deliveries) Index(w http.ResponseWriter, r *http.Request) {
	f, perPage, used, ok := h.query(r)
	if !ok {
		Fail(w, 400, "Check the filters.")
		return
	}
	page, err := h.Repo.IndexAdminDeliveries(r.Context(), f, perPage)
	if errors.Is(err, store.ErrNoCursor) {
		Fail(w, 400, "That page is no longer available. Start from the newest deliveries.")
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	e := events.AdminDeliveryListed{Admin: Admin(r).User.PublicID, Filters: used}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		InternalError(w, err)
		return
	}
	JSON(w, 200, page)
}
