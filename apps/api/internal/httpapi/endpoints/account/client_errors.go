package account

import (
	"errors"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/observe"
	"net/http"
	"strings"
)

// ClientErrors takes the web app's own errors, POST /api/client-errors
// (router/routes.go): a page that crashed, or an error nothing caught, in a
// signed-in browser. Each is scrubbed and grouped with the server's
// (observe.Collector, source client); nothing is stored per report. In the
// account area, as the block links are, rather than an area of its own for
// one route.
type ClientErrors struct{ *httpapi.Server }

// maxClientErrorBody bounds a report: the message, a stack and the route.
const maxClientErrorBody = 4 << 10

// clientErrorInput is what the web app sends (lib/client-errors.ts). Never
// the page's URL or query: the route is the pattern of the page it was on.
type clientErrorInput struct {
	Message string `json:"message"`
	Stack   string `json:"stack"`
	Route   string `json:"route"`
	Release string `json:"release"`
}

const invalidClientError = "Send the error's message."

// validClientError needs a message; the rest is optional, and bounded by
// the body's size.
func validClientError(in *clientErrorInput) bool {
	in.Message = strings.TrimSpace(in.Message)
	return in.Message != ""
}

// clientRoutes are the web app's page routes (apps/web/src/routes.ts, the
// app build's), as the web app names them (clientRoutePatterns in
// lib/client-errors.ts; a test there checks all three match), and "*" for
// its not-found page. A route outside them is stored as unknownClientRoute,
// so a browser can't add labels of its own: the set of groups it can make
// is bounded by the routes and the scrubbed messages, and those by
// observe.MaxClientErrorGroups.
var clientRoutes = map[string]bool{
	"/":                                 true,
	"/auth/login":                       true,
	"/auth/register":                    true,
	"/auth/register/verify":             true,
	"/auth/forgot-password":             true,
	"/auth/forgot-password/:token":      true,
	"/auth/invitations/:token":          true,
	"/auth/email-blocks/:token":         true,
	"/app":                              true,
	"/app/onboarding":                   true,
	"/app/verify-email":                 true,
	"/app/settings":                     true,
	"/app/:workspace/dashboard":         true,
	"/app/:workspace/projects":          true,
	"/app/:workspace/projects/:project": true,
	"/app/:workspace/settings":          true,
	"/app/:workspace/settings/members":  true,
	"*":                                 true,
}

// unknownClientRoute is the route of a report naming none of clientRoutes
// (a tab left open across a release that changed them, or a made-up one).
const unknownClientRoute = "unknown"

// Store groups one error from the browser and answers 202: it's stored with
// the next flush, if at all (a group the collector has no room for is
// dropped). Signed in only, with the session's CSRF token; outside the
// write budget, under client-error:per-user (router/routes.go) and, once the
// body is valid, client-error:global.
func (h ClientErrors) Store(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxClientErrorBody)
	in, ok := httpapi.DecodeValid(w, r, validClientError, invalidClientError)
	if !ok {
		return
	}
	if !h.Allow(w, r, httpapi.ClientErrorGlobal, httpapi.ClientErrorGlobal.Everyone()) {
		return
	}
	route := unknownClientRoute
	if clientRoutes[in.Route] {
		route = in.Route
	}
	ctx := r.Context()
	observe.Report(ctx, observe.Problem{
		Source:    observe.SourceClient,
		Route:     route,
		RequestID: observe.RequestID(ctx),
		Err:       errors.New(in.Message),
		Stack:     []byte(in.Stack),
		Release:   in.Release,
	})
	w.WriteHeader(http.StatusAccepted)
}
