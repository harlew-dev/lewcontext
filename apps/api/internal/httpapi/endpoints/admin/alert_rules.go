package admin

import (
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/monitor"
	"lewcontext/api/internal/store"
	"net/http"
)

type AlertRules struct{ *httpapi.Server }

type alertRuleView struct {
	Signal          string        `json:"signal"`
	Title           string        `json:"title"`
	Meaning         string        `json:"meaning"`
	Page            string        `json:"page"`
	Unit            string        `json:"unit"`
	Warn            float64       `json:"warn"`
	Critical        float64       `json:"critical"`
	MinSample       int64         `json:"minSample"`
	WindowSeconds   int           `json:"windowSeconds"`
	From            monitor.Level `json:"from"`
	Cap             monitor.Level `json:"cap"`
	Levels          []string      `json:"levels"`
	EffectiveLevels []string      `json:"effectiveLevels"`
	ForSeconds      int           `json:"forSeconds"`
	CadenceSeconds  int           `json:"cadenceSeconds"`
	RecoverySeconds int           `json:"recoverySeconds"`
	RecoveryChecks  int           `json:"recoveryChecks"`
	Preference      string        `json:"preference"`
	Events          []string      `json:"events"`
	Ineffective     bool          `json:"ineffective"`
}

func (h AlertRules) Index(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) > 0 {
		httpapi.Fail(w, 400, "This resource takes no query parameters.")
		return
	}
	admin := httpapi.Admin(r).User
	prefs, err := h.Repo.IndexAdminAlertSubscriptions(r.Context(), admin.ID)
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	overrides := map[string]string{}
	kinds := map[string][]string{}
	for _, p := range prefs {
		overrides[p.Signal] = p.MinLevel
		kinds[p.Signal] = p.Events
	}
	out := []alertRuleView{}
	known := map[string]bool{}
	for _, rule := range monitor.AlertRules {
		signal, ok := monitor.FindSignal(rule.Signal)
		if !ok {
			continue
		}
		known[rule.Signal] = true
		v := alertRuleView{Signal: rule.Signal, Title: rule.Title, Meaning: rule.Meaning, Page: signal.Page, Unit: signal.Unit, Warn: signal.Warn, Critical: signal.Critical, MinSample: signal.MinSample, WindowSeconds: int(signal.Window.Seconds()), From: rule.From, Cap: rule.Cap, ForSeconds: int(rule.For.Seconds()), CadenceSeconds: 60, RecoverySeconds: 600, RecoveryChecks: 11, Levels: []string{}, EffectiveLevels: []string{}, Preference: overrides[rule.Signal], Events: nonNilStrings(kinds[rule.Signal])}
		if v.Title == "" {
			v.Title = signal.Label
		}
		if v.From == "" {
			v.From = monitor.LevelWarn
		}
		if v.Cap == "" {
			v.Cap = monitor.LevelCritical
		}
		if v.Preference == "" {
			v.Preference = "off"
		}
		if rule.Hourly {
			v.CadenceSeconds = 3600
			v.RecoverySeconds = 0
			v.RecoveryChecks = 1
		}
		available := map[string]bool{}
		if signal.Warn > 0 {
			level := rule.Level(monitor.LevelWarn)
			if level != monitor.LevelOK {
				available[string(level)] = true
			}
		}
		if signal.Critical > 0 {
			level := rule.Level(monitor.LevelCritical)
			if level != monitor.LevelOK {
				available[string(level)] = true
			}
		}
		recipient := store.AlertRecipient{Subscriptions: overrides}
		for _, level := range []string{"warn", "critical"} {
			if available[level] {
				v.Levels = append(v.Levels, level)
				if recipient.CoversLevel(rule.Signal, level) {
					v.EffectiveLevels = append(v.EffectiveLevels, level)
				}
			}
		}
		v.Ineffective = v.Preference != "off" && len(v.EffectiveLevels) == 0
		out = append(out, v)
	}
	retired := []store.AlertSubscription{}
	for _, p := range prefs {
		if !known[p.Signal] {
			retired = append(retired, p)
		}
	}
	if err := h.Repo.Record(r.Context(), events.AdminAlertRuleListed{Admin: admin.PublicID}, store.EventScope{ActorID: admin.ID}); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"data": out, "retired": retired})
}

// nonNilStrings is s, or an empty list for nil, so JSON says [] never null.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
