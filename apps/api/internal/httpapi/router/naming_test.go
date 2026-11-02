package router

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"lewcontext/api/internal/httpapi/apitest"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Every API route is a controller's action (docs/naming.md#controllers-internalhttpapi):
// registered with <controller>.<Action>, where the controller is a value
// like workspace.Projects{Server: s}, and with GET, POST, PATCH or DELETE
// (update is always PATCH; PUT isn't used). So a new resource route goes
// through a controller, and inside a workspace through ws.Can, where the
// role × route matrix covers it.

// routeMethods are the methods a route may use.
var routeMethods = []string{"GET", "POST", "PATCH", "DELETE"}

// notControllers are the registrations that aren't a controller's action,
// each with its reason. Pages (pageRoutes) are the web app, not the API.
var notControllers = map[string]string{
	"GET /api/health":      "readiness for load balancers: no resource",
	"GET /api/health/live": "liveness for orchestrators: no resource",
	"/api/admin/":          "any other path or method under /api/admin answers as an unknown route",
}

// registration is one route as routes.go registers it: the function it's
// in, its method and path (empty method for a pattern without one), and
// the controller actions its handler names (controller.Action).
type registration struct {
	function string
	method   string
	path     string
	actions  []string
	line     int
}

// registers are the calls that register a route: ServeMux's, and the
// route groups' (routeGroup).
var registers = []string{"Handle", "HandleFunc", "Can", "Role", "Action", "Housekeeping", "OwnSessions"}

// routeRegistrations reads every route registered by a function (not a
// method) in source. A controller is a local assigned a composite literal
// of an area's type (workspace.Projects{Server: s}).
func routeRegistrations(source string) ([]registration, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "routes.go", source, 0)
	if err != nil {
		return nil, err
	}
	var found []registration
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil {
			continue
		}
		controllers := controllerValues(fn.Body)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !slices.Contains(registers, sel.Sel.Name) || len(call.Args) < 2 {
				return true
			}
			r := registration{function: fn.Name.Name, line: fset.Position(call.Pos()).Line}
			first, ok := stringLiteral(call.Args[0])
			if !ok {
				r.method = "unreadable"
			} else if receiver, _ := sel.X.(*ast.Ident); receiver != nil && receiver.Name == "mux" {
				// ServeMux: "METHOD /path", or a path alone.
				if method, path, cut := strings.Cut(first, " "); cut {
					r.method, r.path = method, path
				} else {
					r.path = first
				}
			} else {
				r.method = first
				r.path, _ = stringLiteral(call.Args[1])
			}
			r.actions = controllerActions(call.Args[len(call.Args)-1], controllers)
			found = append(found, r)
			return false
		})
	}
	return found, nil
}

func stringLiteral(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

// controllerValues are a function's locals holding a controller: x :=
// pkg.Type{…}.
func controllerValues(body *ast.BlockStmt) map[string]bool {
	values := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != len(assign.Rhs) {
			return true
		}
		for i, rhs := range assign.Rhs {
			lit, ok := rhs.(*ast.CompositeLit)
			if !ok {
				continue
			}
			if _, ok := lit.Type.(*ast.SelectorExpr); !ok {
				continue
			}
			if name, ok := assign.Lhs[i].(*ast.Ident); ok {
				values[name.Name] = true
			}
		}
		return true
	})
	return values
}

// controllerActions are the controller.Action expressions in a handler,
// however it's wrapped (s.LimitAuth(http.HandlerFunc(x.Y)), s.Limit(l, x.Y)).
func controllerActions(handler ast.Expr, controllers map[string]bool) []string {
	var actions []string
	ast.Inspect(handler, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && controllers[x.Name] {
			actions = append(actions, x.Name+"."+sel.Sel.Name)
		}
		return true
	})
	return actions
}

// badRoute says what's wrong with a registration, or "".
func badRoute(r registration) string {
	where := strings.TrimSpace(r.method + " " + r.path)
	switch {
	case r.method == "unreadable":
		return fmt.Sprintf("routes.go:%d: a route this check can't read: write its method and path as string literals", r.line)
	case r.function == "pageRoutes":
		if r.method != "GET" {
			return fmt.Sprintf("page %s: pages answer GET only", where)
		}
		return ""
	case r.method == "PUT":
		return fmt.Sprintf("route %s: update is always PATCH, never PUT; see docs/naming.md#controllers-internalhttpapi", where)
	case r.method != "" && !slices.Contains(routeMethods, r.method):
		return fmt.Sprintf("route %s: routes use GET, POST, PATCH or DELETE; see docs/naming.md#controllers-internalhttpapi", where)
	case notControllers[where] != "":
		return ""
	case len(r.actions) != 1:
		return fmt.Sprintf("route %s: register a controller's action (x := area.Things{Server: s}; x.Index), not %d of them; see docs/naming.md#controllers-internalhttpapi", where, len(r.actions))
	}
	return ""
}

func TestRouteNames(t *testing.T) {
	source := string(apitest.Must(os.ReadFile("routes.go"))(t))
	routes, err := routeRegistrations(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) < 50 {
		t.Fatalf("found only %d routes in routes.go", len(routes))
	}
	listed := map[string]bool{}
	for _, r := range routes {
		if problem := badRoute(r); problem != "" {
			t.Error(problem)
		}
		listed[strings.TrimSpace(r.method+" "+r.path)] = true
	}
	for route := range notControllers {
		if !listed[route] {
			t.Errorf("%s is listed as not a controller's action, but isn't registered", route)
		}
	}
}

// The check itself, on routes written the ways routes.go writes them.
func TestRouteRegistrations(t *testing.T) {
	source := `package router
func apiRoutes(s *httpapi.Server) {
	projects := workspace.Projects{Server: s}
	mux.Handle("GET /api/things", s.RequireSession(http.HandlerFunc(projects.Index)))
	mux.HandleFunc("PUT /api/things", projects.Update)
	mux.HandleFunc("GET /api/loose", s.Loose)
	mux.HandleFunc(pattern, projects.Show)
	ws.Can("POST", "/projects", auth.ProjectStore, s.Limit(httpapi.ProjectPerUser, projects.Store))
	ws.HandleFunc("OPTIONS", "/projects", projects.Index)
}
func pageRoutes(s *httpapi.Server) {
	mux.Handle("GET /", s.Pages)
	mux.Handle("POST /app", s.Pages)
}
func (g routeGroup) Handle(method, path string, h http.Handler) { g.mux.Handle(method+" "+path, h) }
`
	routes, err := routeRegistrations(source)
	if err != nil {
		t.Fatal(err)
	}
	var problems []string
	for _, r := range routes {
		problems = append(problems, badRoute(r))
	}
	want := []string{
		"",
		"route PUT /api/things: update is always PATCH, never PUT; see docs/naming.md#controllers-internalhttpapi",
		"route GET /api/loose: register a controller's action (x := area.Things{Server: s}; x.Index), not 0 of them; see docs/naming.md#controllers-internalhttpapi",
		"routes.go:7: a route this check can't read: write its method and path as string literals",
		"",
		"route OPTIONS /projects: routes use GET, POST, PATCH or DELETE; see docs/naming.md#controllers-internalhttpapi",
		"",
		"page POST /app: pages answer GET only",
	}
	if !slices.Equal(problems, want) {
		t.Fatalf("problems:\n%q\nwant\n%q", problems, want)
	}
}
