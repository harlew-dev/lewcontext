# kit: a Laravel-style Go framework built from this app

Status: **proposed 2026-11-01**, decisions to confirm. A future plan, not in the build order in [`plans/README.md`](../README.md): start it after the open admin plans (A8, A2, A9, A3) are committed, since they change many of the files it moves. It turns this app's core into a framework that other apps import, and this app into the first app built on it.

**The detailed plans live in the kit repo**, at `/home/homelab/dev/harlew-dev/kit/plans/`. Its `README.md` has the overview and a table of 23 implementation stages, and `features/` has one plan per stage, each listing the files in this repo that it ports. That's also where this repo's [convention checks](../features/convention-checks.md) and [CAPTCHA challenges](../features/captcha-challenges.md) plans now continue, rewritten for kit as stages 19 and 23. This file is lewcontext's side of the work: what moves out, what stays, and the design summary.

## Why

The app has grown a complete, tested base: Argon2id passwords, server sessions, CSRF, MFA, email verification, email blocks, workspaces with roles and invitations, rate limits, the audit trail with durable listeners, SMTP mail, the admin dashboard, the embedded React build with CSP hashes, and conventions that tests enforce. A new app should start with all of that and keep receiving fixes to it, without copying the code and merging updates by hand.

[Buffalo](https://gobuffalo.io), the obvious Go framework to base this on, was archived on 2024-02-24, and its design (server-rendered templates, the Pop ORM, cookie sessions) doesn't fit this stack. Laravel's split is the model instead:

| Laravel | kit |
| --- | --- |
| `laravel/framework`: the dependency each app updates | `github.com/harlew-dev/kit` (Go) and `@harlew-dev/kit-ui` (npm), from one repo with one version |
| `laravel/laravel`: the starter you copy once and own | `github.com/harlew-dev/kit-starter`, created with `kit new myapp` |
| Breeze, Jetstream: auth and teams | built into kit: auth, MFA, email verification, workspaces, invitations, admin |
| `artisan make:*`, `route:list` | `kit make:*`, `myapp routes`, `kit check` |

Three goals shape the design:

1. **One core to update.** A security fix lands once in kit; each app bumps a version and deploys.
2. **Laravel-style code in Go.** Buffalo-style routing and resources, single-action (invokable) controllers, Form Request–style requests, Eloquent-style models over Bun with a built-in paginator, typed events and listeners.
3. **No folder overflows.** One type per file, nested by feature area, so 50 features don't mean 50+ files in one folder. `kit check` enforces the layout from the first commit.

## Where the code is today

Paths are relative to this repo, `/home/homelab/dev/lewcontext`. Each row is the kit package the code moves to and the kit stage that moves it.

| Current code | Moves to | Stage |
| --- | --- | --- |
| `apps/api/internal/config/*` | `kit/config` | 2 |
| `apps/api/internal/database/database.go`, `internal/store/pagination.go`, the query patterns in `internal/store/*.go` | `kit/db` | 3 |
| `apps/api/internal/database/migrate.go`, `lock.go`, `apps/api/migrations/*` (all but `005_create_projects`), `internal/factory/*`, `internal/seed/*`, `cmd/migrate`, `cmd/seed` | `kit/migrate`, `kit/migrations`, `kit/factory`, `kit/seed` | 4 |
| `apps/api/internal/httpapi/server.go`, `middleware.go` (`Security`), `respond.go`, `client_ip.go`, `clientip/`, `context.go`, `router/routes.go`, `apitest/*`, `cmd/server/main.go` | `kit` (router, context, errors, server), `kit/kittest` | 5 |
| `apps/api/internal/httpapi/respond.go` (`Decode`), `input/*` | `kit` (`Validate`, rules) | 6 |
| `apps/api/internal/httpapi/endpoints/workspace/projects.go`, `internal/store/projects.go` | The pilot, then this repo's `app/` | 7, 21 |
| `apps/api/internal/events/*` (all but `projects.go`), `internal/store/events.go`, `deliveries.go`, `prune_events.go`, `internal/prune/*` | `kit/events` | 8 |
| `apps/api/internal/mail/*` | `kit/mail` | 9 |
| `apps/api/internal/httpapi/limits.go`, `ratelimit/*`, `internal/store/rate_limits.go`, `quotas.go`, `internal/events/rate_limits.go` | `kit/ratelimit` | 10 |
| `apps/api/internal/auth/{password,token}.go`, `internal/httpapi/{sessions,credentials}.go`, `endpoints/account/{sessions,me,password}.go`, `internal/store/{users,sessions}.go` | `kit/auth` | 11 |
| `apps/api/internal/httpapi/endpoints/account/{registrations,email_verification,password_resets,email_blocks}.go`, `internal/store/{registrations,email_verifications,password_resets,email_blocks}.go`, `internal/auth/email_code.go` | `kit/auth` | 12 |
| `apps/api/internal/auth/{totp,recovery,encrypt}.go`, `internal/httpapi/second_factor.go`, `endpoints/account/{mfa,recovery_codes}.go`, `internal/store/mfa.go` | `kit/mfa` | 13 |
| `apps/api/internal/auth/permissions.go`, `endpoints/workspace/{workspaces,members,membership,invitations}.go`, `internal/store/{workspaces,members,invitations}.go` | `kit/workspaces` | 14 |
| `apps/api/internal/auth/admin*.go`, `internal/httpapi/admin*.go`, `endpoints/admin/*` (all but `workspace_projects.go`), `internal/store/admin*.go`, `cmd/admin` | `kit/admin` | 15 |
| `apps/api/internal/web/*` | `kit/web` | 16 |
| `apps/web/src/components/{ui,layout}`, `components/app/*` (all but `projects`), `lib/*` (all but `api/projects.ts`), `pages/{auth,app,admin}` (all but the project pages), `styles/`, `vite.config.ts`, `web-build.ts`, `scripts/build-web.sh`, `scripts/check-app-build.sh` | `@harlew-dev/kit-ui` | 17 |
| `apps/api/cmd/*`, `Makefile` | `kit.Run` (the app binary's commands), `cmd/kit` | 18 |
| `Makefile`, `scripts/*`, `Dockerfile`, `compose.yaml`, `.env.example`, `.env.testing`, `AGENTS.md`, `docs/` | `harlew-dev/kit-starter`, and kit's `docs/` | 20 |

**What stays here as app code:** projects. That's `store/projects.go`, `endpoints/workspace/projects.go`, `events/projects.go`, the project permissions in `auth/permissions.go`, `ProjectPerUser`, `MaxWorkspaceProjects`, `endpoints/admin/workspace_projects.go`, migration `005_create_projects`, `factory/projects.go`, the Acme seed, and on the web side `pages/app/[workspace]/{dashboard,projects*}`, `components/app/projects`, `lib/api/projects.ts` and `types/projects.ts`. Kit stage 21 moves them into the app layout.

## Packages and repos

```
github.com/harlew-dev/kit                 one repo, one version tag (v1.4.0) for both packages
  go.mod                              Go module github.com/harlew-dev/kit
  kit.go router.go context.go …       package kit: Router, Context, Resource, errors, Controller
  db/                                 Query[T], Page[T], Transaction, the migration runner
  events/  mail/  config/  ratelimit/  web/  clientip/
  auth/                               passwords, sessions, CSRF, email verification, email blocks
  mfa/  workspaces/  admin/
  migrations/                         kit's own tables (users, sessions, workspaces, events, …)
  cmd/kit/                            the CLI
  docs/                               the framework's docs (moved from this repo's docs/)
  ui/                                 npm package @harlew-dev/kit-ui
    package.json
    src/                              components/ui, layouts, auth/settings/onboarding pages,
                                      session, workspace, HTTP client, SWR helpers, admin build
    vite.ts                           kit() Vite plugin: React Router, Tailwind, the admin build
github.com/harlew-dev/kit-starter         the starter app
```

- **One Go module, not `kit-auth` beside `kit`.** Auth runs through the router (sessions are middleware), rate limits (budgets per user), events, mail listeners, admin (required MFA) and kit's migrations. Separate modules would need paired releases and "kit-auth v1.3 needs kit ≥ v1.4" rules for nearly every change. Go links only the packages an app imports, so an app that never imports `kit/admin` doesn't carry it. Go package names can't contain `-`, so `kit-auth` would also need a different import name.
- **`kit-ui` in the same repo, same version.** The React side depends on the Go side's exact JSON (the session shape, the MFA steps, the paginator's `{data, pages: {current, total, perPage, items}}`). One tag means `kit v1.4.0` and `@harlew-dev/kit-ui 1.4.0` always match, and one change updates both.
- **Private to start:** `GOPRIVATE=github.com/harlew-dev/*` for Go, GitHub Packages for the `@harlew` npm scope, protected release tags.

## An app's layout

```
myapp/
  cmd/server/main.go                  kit.Run(app.New()). Nothing else.
  app/
    app.go                            bootstrap (bootstrap/app.php): config, providers, routes
    http/                             folders only: no Go files, so no package clashes with net/http
      controllers/
        controller.go                 package controllers: the base Controller
        welcome.go                    controllers.Welcome
        workspace/                    package workspace: the workspace's own actions
          show.go update.go destroy.go
          members/                    package members
            index.go update.go destroy.go
          project/                    package project
            index.go show.go store.go update.go destroy.go
            project_request.go
            tasks/                    package tasks
              index.go show.go store.go update.go destroy.go
              task_request.go
        admin/ …
      middleware/
    models/                           package models, flat (see "Models")
      project.go task.go
    events/
      events.go                       generated aliases: events.TaskCreated
      project/project_created.go project_updated.go project_deleted.go
      task/task_created.go task_updated.go task_deleted.go
    listeners/
      listener.go                     package listeners: the base Listener
      task/index_created_task.go index_updated_task.go remove_deleted_task.go index_task.go
    mail/                             the app's mail templates
    providers/
      event_provider.go               registers every listener (EventServiceProvider)
  routes/
    api.go admin.go                   package routes
  database/
    migrations/ factories/ seeders/
  web/                                the React app: pages, routes.ts (resources/js)
```

### Folder and file rules

These are what `kit check` enforces:

1. **A folder is a package** (Go's rule, not a choice). Every file in a folder shares one package, and the folder's last name is how other code refers to it.
2. **One exported type per file, and the file is that type's name in snake_case:** `task_created.go` holds `TaskCreated`, `notify_assignee.go` holds `NotifyAssignee`, `index.go` holds `Index`. An unexported helper shared by a folder gets its own file named after it (`index_task.go` holds `indexTask`).
3. **Controllers, requests, listeners and events nest by feature area** to any depth. Their folders never import each other's siblings, so nesting can't create import cycles.
4. **A parent folder either provides a base type its subfolders import, or imports its subfolders, never both** (that would be an import cycle). `controllers/` and `listeners/` provide bases; `events/` imports its subfolders for the aliases; registration lives in `providers/` and `routes/`.
5. **Folder size cap:** a folder fails `kit check` above 12 files, not counting `_test.go` files and generated files (decision 7).
6. **Name clashes are renamed at the import, using one pattern.** If a file imports two packages with the same last name (`controllers/workspace` and `controllers/admin/workspace`), it renames one to `<parent><name>` (`adminworkspace`). Renames apply only to the file that makes them, so admin routes in their own `routes/admin.go` almost never need one.

## Routing

kit's router is a thin layer over the standard library's `http.ServeMux`, which already matches methods and `{param}` paths. It adds groups, middleware, resources and a route table.

```go
// routes/api.go
package routes

import (
	"myapp/app/http/controllers/workspace"
	"myapp/app/http/controllers/workspace/members"
	"myapp/app/http/controllers/workspace/project"
	"myapp/app/http/controllers/workspace/project/tasks"
)

func API(r *kit.Router) {
	r.Group("/workspaces/{workspace}", func(r *kit.Router) {
		r.Use(kit.RequireSession, kit.RequireWorkspace)
		r.Get("", workspace.Show{})
		r.Patch("", workspace.Update{})
		r.Resource("members", kit.Resource{Index: members.Index{}, Update: members.Update{}, Destroy: members.Destroy{}})
		r.Resource("projects", kit.Resource{
			Index: project.Index{}, Show: project.Show{}, Store: project.Store{},
			Update: project.Update{}, Destroy: project.Destroy{},
		}, func(r *kit.Router) {
			r.Bind("project", kit.BindScoped[models.Project]("workspace_id"))
			r.Resource("tasks", kit.Resource{
				Index: tasks.Index{}, Show: tasks.Show{}, Store: tasks.Store{},
				Update: tasks.Update{}, Destroy: tasks.Destroy{},
			})
		})
	})
}
```

- **`Resource`** maps single-action controllers to routes, like Buffalo's `app.Resource` and Laravel's `Route::apiResource`. A field left empty registers no route (Laravel's `->only()`). The parameter is the singular name, `{project}`:

  ```
  GET    /api/workspaces/{workspace}/projects              project.Index
  POST   /api/workspaces/{workspace}/projects              project.Store
  GET    /api/workspaces/{workspace}/projects/{project}    project.Show
  PATCH  /api/workspaces/{workspace}/projects/{project}    project.Update
  DELETE /api/workspaces/{workspace}/projects/{project}    project.Destroy
  ```

  Methods stay GET, POST, PATCH and DELETE (no PUT), as [`docs/naming.md`](../../docs/naming.md) has them today.
- **kit registers its own routes** (`/api/auth/*`, workspaces, members, invitations, `/api/admin/*`) before calling the app's `routes.API`. An app adds routes and can't remove or replace kit's.
- **The route table is data.** `myapp routes` prints it (`route:list`). `myapp routes:ts` generates the web app's typed `paths`, so frontend paths can't drift from Go routes.
- **Checks when the server starts** (the router panics, like today's `ws` group): a mutating route without a permission, a create route (`Store`, or a listed named action) without its own rate limit, a route outside the listed public set that isn't behind `RequireSession`, an unscoped model binding under `{workspace}`, and a duplicate method and path.

## Controllers

Every controller is a struct with one `Handle` method, the Go version of Laravel's `__invoke`:

```go
// package kit
type Action interface {
	Handle(c *Context) error
}
```

The base controller works by **embedding**: Go has no inheritance, but an embedded struct's fields and methods are available on the outer type. That covers what a Laravel base controller is used for: shared dependencies and helpers. Methods can't be overridden the way PHP's can.

```go
// app/http/controllers/controller.go
package controllers

// Controller is embedded by every controller.
type Controller struct{ kit.Controller }
```

```go
// app/http/controllers/workspace/project/show.go
package project

// Show returns one project in the current workspace.
type Show struct{ controllers.Controller }

func (Show) Handle(c *kit.Context) error {
	p, err := models.Projects(c).InWorkspace(c.Workspace()).FindByPublicID(c.Param("project"))
	if err != nil {
		return err // not found: 404 "Project not found."
	}
	return c.JSON(200, p)
}
```

```go
// app/http/controllers/workspace/project/store.go
package project

// Store creates a project in the current workspace.
type Store struct{ controllers.Controller }

func (Store) Can() auth.Permission { return auth.ProjectStore }

func (Store) Handle(c *kit.Context) error {
	in, err := kit.Validate[ProjectRequest](c)
	if err != nil {
		return err // 422
	}
	p, err := models.CreateProject(c, c.Workspace(), in.Name, in.Description, in.Status, in.Color)
	if err != nil {
		return err
	}
	return c.JSON(201, p)
}
```

- **Names:** the folder names the resource and the type names the action: `project.Index`, `tasks.Store`. Go avoids repeating the package in the type (`tasks.TaskStore`), and routes read as `tasks.Index{}` (decision 5).
- **`Can()`** names the permission. The router reads it when the server starts and panics if a mutating route has none, keeping today's `ws.Can` guarantee. Read routes inside a workspace may name one too (`auth.WorkspaceMemberIndex`).
- **`kit.Context`** wraps the request and response: `c.Param`, `c.Query`, `c.JSON`, `c.NoContent`, `c.User()`, `c.Workspace()`, `c.Request()`, `c.Writer()`. It implements `context.Context`, so it passes straight to models and `db` calls. Standard `func(http.Handler) http.Handler` middleware still plugs in with `r.Use`.

### Errors

Controllers return errors instead of writing failures, as in Buffalo. One central handler turns them into responses, the closest Go gets to Laravel's `abort()` and exception handler:

| Returned | Response |
| --- | --- |
| `db.NotFound{Model: "Project"}` (what `First` and `Find…` return for no rows) | 404 `Project not found.` |
| `kit.Abort(403, "…")` and other `kit.HTTPError`s | that status and message |
| `kit.ValidationError` (from `kit.Validate`) | 422 with a message per field |
| `models.QuotaError` | 422 with the quota's message |
| anything else | 500 with the generic message; the error itself is only logged |

An error's text never reaches the response unless it's a `kit.HTTPError`, which is how public errors stay generic.

## Requests

The Go version of a Form Request: one struct that holds a request body, tidies it and validates it. It lives next to the controllers that use it; `create_task_request.go` and `update_task_request.go` replace it when the rules differ.

```go
// app/http/controllers/workspace/project/tasks/task_request.go
package tasks

// TaskRequest is the body of creating or updating a task.
type TaskRequest struct {
	Title    string `json:"title"`
	Notes    string `json:"notes"`
	Status   string `json:"status"`
	Assignee string `json:"assignee"`
}

// Prepare tidies the input before validation (prepareForValidation).
func (r *TaskRequest) Prepare() {
	r.Title = strings.TrimSpace(r.Title)
	r.Notes = strings.TrimSpace(r.Notes)
}

func (r TaskRequest) Rules() kit.Rules {
	return kit.Rules{
		"title":    {kit.Required, kit.Max(200)},
		"notes":    {kit.Max(2000)},
		"status":   {kit.Required, kit.In(models.TaskStatuses...)},
		"assignee": {kit.PublicID("usr")},
	}
}
```

`kit.Validate[T](c)` keeps today's decoder (`httpapi.Decode`): a 16 KiB body cap, unknown fields refused, wrong types refused. Then it calls `Prepare` and checks `Rules`. `Max` counts Unicode characters, as `validProject` does today. A test, and `kit check`, fail when a rule names a field that isn't one of the struct's JSON names.

Authorization stays on the controller (`Can()`), because the router checks it when the server starts and a GET or DELETE usually has no body.

## Models

Each model file holds the Bun struct, its constants and its query builder with scopes:

```go
// app/models/project.go
package models

var ProjectStatuses = []string{"Planned", "In progress", "Done"}

type Project struct {
	bun.BaseModel `bun:"table:projects,alias:p"`
	kit.Model                                    // id, public_id, created_at, updated_at
	WorkspaceID   int64  `bun:"workspace_id" json:"-"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Status        string `json:"status"`
	Color         string `json:"color"`
}

// ProjectQuery is the generic query builder plus the project's scopes.
type ProjectQuery struct{ *db.Query[Project] }

func Projects(ctx context.Context) ProjectQuery { return ProjectQuery{db.Model[Project](ctx)} }

func (q ProjectQuery) InWorkspace(w kit.Workspace) ProjectQuery {
	q.Where("p.workspace_id = ?", w.ID)
	return q
}

func (q ProjectQuery) Search(term string) ProjectQuery {
	if term != "" {
		q.Where(`(p.name ILIKE ? ESCAPE '\' OR p.description ILIKE ? ESCAPE '\')`, db.Contains(term), db.Contains(term))
	}
	return q
}
```

```go
// a controller
page, err := models.Projects(c).
	InWorkspace(c.Workspace()).
	Search(c.Query("q")).
	Sort(c.Query("sort"), models.ProjectSorts).
	Paginate(c)
```

- **`db.Query[T]`** gives every model `Where`, `With` (Bun's `Relation`, Eloquent's `with()`), `OrderBy`, `Sort` (an allowlisted sort key), `First`, `Get`, `FindByPublicID`, `Count`, `Exists` and `Paginate`.
- **`Paginate(c)`** reads `?page` and `?perPage` (validated and capped, as `input.Page` does today) and returns `db.Page[T]`: today's `store.Page[T]` and its JSON shape, unchanged.
- **Writes are functions in the model's file**, such as `models.CreateProject`. A write that needs a transaction, a quota check and an event does all three, as `store.CreateProject` does today:

  ```go
  func CreateProject(ctx context.Context, w kit.Workspace, name, description, status, color string) (Project, error) {
  	p := Project{WorkspaceID: w.ID, Name: name, Description: description, Status: status, Color: color}
  	err := db.Transaction(ctx, func(ctx context.Context) error {
  		if err := checkWorkspaceProjects(ctx, w); err != nil {
  			return err
  		}
  		if err := db.Insert(ctx, &p); err != nil {
  			return err
  		}
  		return events.Dispatch(ctx, events.ProjectCreated{Project: p.PublicID, Workspace: w.PublicID, Name: p.Name}, events.Scope{WorkspaceID: w.ID})
  	})
  	return p, err
  }
  ```

- **The database handle travels in the context.** `db.Model[T](ctx)` and `events.Dispatch(ctx, …)` use the context's transaction when inside `db.Transaction`, and the connection pool otherwise, like Laravel's `DB::transaction`. Queries inside the closure can't miss the transaction by accident (decision 10).
- **The models folder is flat.** Go refuses import cycles, and models reference each other (a project lists its tasks, a task points at its project), so they share one package. kit's own models (`User`, `Workspace`, `Member`, `Session`, …) live in kit, so an app's `models/` holds only its own. An app that grows a separate area gives it a package that depends one way only (`models/billing` uses `models.Workspace`; billing queries start from the billing side).

## Events

Events nest by area, one per file, with generated aliases so all other code writes `events.TaskCreated`:

```go
// app/events/task/task_created.go
package task

// TaskCreated is recorded when a task is added to a project.
type TaskCreated struct {
	Task    string `json:"task"`
	Project string `json:"project"`
	Title   string `json:"title"`
}

func (TaskCreated) EventName() string { return "task.created" }
```

```go
// app/events/events.go
// Code generated by kit make:event. DO NOT EDIT.
package events

import (
	"myapp/app/events/project"
	"myapp/app/events/task"
)

type (
	ProjectCreated = project.ProjectCreated
	TaskCreated    = task.TaskCreated
)
```

- **The aliases are the same type**, not copies (`=`). Creating events, typed listeners, `EventName()` and JSON all behave as if the nested name were used.
- **Event packages import nothing from the app** (no `models`, no `app/events`), or the aliases would form a cycle. Events hold public IDs and plain values, as [`docs/api/events.md`](../../docs/api/events.md) already requires. `kit check` enforces it.
- **`EventName()` is written by hand** (`task.created`, the `<resource>.<fact>` shape the `events.name` CHECK enforces), never derived from the file or folder, so a rename can't change names already stored.
- **The rules in `events.md` carry over:** recorded in the same transaction as the change, queued for listeners in that transaction, no sensitive fields (`TestPayloadsHoldNothingSensitive` runs on the app's events too), and the catalogue in the docs.

## Listeners

One listener per file, nested by area, registered in one provider. Every listener runs on today's durable delivery queue, the equivalent of Laravel's `ShouldQueue` with `afterCommit`: saved with the change, run by the worker, retried after 1 s, 10 s, 1 min, 10 min and 1 h.

```go
// app/listeners/listener.go
package listeners

// Listener is embedded by every listener.
type Listener struct {
	Mail *kit.Mailer
}
```

```go
// app/listeners/task/notify_assignee.go
package task

// NotifyAssignee emails a task's assignee when it's created.
type NotifyAssignee struct{ listeners.Listener }

func (NotifyAssignee) ListenerName() string { return "task.notify_assignee" }

func (l NotifyAssignee) Handle(ctx context.Context, e events.TaskCreated, d kit.Delivery) error {
	if e.Assignee == "" {
		return nil
	}
	user, err := kit.Users(ctx).FindByPublicID(e.Assignee)
	if err != nil {
		return err
	}
	return l.Mail.Send(ctx, mail.TaskAssigned{To: user, Task: e.Task, Title: e.Title})
}
```

```go
// app/providers/event_provider.go
package providers

func Events(bus *kit.Bus, base listeners.Listener) {
	kit.Listen[events.TaskCreated](bus, task.NotifyAssignee{Listener: base})
}
```

- **One event per listener.** Go has no method overloading, so a struct can't hold `Handle(TaskCreated)` and `Handle(TaskUpdated)`. A Laravel event subscriber becomes one listener per event, with shared code in an unexported helper in the same folder:

  ```
  app/listeners/task/
    index_created_task.go     IndexCreatedTask    TaskCreated
    index_updated_task.go     IndexUpdatedTask    TaskUpdated
    remove_deleted_task.go    RemoveDeletedTask   TaskDeleted
    index_task.go             indexTask           the shared helper
  ```

  Each is tracked under its own name in the deliveries table, so the admin Events page shows which one failed.
- **The event type is written at registration** (`kit.Listen[events.TaskCreated]`). Go can't infer it from the listener's `Handle` method, but the compiler checks that they match.
- **Registered in `providers/`, not discovered.** Go can't list a package's types at runtime. `kit make:listener task/NotifyAssignee --event=TaskCreated` writes the file and the registration line; `kit check` fails on a listener that isn't registered. kit registers its own listeners (password reset, invitations, email verification).
- **No synchronous listeners.** Work that must happen in the change's transaction belongs in the model's write function; everything else is queued. One way to react to an event.
- **`events.md`'s listener rules carry over:** `ListenerName()` stored with each delivery and written by hand, at-least-once delivery (use `d.EventID` to do something only once), a 30-second timeout through `ctx`, no ordering, and email sent only from listeners.

## Migrations

- **kit ships its own migrations** (`kit/migrations`, embedded) and records them in `kit_migrations`. An app's migrations run after kit's and are recorded in `bun_migrations`, so their version numbers can never collide. `myapp migrate` runs both under the existing advisory lock; `migrate-status` shows both.
- **Append-only from the first published version.** Once an app holds data on a kit version, kit's migrations are never edited. This ends the squash-while-in-development rule in AGENTS.md for kit's tables; an app may keep squashing its own until it has data.
- **Factories:** kit ships factories for its tables (`kit/factory`), and `TestEveryTableHasAFactory` becomes a kit helper that an app's tests call, covering kit's tables and the app's.

## kit-ui and the web app

- **`@harlew-dev/kit-ui` exports** `components/ui` and the layouts, `useSession`, `useWorkspace`, the HTTP client core (CSRF, the expired-session redirect, `ChallengeDialog` once captcha ships), SWR helpers (`useResource`, `useResourcePages`), the auth, settings and onboarding pages, the admin build, and route helpers.
- **The app owns `web/routes.ts`** and composes kit's routes with its own: `...kitRoutes()` for `/auth/*`, `/app/settings`, `/app/onboarding` and the admin pages, then the app's pages.
- **`kit()` Vite plugin** sets up React Router framework mode, prerendering, Tailwind v4 (with an `@source` for kit-ui's files so its classes are generated), the admin build and the dev proxy, so an app's `vite.config.ts` is a few lines.
- **The typed client** for the app's endpoints comes from `myapp routes:ts`, with the same `api.projects(workspace.id).store()` shape as today.
- **`internal/web`'s serving moves into `kit/web` unchanged:** prerendered pages, SPA fallback, CSP hashes computed at startup from each file's inline scripts, the admin build behind the key.

## The CLI

Go needs two halves of `artisan` (kit stage 18). **`kit`** (`go install github.com/harlew-dev/kit/cmd/kit@latest`) works on source files and needs no running app. **The app's own binary** (`myapp`, from `kit.Run(app.New())`) runs the app's code: migrations, seeders and the route table live there. The v0.1 commands:

| Command | Does |
| --- | --- |
| `kit new myapp` | Clones kit-starter, sets the module name, writes `.env` from `.env.example` with a fresh `APP_KEY` |
| `kit make:resource workspace/project/tasks` | Controllers, request, model, migration, factory, events, typed client and page stubs, integration test with the role and non-member cases |
| `kit make:controller workspace/project/tasks/index` | One controller embedding the base |
| `kit make:request`, `make:model`, `make:migration`, `make:event`, `make:listener`, `make:mail` | One file each in the right place; events and listeners regenerate `events.go` and the provider line |
| `myapp routes`, `myapp routes:ts` | Print the route table; generate the web app's paths |
| `kit check` | The layout and naming rules below, run in CI and `make check` |
| `myapp serve`, `migrate`, `seed`, `prune`, `admin …` | Today's `cmd/server`, `cmd/migrate`, `cmd/seed`, `cmd/prune` and `cmd/admin` |

### `kit check`

Absorbs the open [convention checks plan](../features/convention-checks.md) (decision 18) and adds the layout rules:

- Folder and file rules 1–6 above: one exported type per file, the file named after it, the folder cap, controllers only under `controllers/`, the import-rename pattern.
- Event packages import nothing from the app; every event has an alias and every alias an event; every listener is registered.
- A request's rules name only its struct's JSON fields.
- **SQL text is constant:** the first argument of `Where`, `WhereOr`, `OrderExpr`, `ColumnExpr`, `Set` and `With` is a string literal or constant, never built from a variable (a `go/analysis` check), so input only ever reaches SQL through `?` placeholders.
- Today's naming tests (permissions, event names, rate-limit names, client methods, page exports).

## Where Go differs from Laravel

| Laravel | Go | kit's answer |
| --- | --- | --- |
| Class inheritance, overridable methods | Embedding, no overriding | Base `Controller` and `Listener` for shared dependencies and helpers |
| Namespaces separate from files | A folder is a package, named by its last segment | Nested folders, one type per file, the rename pattern for clashes |
| `Events\Task\TaskCreated` | One dot only: `package.Name` | Generated aliases: `events.TaskCreated` |
| Static calls: `Project::where()` | No static methods on types | A function per model: `models.Projects(ctx).Where(…)` |
| Lazy relations: `$user->workspaces` | No magic `__get`; hidden queries go against Go style | Eager loading: `.With("Workspaces")` |
| `$project->save()` | Possible only by putting the database in the model | Write functions: `models.UpdateProject(ctx, …)` |
| Exceptions, `abort(404)` | Errors are returned values | Return the error; one handler maps it to a response |
| Service container, autowiring | Only with reflection, which turns compile errors into runtime crashes | Explicit wiring in `app/app.go` and providers; the base types carry common dependencies |
| Listener and route auto-discovery | No listing of a package's types at runtime | Registration files kept by `kit make:*` and checked by `kit check` |
| Overloaded `handle()` in a subscriber | No overloading | One listener per event |
| `query->get<T>()`-style generic methods | Methods can't have their own type parameters | The type sits on the builder: `db.Query[Project]` |

## Moving this app onto kit

kit is built in its own repo, `/home/homelab/dev/harlew-dev/kit`, in the 23 stages listed in its `plans/README.md`. Two of those stages touch this repo:

- **Stage 7, the pilot:** this app's five project routes run on kit's router, controllers, requests and `db.Query[T]`, in a branch of this repo, while everything else here stays as it is. It proves kit's core on real code before auth and workspaces are built on it.
- **Stage 21, the port:** this repo becomes an ordinary kit app in the starter layout (`app/`, `routes/`, `database/`, `web/`, `cmd/lewcontext`), with projects as its only app code, and `apps/` is deleted.

Throughout, `make test`, `make check` and `make test-e2e` must pass after each step. The existing integration tests (the role × route matrix, non-members get 404, other workspaces, rate limits, CSRF and Origin) are the proof that nothing changed: kit's tests are ported from them with their assertions unchanged, and this repo keeps its project tests unchanged. A golden route table (method, path, middleware, permission, role, limit) from before the port is compared with `myapp routes` after it.

Until stage 21, this repo carries on as it is. A `go.work` joining `apps/api` to `../harlew-dev/kit` is local only and never committed.

## Docs

- **kit's docs** move from `docs/` to `kit/docs/`: auth, admin, rate limits, events, mail, pagination, database, deployment, and the UI docs. `naming.md` is rewritten for the new layout (controllers, requests, models, events, listeners, file rules).
- **This app's `docs/`** keeps only what's specific to it (projects, its seed data), with links to kit's docs.
- **AGENTS.md splits:** kit's rules (security invariants, layout, `kit check`) ship with kit and the starter; an app's AGENTS.md adds its own and points at kit's.
- **A changelog and upgrade guide** in kit (`CHANGELOG.md`, `UPGRADING.md`), with security fixes marked.
- [`plans/README.md`](../README.md) lists this plan under Future plans.

## Security risks and how they're avoided

| Risk | How it's avoided |
| --- | --- |
| A ported route loses `RequireSession`, Origin, CSRF, its budget or its permission | The golden route-table test in phase 2 onwards compares method, path, middleware, permission and limit before and after. The router's startup checks panic on a mutating route without a permission, a create route without a limit, and a private route without a session. The existing role × route matrix runs unchanged. |
| Route model binding fetches another workspace's record | Only `BindScoped` exists under `{workspace}`; the router panics on an unscoped binding there. The bound record must match the scope column, or it's the same 404 as a missing one. Tests cover other workspaces for every binding. |
| `Query[T].Where` makes SQL from input | `kit check` requires constant SQL text in the builder methods; values go through `?`. Sorting goes through an allowlist (`Sort(key, allowlist)`), as `projectSorts` does today. |
| An internal error reaches the response | Only `kit.HTTPError` text is sent; everything else is the generic 500 and is logged. A test sends a wrapped database error through a controller and checks the body. |
| A transaction in the context is used after it ends (a goroutine started inside `db.Transaction`) | Listeners get a fresh context from the worker, never the request's. `db` refuses a finished transaction with an error instead of running on it. Document "don't start goroutines inside a transaction". |
| An event or listener is renamed by a file move | `EventName()` and `ListenerName()` are written by hand; `TestEventNamePattern` and the catalogue test run on the app's events. |
| Apps fall behind on security fixes | Semver, a changelog that marks security fixes, Renovate or Dependabot on each app. The core is one module, so a fix is one version bump. |
| A tampered or mistaken release of a private package | Protected tags, required reviews on kit's repo, `go.sum` checked in apps (`GOPRIVATE` skips the public checksum database, so the app's `go.sum` is the check), lockfiles kept. |
| Generated files (aliases, typed paths, the provider) drift from the code | `kit check` regenerates them in memory and fails on a difference. |
| kit's invariants weaken while being generalised (admin key, sessions, CSP, mail rules, email blocks) | They move unchanged, with their tests. Apps can add routes and listeners but can't replace kit's middleware, admin routes or mailer. |

## Tests

- **Router:** resource mapping (only the set fields, singular params, nested resources), groups and middleware order, every startup panic (permission, limit, session, unscoped binding, duplicate), unknown routes as JSON, the golden route table.
- **Errors:** each row of the error table, and a wrapped internal error answering the generic 500.
- **Requests:** decode limits (16 KiB, unknown fields, wrong types), `Prepare` before `Rules`, each rule, Unicode lengths, the rules-match-fields check.
- **db, against real PostgreSQL:** `Query[T]` methods, `Paginate` (bounds, totals, stable order), `Sort` allowlist, `Transaction` rollback and nested use, a finished transaction refused, `NotFound`.
- **Events and listeners:** alias generation (golden files), registration, typed decoding, the existing worker tests.
- **Migrations:** kit's and an app's run in order with separate tables, under the lock, and roll back per group.
- **`kit check`:** each rule as a pure function with table tests of good and bad examples, then a run on the starter and this app.
- **CLI:** each `make:*` command against golden output in a temp directory, then `go build` and `go vet` on the result.
- **This app:** every existing Go, Vitest and Playwright test passes after each phase.

## Decisions (to confirm)

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Names | `kit`, Go module `github.com/harlew-dev/kit`, npm `@harlew-dev/kit-ui`, starter `github.com/harlew-dev/kit-starter`. |
| 2 | Package split | One Go module (no separate `kit-auth`) and the npm package in the same repo, released under one version. |
| 3 | Where kit is built | In its own repo from the start, `/home/homelab/dev/harlew-dev/kit`, with a local `go.work` (not committed) joining it to this repo's `apps/api` for the pilot and the port. |
| 4 | Custom `kit.Context` or plain `http.Handler` | `kit.Context` (as Buffalo, Echo and Gin do), implementing `context.Context` and accepting standard middleware. |
| 5 | Controller type names | Short: `tasks.Index`, with the folder naming the resource. Not `tasks.TaskIndex`. |
| 6 | File names | One exported type per file, named after it in snake_case; an unexported helper gets its own file named after it. |
| 7 | Folder cap | 12 files, not counting tests and generated files, in every folder except `models/`, which warns at 40 and suggests a one-way split. |
| 8 | Events | Nested folders, `<area>_<fact>.go` files, generated aliases (`events.TaskCreated`), `EventName()` by hand. The methods aren't called `Name()` because a struct can't have a field and a method with the same name, and many events have a `Name` field. |
| 9 | Listeners | One per event and file, registered in `providers/event_provider.go`, all queued. The subscriber shape (`ListenFunc` with named methods) isn't offered, so there's one way to write one. |
| 10 | Database access | The handle and transaction travel in the context (`db.Model[T](ctx)`, `db.Transaction`). The alternative, passing `db` or `tx` to every call, is more explicit but easy to get wrong inside a transaction. |
| 11 | Route model binding | Included from v0.1, scoped only (`BindScoped`), because nested resources (tasks under a project) would otherwise repeat the parent lookup in every controller. |
| 12 | Not-found messages | `db.NotFound{Model}` answers `<Model> not found.`, matching today's messages, with no per-controller code. |
| 13 | Rate limits on create routes | Declared on the controller (`Limit()`, beside `Can()`), shown by `myapp routes`, with a startup panic when a create route has none. |
| 14 | Validation rules | A `kit.Rules` map keyed by JSON field name, checked against the struct. Not struct-tag rules, which get unreadable past two rules. |
| 15 | Migrations | kit's in `kit_migrations`, the app's in `bun_migrations`; kit's append-only from v0.1.0. |
| 16 | App layout | The starter layout above (`app/`, `routes/`, `database/`, `web/`, `cmd/`) for every app, this one included (phase 7), instead of `apps/api` and `apps/web`. |
| 17 | Workspaces | Part of kit and always on in v1. Making them optional touches onboarding, roles, invitations and admin, so wait until an app needs it. |
| 18 | Convention checks plan | Folded into `kit check` instead of being built as tests in this repo first. Its docs link check stays a script, in kit and the starter. |
| 19 | Distribution | Private: GitHub repos, `GOPRIVATE`, GitHub Packages for npm, protected tags. |
| 20 | Laravel features left out | No facades, no autowiring container, no lazy relations, no active-record `save()`, no auto-discovery, no synchronous listeners, no subscribers (the table above says why). |

## Not included

- Server-rendered templates (Blade, Plush), Inertia or Livewire: the web app stays a React SPA with prerendered public pages and no server rendering of user data.
- A general job queue, a scheduler, broadcasting or file storage. Listeners on the delivery queue cover background work today; each of these is its own plan when an app needs it.
- Databases other than PostgreSQL, or several databases per app.
- Passkeys, OIDC or social sign-in, and changing an account's email: still not built.
- Admin actions defined by an app. Apps may add read-only admin routes and pages, behind kit's guards and audit (needed for this app's workspace projects page); actions wait for the [admin actions plan](../features/admin/actions.md).
- A public open-source release, a plugin system or third-party kit packages.
- Optional workspaces (decision 17).
