package httpapi

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// The layout of internal/httpapi (docs/naming.md#controllers): the core at
// the top, one package per area under endpoints/, one controller per file,
// and areas that never import each other.

// modulePath is this tree's import path.
const modulePath = "lewcontext/api/internal/httpapi"

// supportPackages are the folders that aren't areas, each with its reason. A
// new area is a new folder under endpoints/; anything else is listed here.
var supportPackages = map[string]string{
	"router":    "the one registry of routes and pages; it imports every area",
	"apitest":   "the helpers integration tests share; only tests import it",
	"clientip":  "a leaf the core imports: resolving the client's address",
	"input":     "a leaf the core imports: query parameters, paging, public IDs",
	"ratelimit": "a leaf the core imports: what a limit is, the in-memory counter",
}

// crudActions are a controller's methods (docs/naming.md#controllers).
var crudActions = []string{"Index", "Show", "Store", "Update", "Destroy"}

// namedActions are the other methods a controller may have, each with its
// reason. Check first whether a new one is really a CRUD action of another
// resource.
var namedActions = map[string]string{
	"Register":      "/api/auth/register: the account's named actions",
	"Login":         "/api/auth/login and the admin door's /login",
	"LoginMFA":      "/api/auth/login/mfa and the admin door's /login/mfa",
	"LoginMFASetup": "the admin door's /login/mfa/setup",
	"Logout":        "/api/auth/logout and the admin door's /logout",
	"RevokeOthers":  "DELETE /api/auth/sessions: signing out other sessions",
	"Verify":        "/api/auth/password/verify and /register/verify",
	"Resend":        "/api/auth/register/resend",
	"Check":         "the checks of a link or sign-up: /invitations/check, /password-reset/check, /register/check, /email-blocks/check",
	"Request":       "POST /api/auth/password-reset: asking for a link",
	"Reset":         "PATCH /api/auth/password-reset: setting the password with a link",
	"Setup":         "/api/auth/mfa/setup: a new secret before MFA is on",
	"Accept":        "/api/invitations/accept",
	"Me":            "the admin door's /me, its session",
	"Catalogue":     "the admin filters' names, /api/admin/catalogue",
	"Retry":         "the admin dashboard's POST /deliveries/{event}/{listener}/retry and /deliveries/retry: queuing failed deliveries again",
}

// sourcePackage is one folder of the tree: its path under httpapi ("" for the
// core) and its non-test files by name.
type sourcePackage struct {
	dir   string
	files map[string]string
}

// layoutProblems lists every way pkgs break the layout, with how to fix it.
func layoutProblems(pkgs []sourcePackage) []string {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...)+"; see docs/naming.md#controllers")
	}
	for _, pkg := range pkgs {
		kind := folderKind(pkg.dir)
		if kind == "" {
			add("folder %q: put controllers in endpoints/<area>, or list it in supportPackages with its reason", pkg.dir)
			continue
		}
		if kind == "endpoints" && len(pkg.files) > 0 {
			add("endpoints/ holds only area folders, not Go files")
		}
		for _, name := range sortedKeys(pkg.files) {
			file, err := parser.ParseFile(token.NewFileSet(), name, pkg.files[name], 0)
			if err != nil {
				add("%s/%s: %v", pkg.dir, name, err)
				continue
			}
			for _, problem := range importProblems(pkg.dir, kind, file) {
				add("%s/%s: %s", pkg.dir, name, problem)
			}
			for _, problem := range declarationProblems(kind, name, file) {
				add("%s/%s: %s", pkg.dir, name, problem)
			}
		}
	}
	return problems
}

// folderKind is what a folder under httpapi is: "core", "area", "endpoints",
// a support package's name, or "" for a folder that isn't allowed.
func folderKind(dir string) string {
	switch {
	case dir == "":
		return "core"
	case dir == "endpoints":
		return "endpoints"
	case strings.HasPrefix(dir, "endpoints/"):
		return "area"
	}
	if _, ok := supportPackages[dir]; ok {
		return dir
	}
	return ""
}

// areaOf is an area package's area: the folder under endpoints/.
func areaOf(dir string) string {
	rest, _ := strings.CutPrefix(dir, "endpoints/")
	area, _, _ := strings.Cut(rest, "/")
	return area
}

// importProblems are file's imports that cross the layout: an area importing
// another area, the core or a leaf importing an area or the router, apitest
// importing the server, and anything but a test importing apitest.
func importProblems(dir, kind string, file *ast.File) []string {
	var problems []string
	for _, spec := range file.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		target, ok := strings.CutPrefix(path, modulePath)
		if !ok {
			continue
		}
		target = strings.TrimPrefix(target, "/")
		targetKind := folderKind(target)
		switch {
		case targetKind == "apitest":
			problems = append(problems, "imports apitest, which only tests may import")
		case kind == "area" && targetKind == "area" && areaOf(target) != areaOf(dir):
			problems = append(problems, fmt.Sprintf("the %s area imports the %s area; move what both need into the core or a leaf", areaOf(dir), areaOf(target)))
		case kind != "area" && kind != "router" && (targetKind == "area" || targetKind == "router"):
			problems = append(problems, fmt.Sprintf("imports %s; only the router imports areas", path))
		case kind == "apitest" && (targetKind == "core" || targetKind == "router"):
			problems = append(problems, fmt.Sprintf("apitest imports %s, so tests inside it couldn't use apitest", path))
		}
	}
	return problems
}

// declarationProblems are file's controllers and exported types out of
// place, and controller methods that aren't actions.
func declarationProblems(kind, name string, file *ast.File) []string {
	var problems []string
	controllers := map[string]bool{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			t := spec.(*ast.TypeSpec)
			controller := embedsServer(t)
			if controller {
				controllers[t.Name.Name] = true
			}
			switch {
			case controller && kind != "area":
				problems = append(problems, fmt.Sprintf("controller %s belongs in an area package under endpoints/", t.Name.Name))
			case kind == "area" && t.Name.IsExported() && !controller:
				problems = append(problems, fmt.Sprintf("exported type %s isn't a controller (a struct embedding *httpapi.Server); unexport it", t.Name.Name))
			case kind == "area" && controller && name != snakeCase(t.Name.Name)+".go":
				problems = append(problems, fmt.Sprintf("controller %s belongs in %s.go, one controller per file", t.Name.Name, snakeCase(t.Name.Name)))
			}
		}
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || !fn.Name.IsExported() {
			continue
		}
		receiver := receiverName(fn)
		if !controllers[receiver] {
			continue
		}
		method := fn.Name.Name
		if !slices.Contains(crudActions, method) && namedActions[method] == "" {
			problems = append(problems, fmt.Sprintf("%s.%s: a controller's methods are Index, Show, Store, Update, Destroy or a named action listed in namedActions", receiver, method))
		}
		if !isHandler(fn.Type) {
			problems = append(problems, fmt.Sprintf("%s.%s: a controller's actions are func(http.ResponseWriter, *http.Request); unexport helpers", receiver, method))
		}
	}
	return problems
}

// embedsServer reports whether t is a struct embedding *Server or
// *httpapi.Server: a controller.
func embedsServer(t *ast.TypeSpec) bool {
	s, ok := t.Type.(*ast.StructType)
	if !ok {
		return false
	}
	for _, field := range s.Fields.List {
		star, ok := field.Type.(*ast.StarExpr)
		if len(field.Names) != 0 || !ok {
			continue
		}
		switch x := star.X.(type) {
		case *ast.Ident:
			if x.Name == "Server" {
				return true
			}
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && id.Name == "httpapi" && x.Sel.Name == "Server" {
				return true
			}
		}
	}
	return false
}

// receiverName is the type a method is declared on.
func receiverName(fn *ast.FuncDecl) string {
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// isHandler reports whether f is func(http.ResponseWriter, *http.Request).
func isHandler(f *ast.FuncType) bool {
	if f.Results != nil && len(f.Results.List) > 0 {
		return false
	}
	var types []string
	for _, p := range f.Params.List {
		for range max(len(p.Names), 1) {
			types = append(types, typeString(p.Type))
		}
	}
	return slices.Equal(types, []string{"http.ResponseWriter", "*http.Request"})
}
func typeString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return "*" + typeString(x.X)
	case *ast.SelectorExpr:
		return typeString(x.X) + "." + x.Sel.Name
	case *ast.Ident:
		return x.Name
	}
	return ""
}

// snakeCase is a type's file name: WorkspaceProjects is workspace_projects,
// MFA is mfa.
func snakeCase(name string) string {
	var b strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 && (unicode.IsLower(runes[i-1]) || i+1 < len(runes) && unicode.IsLower(runes[i+1])) {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// readTree reads every folder under root and its non-test Go files.
func readTree(t *testing.T, root string) []sourcePackage {
	t.Helper()
	byDir := map[string]*sourcePackage{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			rel = ""
		}
		if d.IsDir() {
			byDir[rel] = &sourcePackage{dir: rel, files: map[string]string{}}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." {
			dir = ""
		}
		byDir[dir].files[d.Name()] = string(source)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var pkgs []sourcePackage
	for _, dir := range slices.Sorted(maps.Keys(byDir)) {
		pkgs = append(pkgs, *byDir[dir])
	}
	return pkgs
}

func TestHTTPLayout(t *testing.T) {
	for _, problem := range layoutProblems(readTree(t, ".")) {
		t.Error(problem)
	}
}

// layoutProblems refuses each way the layout can drift.
func TestLayoutProblems(t *testing.T) {
	const controller = "package workspace\n\nimport (\n\t\"lewcontext/api/internal/httpapi\"\n\t\"net/http\"\n)\n\ntype Projects struct{ *httpapi.Server }\n\nfunc (h Projects) Index(w http.ResponseWriter, r *http.Request) {}\n"
	area := func(dir string, files map[string]string) []sourcePackage {
		return []sourcePackage{{dir: dir, files: files}}
	}
	if problems := layoutProblems(area("endpoints/workspace", map[string]string{"projects.go": controller})); len(problems) != 0 {
		t.Fatalf("a controller in its file is refused: %v", problems)
	}
	for _, c := range []struct {
		name string
		pkgs []sourcePackage
		want string
	}{
		{"a folder that isn't an area or listed", area("helpers", nil), `folder "helpers"`},
		{"Go files directly in endpoints/", area("endpoints", map[string]string{"x.go": "package endpoints\n"}), "only area folders"},
		{"a controller in the core", area("", map[string]string{"projects.go": "package httpapi\n\ntype Projects struct{ *Server }\n"}), "belongs in an area package"},
		{"a controller in the wrong file", area("endpoints/workspace", map[string]string{"members.go": controller}), "belongs in projects.go"},
		{"an exported type that isn't a controller", area("endpoints/workspace", map[string]string{"input.go": "package workspace\n\ntype ProjectInput struct{ Name string }\n"}), "isn't a controller"},
		{"a method that isn't an action", area("endpoints/workspace", map[string]string{"projects.go": controller + "\nfunc (h Projects) List(w http.ResponseWriter, r *http.Request) {}\n"}), "Projects.List"},
		{"an exported helper method", area("endpoints/workspace", map[string]string{"projects.go": controller + "\nfunc (h Projects) Show(id string) bool { return true }\n"}), "func(http.ResponseWriter, *http.Request)"},
		{"an area importing another", area("endpoints/workspace", map[string]string{"x.go": "package workspace\n\nimport \"" + modulePath + "/endpoints/account\"\n"}), "the workspace area imports the account area"},
		{"the core importing an area", area("", map[string]string{"x.go": "package httpapi\n\nimport \"" + modulePath + "/endpoints/account\"\n"}), "only the router imports areas"},
		{"a leaf importing the router", area("input", map[string]string{"x.go": "package input\n\nimport \"" + modulePath + "/router\"\n"}), "only the router imports areas"},
		{"code importing apitest", area("endpoints/admin", map[string]string{"x.go": "package admin\n\nimport \"" + modulePath + "/apitest\"\n"}), "only tests may import"},
		{"apitest importing the core", area("apitest", map[string]string{"x.go": "package apitest\n\nimport \"" + modulePath + "\"\n"}), "tests inside it couldn't use apitest"},
	} {
		problems := strings.Join(layoutProblems(c.pkgs), "\n")
		if !strings.Contains(problems, c.want) {
			t.Errorf("%s: want a problem with %q, got %q", c.name, c.want, problems)
		}
	}
	// A resource with sub-resources gets a folder inside its area, which may
	// import its area.
	sub := area("endpoints/workspace/project", map[string]string{"x.go": "package project\n\nimport \"" + modulePath + "/endpoints/workspace\"\n"})
	if problems := layoutProblems(sub); len(problems) != 0 {
		t.Fatalf("a resource folder importing its area is refused: %v", problems)
	}
	if got := snakeCase("WorkspaceProjects") + snakeCase("MFA") + snakeCase("EmailVerification"); got != "workspace_projectsmfaemail_verification" {
		t.Errorf("snakeCase: %s", got)
	}
}
