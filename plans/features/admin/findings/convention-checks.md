# Findings: convention checks

Plan: convention-checks (on the events branch it has moved to `plans/features/implemented/convention-checks.md`). Built on the **events** worktree (`../lewcontext-events`, branch `admin-run/events`), with every recommendation in its Decisions table taken as accepted.

## What shipped

| Commit | What |
| --- | --- |
| `ece3ceb` | Merge of main (A3 phase 1) into the events branch. Migration 007 kept both edits without a conflict |
| `26979ec` | Four Go checks and four Vitest checks |
| `32a48da` | A listed exception that nothing uses any more fails its check |
| `52c1734` | `scripts/check-doc-links.mjs`, run by `make check`: every relative link and `#anchor` in the docs must resolve |
| `d743f25` | `docs/checks.md`, with pointers from AGENTS.md, `naming.md` and the testing docs |

### The checks

| Check | Enforces |
| --- | --- |
| `TestStoreMethodNames` | Store methods start with `Index`, `Find`, `Create`, `Update`, `Delete`, `Check`, or a listed domain verb with its reason; `Index…` returns a list |
| `TestRouteNames` | Every API route is a controller action. Methods are GET, POST, PATCH or DELETE (never PUT); pages are GET only; a route the test can't read fails |
| `TestPermissionNames` | `<resource>.<action>` in snake_case, ending in a CRUD action |
| `TestEventResources` | A three-part event name's resource is a permission's resource, a listed stored resource, or an admin read |
| Vitest | API client methods and typed paths use the API's actions; page route constants are resource groups or listed plain pages; resource pages export `<Resource><Action>`; files outside `pages/` are kebab-case |
| Doc links | Every relative link and anchor resolves (GitHub's heading ids). A link from a tracked file to an untracked one warns, not fails |

`TestHTTPLayout` and `TestRateLimitNames` already existed and are listed in `docs/checks.md`.

## Existing problems it found

**Fixed:** dead anchors and links in AGENTS.md, `docs/api/README.md`, `endpoints.md`, `docs/testing.md`, `docs/ui/pages.md`, the MFA and httpapi-layout plans, and the reading folder. The most common was `naming.md#controllers`, an anchor that never existed.

**Excepted until you agree (the plan says exceptions need your say):**
1. **`SetupMFA` should be `SetUpMFA`** ("set up" is the verb). It wasn't renamed because the change crosses two packages. _Recommendation: rename it at the final merge._
2. **A link from `plans/README.md` to `future/framework.md`,** a file another session keeps untracked. The three other dead links were the orchestrator's own (to `notes.md` and `page-folders.md`). They're now plain text, and their exceptions are being removed.

**Lists the checks now hold, each with its reason** (all allowed by `naming.md`; tell me if any shouldn't be):
- **Domain verbs in the store:** `Accept`, `Answer`, `Block`, `Complete`, `Disable`, `Enable`, `Flag`, `Grant`, `Resend`, `Revoke`, `StepUp`, `Summarize`, `Unblock`, `Verify`.
- **Plain pages:** `verifyEmail`, `registerVerify`.
- **Client verbs:** `check`, `accept`.
- **Stored event resources:** `admin.session`, `admin.email`.
- **Routes:** the health checks and the `/api/admin/` catch-all.

## Where the build differs from the plan

- **`TestHandlerNames` already existed** as `TestHTTPLayout`.
- **`Index…` may also return a cursor page or a plain list.**
- **Admin reads pass by `naming.md`'s rule,** not a list.
- **The admin client and admin pages aren't checked.** They're in the admin build, and the plan covered only `routes`.
- **An exception nothing uses fails its check,** so the lists can't go stale.

## What this means for later work

Any new store verb, plain page or event resource must be added to its list with a reason, by design (decision 3). Builders on the other lines are told this, and the checks will run on everything at the final merge.

## Review

The checks are tests and a docs script, with no runtime or security surface, so they had no separate security review. They pass on the whole tree: the link check found 81 files, 0 broken and 0 warnings. `make check`, `make test` (327 Vitest tests), `make build` and e2e (23 of 23) all passed.
