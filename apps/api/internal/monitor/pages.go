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
	Sections func(ctx context.Context, env Env, w Window) (Sections, error)
}

// pages are the pages built, in the sidebar's order (the plan ships them
// one by one).
var pages = []Page{
	{Name: PageOverview, Signals: overviewSignals, Sections: overviewPage},
	{Name: PageSecurity, Signals: func() []Signal { return SecuritySignals }, Sections: securityPage},
	{Name: PageLimits, Signals: func() []Signal { return LimitsSignals }, Sections: limitsPage},
	{Name: PageIPs, Signals: func() []Signal { return IPsSignals }, Sections: ipsPage},
	{Name: PageAccounts, Signals: func() []Signal { return AccountsSignals }, Sections: accountsPage},
	{Name: PageWorkspaces, Signals: func() []Signal { return WorkspacesSignals }, Sections: workspacesPage},
	{Name: PageUsage, Signals: func() []Signal { return UsageSignals }, Sections: usagePage},
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
// at env.Now, its sections over the window, and the sections that couldn't
// be read within their budget (by their key in sections).
type View struct {
	Page        string     `json:"page"`
	Window      WindowEcho `json:"window"`
	Signals     []Result   `json:"signals"`
	Sections    any        `json:"sections"`
	Unavailable []string   `json:"unavailable"`
}

// Show reads page p over w, each signal and section under env's budget
// (sections.go). ErrNothingRead when no part of it could be read.
func Show(ctx context.Context, env Env, p Page, w Window) (View, error) {
	signals, read, err := readSignals(ctx, env, p.Signals())
	if err != nil {
		return View{}, err
	}
	sections, err := p.Sections(ctx, env, w)
	if err != nil {
		return View{}, err
	}
	if read+sections.Read == 0 {
		return View{}, ErrNothingRead
	}
	return View{Page: p.Name, Window: w.Echo(), Signals: signals, Sections: sections.Value, Unavailable: sections.Unavailable}, nil
}

// Signals are every signal defined, for the docs' list and the checks
// (TestSignalsAreWellFormed).
func Signals() []Signal {
	var all []Signal
	for _, list := range [][]Signal{SecuritySignals, LimitsSignals, IPsSignals, AccountsSignals, WorkspacesSignals, UsageSignals, OverviewSignals} {
		all = append(all, list...)
	}
	return all
}
