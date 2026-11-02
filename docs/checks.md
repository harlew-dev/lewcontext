# Convention checks

The naming rules in [naming.md](naming.md) and the docs' links are checked by ordinary tests and one script, so `make test` and `make check` fail as soon as a name or a link is wrong. Names are how people and agents find and reuse code, and a wrong name gets copied by the next change that looks at it. Plan: [convention checks](../plans/features/implemented/convention-checks.md).

## What's checked

| Check | Rule | Lives in | Exceptions |
| --- | --- | --- | --- |
| `TestStoreMethodNames` | Store methods start with a verb: `Index`, `Find`, `Create`, `Update`, `Delete`, `Check` or a listed domain verb; `Index…` returns a list (`Page[T]`, a cursor page, or `[]T`) and an error; `events.Queue`'s methods keep the interface's names ([naming.md](naming.md#store-methods-internalstore)) | `apps/api/internal/store/naming_test.go` | `domainVerbs`, each with what it's for |
| `TestHTTPLayout` | Controllers live in `endpoints/<area>`, one per file, their methods `Index`, `Show`, `Store`, `Update`, `Destroy` or a listed named action ([naming.md](naming.md#controllers-internalhttpapi)) | `apps/api/internal/httpapi/layout_test.go` | `namedActions`, `supportPackages` |
| `TestRouteNames` | Every API route is a controller's action (`x := area.Things{Server: s}`, then `x.Index`), with GET, POST, PATCH or DELETE: never PUT, never a bare handler ([naming.md](naming.md#controllers-internalhttpapi)) | `apps/api/internal/httpapi/router/naming_test.go` | `notControllers` (health checks, the admin catch-all); pages may only be GET |
| `TestPermissionNames` | Permissions are `<resource>.<action>`, snake_case, ending in `index`, `show`, `store`, `update` or `destroy`; the constant is the same words in PascalCase ([naming.md](naming.md#permissions-internalauthpermissionsgo)) | `apps/api/internal/auth/naming_test.go` | `notPermissionNames` (`AnyMember`) |
| `TestEventResources` | A three-segment event's resource is a permission's resource (`workspace.member`), a listed stored resource, or what an admin read (`admin.<resource>.listed`/`.viewed`) ([naming.md](naming.md#events-internalevents)). `TestCatalogueNames` checks the shape | `apps/api/internal/events/naming_test.go` | `storedResources` |
| `TestRateLimitNames` | `<feature>:<counted by>` ([naming.md](naming.md#rate-limits-httpapilimitsgo)) | `apps/api/internal/httpapi/limits_test.go` | none |
| API client names | Each resource's client methods and typed paths are the API's actions, plus listed verbs (`leave`, `check`, `accept`), `matches` and listed singleton paths ([naming.md](naming.md#web-app)) | `apps/web/src/lib/api/naming.test.ts` | `clientVerbs`, `pathExtras`, `namedActionResources` (`auth`, `sessions`) |
| Page route constants | A resource group in `routes` holds only `index`, `show`, `create` and `edit`; every other key is a listed page that isn't a resource ([naming.md](naming.md#web-app)) | `apps/web/src/lib/routes.test.ts` | `plainPages` |
| Page export names | A resource page's default export is `<Resource><Action>` (`routes.projects.show`'s page is `ProjectShow`) ([naming.md](naming.md#web-app)) | `apps/web/src/pages/naming.test.ts` | `irregular` plurals |
| File names | Files in `components/`, `lib/`, `types/` and `test/` are kebab-case (React Router's `.server` suffix allowed, for a module kept out of the browser); pages has its own check below | `apps/web/src/naming.test.ts` | none |
| Page folders | Every page is a folder with the page in its `index.tsx`, pages beneath it in deeper folders, a record's page `[<param>].tsx` in the resource's folder, and a folder with pages beneath it its bare URL's own page ([naming.md](naming.md#web-app)) | `apps/web/src/pages/layout.test.ts` | `noIndex` (`auth/invitations`, `auth/email-blocks`: its folder holds only the `[token]` page) |
| Doc links | Every relative link in the markdown git tracks or has staged (an untracked file may be another session's work in progress; stage a new doc to check it) points at a file that exists, and every `#anchor` at a heading in it (GitHub's ids); a tracked file linking an untracked one is a warning | `scripts/check-doc-links.mjs`, run by `make check` (with its `--test`) | `allowedDeadLinks` |

## Terminology

The confirmed email-change feature registers `auth.email_change` in `storedResources` because `email_changes` is a stored session-bound singleton. The account client's singleton remains covered by the existing auth-area action convention; its four CRUD methods/typed paths have explicit client coverage. The exact unverified-route check is now `TestOnlyNineRoutesAllowUnverified`, including only the four additional email-change routes. No naming regex or convention rule was loosened.

| Say | Not | Where |
| --- | --- | --- |
| `index` | `list`, `all` | the API's and client's actions, `Index…` store methods ([naming.md](naming.md#controllers-internalhttpapi)) |
| `store` | `create`, `add` | the API's and client's create action; the store's writes are `Create…` ([naming.md](naming.md#store-methods-internalstore)) |
| `destroy` | `delete`, `remove` | the API's and client's delete action; the store's writes are `Delete…`, or a domain verb (`RemoveMember`) |
| `update` (PATCH) | `edit`, `save`, PUT | the API's change action; `edit` is the settings **page** constant (`routes.workspaces.edit`) ([naming.md](naming.md#web-app)) |
| `<resource>.<fact>` | `<resource>.<command>` | events are facts, past tense (`project.created`), never commands (`project.create`) ([naming.md](naming.md#events-internalevents)) |
| member | user | someone in a workspace is a member; the account is a user |

## Reading a failure

Every message names the rule and the section of [naming.md](naming.md) it comes from. The usual fix is to rename your code, not the check:

| Message | Means | Usual fix |
| --- | --- | --- |
| `store method "ProjectList": store methods start with a verb …` | A store method named by a noun | `IndexProjects` |
| `store method "IndexThings": Index methods return one page of a list …` | An `Index…` returning one record | `FindThing`, or return the list |
| `route GET /api/things: register a controller's action …` | A route handled by a bare function (`s.something`) | Add the action to the area's controller and register `things.Index` |
| `route PUT /api/things/{thing}: update is always PATCH …` | PUT | PATCH |
| `permission WorkspaceMemberRemove = "workspace.member.remove": … ending in index, show, store, update or destroy` | A permission named after a fact or a verb | `workspace.member.destroy`, `WorkspaceMemberDestroy` |
| `event "workspace.membership.removed": its resource "workspace.membership" is neither a permission's resource nor a listed stored resource …` | An event's resource spelled unlike its permission | `workspace.member.removed` |
| `projects.create: the API client uses the API's actions …` | A client method named by the page or the verb you'd say | `store` |
| `routes.projectList: a page that isn't a resource is listed in plainPages …` | A new page constant outside a resource group | `routes.projects.index`, or list it if it really isn't a resource |
| `routes.projects.index's page exports Projects: … ProjectIndex` | A page's default export | `export default function ProjectIndex` |
| `components/ui/ProjectTable.tsx: files … are kebab-case` | A file name | `project-table.tsx` |
| `docs/a.md:12: b.md#part → no heading #part in docs/b.md` | A dead link or anchor | Point it at the file or heading that exists (headings' ids are lower case, punctuation dropped, spaces as `-`) |
| `warning: … isn't tracked: stage it, or the link is dead in the repository` | A tracked doc links one you haven't staged | `git add` the new file, or remove the link |

## When a check is wrong, or a name is new

Some names are new on purpose: a new domain verb (`Archive…`), a named action that isn't one of the five, a page that isn't a resource, a dead link that has to stay. Then:

1. Propose the name and the reason to the user, and get their agreement.
2. Add the exception to the check's list, with that reason, in the same change as the code that needs it.
3. If the rule itself changed, update [naming.md](naming.md) too.

Exceptions marked "existing; to fix" are names that broke a rule before the checks existed; fix them when you next touch that code.

## For AI agents

If you are an AI agent working in this repository:

- **Don't work around these checks.** Never rename something to slip past a check while breaking the rule's intent, add an exception, loosen a regex, skip a test (`t.Skip`, `it.skip`), delete a test, or edit a check's word lists to make your change pass, unless the user has agreed to that exception.
- **Fix the name, not the check.** When a check fails, rename your code to follow [naming.md](naming.md). The failure message says which rule and section apply.
- **Ask when unsure.** If [naming.md](naming.md) doesn't settle a name (a new resource, a new verb, an action that isn't one of the five), stop and ask the user. Give your proposed name and why, rather than inventing a convention or copying a nearby file that may be wrong.
- **Say so if a check seems wrong.** If a rule doesn't fit the task, tell the user and explain why. Don't silently pick one side, as AGENTS.md already requires for docs that disagree with code.
- **Run the checks before saying you're done:** `make test` and `make check`. A warning from the link check (a tracked file linking to an untracked one) means stage the new file or remove the link. Don't leave it for the user to find.

## Adding a check

Follow the shape every check here has:

1. **A pure function** that takes names and returns the ones breaking the rule, each with a message naming the rule and its section of [naming.md](naming.md) and saying how to fix it.
2. **A table test** feeding it good names and names it must refuse, so a check that never fails is caught.
3. **A run on the real code**, read by reflection, by parsing the Go source (`go/parser`) or by importing the web modules, which fails if it finds suspiciously few names (so a moved file can't make it pass by finding nothing).
4. **Exceptions as a list in the test**, each with its reason, and, where the list names things in the code, a failure for an entry nothing uses any more.
5. **A row in this doc's table**, and a link from the rule's section in [naming.md](naming.md).
