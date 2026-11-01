package monitor

import (
	"context"
	"slices"
)

// Page is a Monitoring page: its signals, and how its sections are read
// over a window.
type Page struct {
	Name     string
	Signals  func() []Signal
	Sections func(ctx context.Context, env Env, w Window) (any, error)
}

// pages are the pages built, in the sidebar's order (the plan ships them
// one by one).
var pages = []Page{
	{Name: PageOverview, Signals: overviewSignals, Sections: overviewPage},
	{Name: PageSecurity, Signals: func() []Signal { return SecuritySignals }, Sections: securityPage},
	{Name: PageLimits, Signals: func() []Signal { return LimitsSignals }, Sections: limitsPage},
}

// PageNames are the pages built, in order.
func PageNames() []string {
	names := make([]string, len(pages))
	for i, p := range pages {
		names[i] = p.Name
	}
	return names
}

// FindPage finds a page by name; false for any other.
func FindPage(name string) (Page, bool) {
	i := slices.IndexFunc(pages, func(p Page) bool { return p.Name == name })
	if i < 0 {
		return Page{}, false
	}
	return pages[i], true
}

// View is a page as its endpoint answers: its window, its signals as read
// at env.Now, and its sections over the window.
type View struct {
	Page     string     `json:"page"`
	Window   WindowEcho `json:"window"`
	Signals  []Result   `json:"signals"`
	Sections any        `json:"sections"`
}

// Show reads page p over w.
func Show(ctx context.Context, env Env, p Page, w Window) (View, error) {
	signals, err := ReadAll(ctx, env, p.Signals())
	if err != nil {
		return View{}, err
	}
	sections, err := p.Sections(ctx, env, w)
	if err != nil {
		return View{}, err
	}
	return View{Page: p.Name, Window: w.Echo(), Signals: signals, Sections: sections}, nil
}

// Signals are every signal defined, for the docs' list and the checks
// (TestSignalsAreWellFormed).
func Signals() []Signal {
	return append(append(append([]Signal{}, SecuritySignals...), LimitsSignals...), OverviewSignals...)
}
