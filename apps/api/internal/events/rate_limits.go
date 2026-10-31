package events

import "time"

// RateLimitReached: a rate limit refused its first request in a window, so a
// flood records one event per limit and window. Limit is the limit's name and
// the level reached, e.g. "auth:per-ip:block" or "project:per-user:block"
// (httpapi/limits.go). User is set when a signed-in user reached it, and
// Workspace when the request was inside a workspace. IP is the connection's
// address; forwarded headers aren't trusted. Window is in seconds.
type RateLimitReached struct {
	Limit     string    `json:"limit"`
	User      string    `json:"user,omitempty"`
	Workspace string    `json:"workspace,omitempty"`
	IP        string    `json:"ip"`
	Window    int       `json:"window"`
	ResetsAt  time.Time `json:"resetsAt"`
}

func (RateLimitReached) EventName() string { return "rate_limit.reached" }
