# Convention checks

Status: **proposed 2026-10-31**, decisions to confirm. Enforces [`docs/naming.md`](../../docs/naming.md) after the [CRUD naming convention](implemented/crud-routing-convention.md) and [event names](implemented/event-names.md) renames. Do it before [deletion](implemented/deletion.md), so every later plan's new routes, store methods, events and pages are checked as they're written.

## Why

The naming rules live in `docs/naming.md`, and nothing stops code that breaks them except a reviewer noticing. Two renames in one day (CRUD names, event names) show how quickly names drift, and an agent copying a nearby file copies its mistakes. On the same day a commit linked `docs/deployment.md` while that file was still untracked, so the link was dead in the repository.

The project already enforces rules with ordinary tests: `TestEveryTableHasAFactory`, the permissions tests that parse `permissions.go`, the Vitest test that compares the sort allowlist with the Go source, and the role × route matrix. This plan adds the same kind of test for each naming rule, plus a link check for the docs, so `make test` and `make check` fail as soon as a name or link is wrong. AGENTS.md already requires both before work is called done.

## Design

Every check follows the same shape:

- **A pure function** that takes names and returns the ones that break the rule, with a reason (`badStoreMethods([]string) []string`). A table test feeds it good and bad examples, so the check is itself tested.
- **A test that runs it on the real code**, read by reflection, by parsing the Go source (`go/parser`, as the permissions test does), or by importing the web modules.
- **Exceptions are explicit lists in the test**, each entry with a one-line reason. Adding a domain verb or a named action means adding it to the list, which shows up in review.
- **Failure messages name the rule and the doc section**, e.g. `store method "ProjectList": store methods start with a verb (Index, Find, Create, …); see docs/naming.md#store-methods-internalstore`. That message is what an agent sees, so it must say how to fix the name.

### Go

| Check | Where | Reads | Rule |
| --- | --- | --- | --- |
| `TestStoreMethodNames` | `internal/store/naming_test.go` | reflection on `*Store` | Every exported method starts with `Index`, `Find`, `Create`, `Update`, `Delete` or `Check`, or with a listed domain verb: `Rename`, `Remove`, `Leave`, `Issue`, `Reset`, `Change`, `Record`, `Count`, `Prune`, `Allow`, `Cleanup`. Methods of `events.Queue` (`Claim`, `Delivered`, `Fail`, `Retry`) are exempt, read from the interface type rather than listed, and the test asserts `*Store` implements it. `Index…` methods return `Page[T]` and an error. |
| `TestHandlerNames` | (done: `TestHTTPLayout`, `internal/httpapi/layout_test.go`) | the `httpapi` tree's Go source | Moved into `TestHTTPLayout` by the [httpapi layout](implemented/httpapi-layout.md): controllers (structs embedding `*httpapi.Server`) only in `endpoints/<area>`, one per file, their exported methods `Index`, `Show`, `Store`, `Update`, `Destroy` or a listed named action. |
| `TestRouteNames` | `internal/httpapi/router/routes_test.go` | `router/routes.go` source | Every route's method is GET, POST, PATCH or DELETE (no PUT). Every route outside `/api/health` is registered with `<var>.<Action>`, where `<var>` is a controller value (`workspace.Projects{Server: s}`): no bare `s.something` handlers. |
| `TestPermissionNames` | `internal/auth/permissions_test.go` | `permissions.go` source (the existing `declaredPermissions`, extended to return the constant's name) | Each permission is `<resource>.<action>`: snake_case segments ending in `index`, `show`, `store`, `update` or `destroy`. The Go constant is the same words in PascalCase (`workspace.member.destroy` → `WorkspaceMemberDestroy`). `AnyMember` is exempt. |
| `TestEventResources` | `internal/events/events_test.go` | `events.Catalogue`, and `auth`'s permissions | A three-segment event's resource (`workspace.member` in `workspace.member.removed`) is a permission resource (a permission name minus its action), or is listed as a stored resource without permissions (`auth.password_reset`). The existing `TestEventNamePattern` already checks the shape. |

### Web (Vitest)

| Check | Where | Reads | Rule |
| --- | --- | --- | --- |
| API client names | `src/lib/api/naming.test.ts` | `client(stubHttp)` and `paths` | Each resource's methods are `index`, `show`, `store`, `update`, `destroy` or a listed verb (`leave` on workspaces). Typed paths are the same actions plus `matches`, and listed singleton paths (`membership` on workspaces). `auth` and `sessions` are exempt as named actions, like `/api/auth/*`. Workspace resources are called with a sample `wsp_` ID. |
| Page route constants | `src/lib/routes.test.ts` | `routes` | A resource group (an object in `routes`) has only `index`, `show`, `create` and `edit`. Every other key is in the list of non-resource pages (`home`, `app`, `onboarding`, `settings`, `dashboard`, `login`, `register`, `forgotPassword`), so a new page has to be either a resource action or deliberately listed. |
| Page export names | `src/pages/naming.test.ts` | `routes`, `src/routes.ts` and the page modules | For each resource group and action, build its URL with sample IDs, find the `src/routes.ts` entry it matches, import that page and expect its default export to be named `<Resource><Action>`. The resource is the group's key made singular (strip a trailing `s`; a small map for irregular plurals). Today that's `ProjectIndex`, `ProjectShow`, `WorkspaceEdit` and `MemberIndex`. |
| File names | `src/naming.test.ts` | the file tree under `src/` (`node:fs`) | Files in `components/`, `lib/`, `types/` and `test/` are kebab-case (`.ts`, `.tsx`, `.test.ts(x)`). `pages/` is exempt: its files are shaped like URLs (`[workspace]`). |

### Docs links (`scripts/check-doc-links.mjs`, run by `make check`)

A Node script with no dependencies:

1. Lists markdown files with `git ls-files '*.md'` plus untracked ones (`git ls-files --others --exclude-standard '*.md'`), outside `node_modules` and build output.
2. Finds relative links (`[text](path)` and `[text](path#anchor)`), ignoring fenced code blocks, inline code and absolute URLs.
3. **Fails** when the target file doesn't exist, or when an `#anchor` matches no heading in the target, using GitHub's heading ids: lowercase, punctuation other than `-` and `_` dropped, spaces as `-`, and `-1`, `-2` for repeated headings. Links within a page (`#anchor`) are checked against its own headings.
4. **Warns** (prints, exits 0) when a **tracked** file links to an **untracked** one: the `deployment.md` case. It's a warning because while you're working a new doc is legitimately untracked; see decision 4.
5. Prints `file:line: link → reason` for each problem, and a count.

On 2026-10-31 every relative link in the repository's markdown resolved to a file, so the check should start clean apart from any anchors the script finds.

## Changes

1. The five Go checks and four Vitest checks above, each with its pure function and table test.
2. `scripts/check-doc-links.mjs`, and a line in `make check` that runs it (`node scripts/check-doc-links.mjs`) before the web checks.
3. **`docs/checks.md`**, a new doc explaining the checks and how to respond to them ([below](#the-checks-doc-docschecksmd)).
4. Fix whatever the checks find on their first run. Nothing is expected apart from possible anchor mismatches. A real violation that should stay becomes a listed exception with its reason, only after the user agrees (decision 8).
5. **The [rate limits plan](implemented/rate-limits.md):** add `TestRateLimitNames` (`<feature>:<counted by>`, counted by `per-ip`, `per-account`, `per-email`, `per-user` or `global`) to its Tests section. It isn't added here, because `password-reset:all-accounts` breaks the rule until that plan renames it to `password-reset:global` (decision 5).

## The checks doc (`docs/checks.md`)

A new doc for people and AI agents. It explains what the checks enforce and how to respond when one fails, so following the standards is the easy path. It covers:

1. **What's checked and why:** one table listing each check, the rule it enforces, the file it lives in and where its exception list is, linking to the rule's section in `naming.md`. Plus a short reason: names are how people and agents find and reuse code, and a wrong name gets copied.
2. **Terminology:** the words the project uses and the ones it doesn't. For example: `store`, not `create`, in the API and client; `destroy`, not `delete`, as an action, though `Delete…` for store writes; `index`, not `list`; `edit` for the settings page constant; `<resource>.<fact>` for events, which are facts, not commands; "member" for someone in a workspace and "user" for the account. Each term links to `naming.md`.
3. **Reading a failure:** what each message means and the usual fix, with one example per check.
4. **When a check is wrong, or a name is new:** what to do when the rule doesn't fit, such as a new domain verb, a new named action or a new non-resource page. Propose the name and the reason, get agreement, then add the exception with its reason in the same change as the code that needs it, and update `naming.md` if the rule itself changed.
5. **For AI agents**, as its own section, worded as instructions:

   > If you are an AI agent working in this repository:
   >
   > - **Don't work around these checks.** Never rename something to slip past a check while breaking the rule's intent, add an exception, loosen a regex, skip a test (`t.Skip`, `it.skip`), delete a test, or edit a check's word lists to make your change pass, unless the user has agreed to that exception.
   > - **Fix the name, not the check.** When a check fails, rename your code to follow [`naming.md`](naming.md). The failure message says which rule and section apply.
   > - **Ask when unsure.** If `naming.md` doesn't settle a name (a new resource, a new verb, an action that isn't one of the five), stop and ask the user. Give your proposed name and why, rather than inventing a convention or copying a nearby file that may be wrong.
   > - **Say so if a check seems wrong.** If a rule doesn't fit the task, tell the user and explain why. Don't silently pick one side, as AGENTS.md already requires for docs that disagree with code.
   > - **Run the checks before saying you're done:** `make test` and `make check`. A warning from the link check (a tracked file linking to an untracked one) means stage the new file or remove the link. Don't leave it for the user to find.

6. **Adding a check:** the shape (a pure function, a table test with refused examples, a run on the real code, exceptions as lists with reasons, failure messages that name the rule and doc section), and a row in this doc's table.

## Docs

- **`docs/checks.md`:** new (above).
- **`docs/naming.md`:** a line at the top: every rule here is checked; see [checks.md](checks.md). Each rule's section links to its check.
- **`AGENTS.md`:** a row for `docs/checks.md` in the docs table, marked as applying to every change, like `naming.md`. Add a line to "Docs and plans" saying agents must not work around a failing check and should ask the user when `naming.md` doesn't settle a name, pointing to `checks.md`.
- **`docs/README.md`:** a row for `checks.md` in "Everything else", next to `naming.md`.
- **`docs/api/testing.md`** and **`docs/ui/testing.md`:** the new tests in "Notable tests", and a sentence saying naming checks are pure functions plus a run on the real code, and that exceptions are lists in the test.
- **`docs/testing.md`** and **`AGENTS.md`**, "Commands and validation": `make check` also checks doc links; fix a dead link or anchor rather than ignoring it.
- **`plans/README.md`:** this plan in the open plans table.

## Security risks and how they're avoided

No runtime change: tests, a script and a Makefile line. The checks protect security conventions indirectly. `TestRouteNames` keeps every route on a controller, so a new resource route goes through a controller, the `ws` group and `ws.Can`, where the role × route matrix covers it.

| Risk | Mitigation |
| --- | --- |
| A check that never fails gives false confidence | Each check's table test includes names that must be refused. |
| Exceptions grow until the check means nothing | Every exception has a reason in the test, and new ones show up in review. |
| The link script runs code from the repo | It only reads files and runs `git ls-files`; no network, no `eval`. |

## Tests

- Each check's table test: good names pass; bad names fail with the expected reason. For example, `ProjectList` and `Projects` for store methods; `create` and `list` for handlers; `PUT` routes; `workspace.member.remove` and a constant spelled differently from its permission; `workspace.membership.removed` as an unknown event resource; `create` in the API client; `routes.projectList`; a page exported as `Projects`; `ProjectTable.tsx`; a dead link, a dead anchor and a link to an untracked file for the script.
- Run on the real code: all pass.
- Run `gofmt`, `go vet`, `make test`, `make check` (which now includes the link check) and `make build`. No integration or e2e change is expected, but run `make test-integration` once, since `TestRouteNames` sits beside the router's integration tests.

## Decisions (to confirm)

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Where checks live | Ordinary Go and Vitest tests beside the code they check, plus one script for docs. No new tools or dependencies (no custom `go vet` analyzer, no ESLint plugin). |
| 2 | Exceptions | Explicit lists in each test, each with a reason. `events.Queue` methods come from the interface type, not a list. |
| 3 | Non-resource pages | Listed in the route constants test, so adding one is a deliberate choice rather than a fallback. |
| 4 | Links from tracked files to untracked ones | Warn in `make check`, don't fail. Failing would break `make check` whenever you create a doc and link it before staging it. A pre-commit hook could fail on it later (decision 6). |
| 5 | Rate-limit names | Checked by the rate limits plan, which renames `password-reset:all-accounts`. |
| 6 | Git or Claude Code hooks | Not now. The checks run in `make test`/`make check`, which AGENTS.md already requires. Add a pre-commit hook or a Claude Code `Stop` hook later if checks get skipped. |
| 7 | Heading anchors | Checked, with GitHub's id rules, since the docs link to anchors often (`auth.md#workspaces-and-roles`). |
| 8 | Who may add an exception | Only with the user's agreement, recorded as the exception's reason in the test. `docs/checks.md` tells agents to ask rather than add one. |
| 9 | Where agent guidance lives | `docs/checks.md` (the detail), with a short pointer in AGENTS.md, which every agent reads first. Not a separate agents-only file, so people and agents follow the same rules. |

## Not included

- Git pre-commit or Claude Code hooks (decision 6).
- Rate-limit names (decision 5).
- Checking prose: whether a doc's description matches the code.
- Checking external URLs in docs (needs the network).
- Lint rules for code style beyond names; ESLint and Prettier already cover style.
