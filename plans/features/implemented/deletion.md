# Deleting projects, workspaces and accounts

Status: **implemented 2026-10-31**. Uses the permissions from [workspace permissions](workspace-permissions.md) (`project.destroy`, `workspace.destroy`) and the workspace lock and last-owner rule from [workspace members](workspace-members.md). Names follow the [CRUD convention](crud-routing-convention.md).

Docs updated: [`docs/api/endpoints.md`](../../../docs/api/endpoints.md#deleting), [`auth.md`](../../../docs/api/auth.md#deleting), [`events.md`](../../../docs/api/events.md), [`database.md`](../../../docs/api/database.md), [`pagination.md`](../../../docs/api/pagination.md), [`testing.md`](../../../docs/api/testing.md), [`docs/ui/design.md`](../../../docs/ui/design.md#confirmations), [`pages.md`](../../../docs/ui/pages.md), [`api.md`](../../../docs/ui/api.md), [`README.md`](../../../docs/ui/README.md), [`testing.md`](../../../docs/ui/testing.md), and AGENTS.md.

**Where the build differs from the plan:**

- **After deleting the account**, the app goes to login with the notice "Your account has been deleted." (decision 11), not `/` with a toast: the homepage mounts no `Toaster`.
- **Onboarding can delete the account.** An account with no workspace can't open `/app/settings` (the layout needs a workspace), so once someone deleted the last workspace they owned there was nowhere to delete the account. Onboarding has a ⋯ menu at the top right with "Signed in as …", Sign out (moved there from the bottom of the page) and Delete account. `DeleteAccountDialog` lives in `components/app/account`, since both pages use it.
- **After deleting a workspace**, the app opens your next workspace's dashboard with the toast, or, for your last one, onboarding with "<name> deleted." in navigation state. Going through `/app` lost the toast: React Router commits the redirect after `navigate()` resolves, so it went to the app layout's `Toaster` as it unmounted.
- **Leave is hidden, not disabled,** for a workspace's only owner; their danger zone shows only Delete workspace.
- **`SettingsSection` gained `stacked`** (content under the title at full width), and danger zones are always stacked, with `DangerAction` rows: text on the left, button on the right. Settings actions (Save, Change password, Sign out other sessions) are right-aligned too, like dialog actions.
- **The project menu item says "Delete project"**, not "Delete".
- **The last-owner warning** shows each workspace with its `WorkspaceAvatar`, as the switcher does, and the account's danger zone says up front how many workspaces block it.
- **Password checks are audited** (decision 12) and **signing out clears the SWR cache** too (decision 13), through `useClearCache`, which deletes the cache's entries directly: SWR's filtered `mutate` skips infinite-list keys.
- **`ApiError` gained `details`**, the rest of the error body, so the client reads the 409's workspaces (`lastOwnerWorkspaces`).
- The 409's list type is `store.WorkspaceSummary` (`id`, `name`).
- Invitations don't exist yet, so nothing deletes them; their table should cascade from `workspaces` when the invitations plan adds it.

## Why

Nothing can be deleted today. A project made by mistake stays forever, and so does a workspace. The only way to leave the app is to stop signing in. This plan adds deleting a project, a workspace, and your own account.

**Deletion is permanent and immediate.** There is no trash, restore or grace period. The database removes everything that belongs to the deleted row (`ON DELETE CASCADE`), in the same transaction. Soft deletes may come later; nothing here makes them harder.

## Flows

```
Project   ⋯ menu (project table row, or project page header) → Delete (red)
            → ConfirmationDialog "Delete Website redesign?"
            DELETE /api/workspaces/{ws}/projects/{prj}            project.destroy (owner, admin)
            → 204, toast "Project deleted."; from its own page → back to Projects

Workspace /app/<wsp>/settings → Danger zone → Delete workspace (red)  (owners only)
            → ConfirmationDialog: type the workspace name + your password
            DELETE /api/workspaces/{ws} {name, password}           workspace.destroy (owner)
            → 204, toast "Acme deleted." → /app (next workspace, or onboarding)

Account   /app/settings → Danger zone → Delete account (red)
            a workspace you can't leave?  → Warning dialog: "Delete these workspaces first"
                                            with links to each one's settings, and no Delete button
            otherwise                     → ConfirmationDialog: your password
            DELETE /api/auth/me {password}
            → 204, session cookie cleared, SWR cache cleared
            → /auth/login with the notice "Your account has been deleted."
```

## API

| Endpoint | Requires | Success | Failure |
| --- | --- | --- | --- |
| `DELETE /api/workspaces/{ws}/projects/{project}` | `project.destroy`, CSRF, Origin | 204 | 403 · 404 (missing, or another workspace's) |
| `DELETE /api/workspaces/{ws}` | `workspace.destroy`, CSRF, Origin, rate limit | 204 | 403 · 422 `field: "name"` ("Type the workspace's name to confirm.") or `field: "password"` · 429 / 503 |
| `DELETE /api/auth/me` | Session, CSRF, Origin, rate limit | 204, cookie cleared | 422 `field: "password"` · 409 last owner of a workspace (below) · 429 / 503 |
| `GET /api/workspaces?canLeave=false` | Session | one page of the workspaces you can't leave: those where you're the **only** owner | 400 bad params |

- **The name check is on the server too.** It compares the current name, so a tab opened before a rename can't delete the workspace under its old name.
- **Password checks** work as `POST /api/auth/password/verify` does: `limitAuth` (`auth:per-ip`, one of the 4 hashing slots) and `auth:per-account`. These routes pass no CAPTCHA action, so they are never challenged ([CAPTCHA plan](captcha-challenges.md)).
- **DELETE with a body:** `security` today skips the JSON content-type check for DELETE. It changes to require `application/json` whenever a DELETE has a body. Bodies stay bounded (16 KiB).
- **The 409 for a last owner** lists what to deal with, so the web app can switch to the warning dialog even when the pre-check was stale:

  ```json
  { "error": "Delete the workspaces you own first, or make someone else an owner.",
    "workspaces": [ { "id": "wsp_2yEHYLwiwPVhLW", "name": "Acme" } ] }
  ```

  The quota plan caps owned workspaces at 20, so the list is short. It's capped at 100 anyway.
- **`canLeave=false`** filters the existing workspace index ([pagination.md](../../../docs/api/pagination.md)) by the `canLeave` field every workspace response already carries (members plan): false only when your role is `owner` and no other membership in that workspace has `owner`. Deleting your account means leaving every workspace, so the workspaces that block it are exactly the ones you can't leave. The `page`/`perPage` rules are unchanged; `canLeave` is added to the allowed parameters and accepts only `false` (anything else is 400), the one value with a use.

## Data

### What a delete removes (cascades)

| Deleted | Removed with it | Kept |
| --- | --- | --- |
| project | nothing else references a project | its events: payloads hold `prj_…` only, with no foreign key |
| workspace | `workspace_user`, `projects`, `workspace_invitations` (planned) | **its events**, with `workspace_id` set to null (decision 3) |
| user | `sessions`, `workspace_user`, `password_resets`, and **events about the account** (`events.user_id`) | events where they acted for others: `actor_id` set to null (as today). Invitations they sent: `invited_by` set to null (invitations plan) |

### Migration change (squashed, `007_create_events`)

```sql
workspace_id bigint REFERENCES workspaces(id) ON DELETE SET NULL,  -- was CASCADE
```

Without this, deleting a workspace also deletes its whole history, including the `workspace.deleted` event recorded in the same transaction. With it, the history stays readable through the public IDs in payloads until `make prune` removes it after 90 days. `events.user_id` stays `CASCADE`: a deleted person's sign-ins, IPs and password changes go with them. Rebuild with `make migrate-fresh CONFIRM=1 && make seed`.

### Store

- `DeleteProject(ctx, workspaceID, publicID)`: deletes the project, scoped by workspace, and records `project.deleted` in one transaction. `sql.ErrNoRows` → 404.
- `DeleteWorkspace(ctx, workspaceID, name)`: locks the workspace row `FOR UPDATE` (the members plan's lock, so a concurrent role change, removal or invitation acceptance waits), compares `name`, records `workspace.deleted`, then deletes the row. The cascades and `SET NULL` do the rest.
- `DeleteAccount(ctx, user)`, in one transaction:
  1. **Lock every workspace you own, in `id` order:** `SELECT … FROM workspaces JOIN workspace_user … WHERE user_id = ? AND role = 'owner' ORDER BY id FOR UPDATE`.
  2. For each, check whether you can leave it, with the same expression as the `canLeave` field. Any you can't returns a `LastOwnerError{Workspaces}` → 409, and nothing is deleted. It matches the members plan's `ErrLastOwner` with `errors.Is`, so the concept keeps one name.
  3. Record `auth.account_deleted`, delete the user row, and let the cascades run.

  **This lock stops two owners stranding a workspace.** Say Ada and Bo both own Acme and both delete their accounts at the same moment. Without a lock, each sees the other as an owner and both succeed, leaving Acme with members but no owner. With it, Ada's transaction locks Acme, sees Bo, and deletes Ada. Bo's waits for the lock, then counts only Bo and gets 409. Every membership change (demote, remove, leave, accept an invitation, delete the workspace) takes the same workspace lock first, so none of them can slip in between either. Locking in `id` order means two transactions that own the same workspaces always take the locks in the same order, so they can't deadlock.
- `IndexWorkspaces` gains a `CanLeave *bool` filter field (nil: no filter) for the `canLeave=false` page. A filter is a field on the query, not a new method ([naming](../../../docs/naming.md#store-methods-internalstore)).
- **One expression for "can leave".** The `can_leave` column expression in `memberWorkspaces` (`m.role <> 'owner' OR EXISTS` another owner) becomes one helper, used by the response field, the filter and `DeleteAccount`'s check, so the three can't disagree.

## Events

| Event | Payload | Scope | Actor |
| --- | --- | --- | --- |
| `project.deleted` | `project`, `workspace`, `name` | the workspace | whoever deleted it |
| `workspace.deleted` | `workspace`, `name`, `projects` (how many), `members` (how many) | the workspace, which becomes null once deleted | the owner who deleted it |
| `auth.account_deleted` | `user` (`usr_…` only) | none: a user scope would be cascaded away with the user | none (it would be set to null anyway) |

`auth.account_deleted` holds no email or name, because the account's personal data is what's being erased.

The password checks for deleting a workspace or an account record `auth.password_checked`, as `verify` does, with the steps `destroy_workspace` and `destroy_account` (decision 12). A successful account deletion removes its own check along with the account's other events.

## Web app

### `ConfirmationDialog` (`components/ui/confirmation-dialog.tsx`)

One dialog for every "are you sure?", built on `Dialog`:

```tsx
<ConfirmationDialog
  open={open}
  onClose={() => setOpen(false)}
  title="Delete Acme?"
  description="This permanently deletes the workspace, its 12 projects and every membership. It can't be undone."
  confirm="Delete workspace"          // the button's label
  variant="danger"                    // red confirm button; "default" is dark/zinc
  disabled={typed !== workspace.name || password === ""}
  onConfirm={() => api.workspaces().destroy(workspace.id, { name: typed, password })}
  onConfirmed={() => { refresh(); navigate(routes.app); toast.success("Acme deleted."); }}
>
  {/* optional extra fields: type-to-confirm, password */}
</ConfirmationDialog>
```

- **Pending state:** `onConfirm` runs through `useAction`, so the confirm button shows `loading`, a double click submits once, and Cancel and closing are disabled while it runs.
- **On failure** the dialog stays open, with the error in an `ErrorNotice`, and the typed fields kept. On success it closes, then calls `onConfirmed`.
- **Props:** `title`, `description` and `confirm` are required. `variant` follows AGENTS rule 6 (a `variant` prop, not one component per kind). `children` holds extra fields.
- Every other confirmation uses it too. The members page's existing confirmations (remove, leave; implemented with the members plan) switch to it in this change, and the invitations plan's Revoke uses it.

### Danger zone

- `SettingsSection` gains `variant="danger"`. It shows a red title, and the content sits in a red-tinted bordered panel (`border-red-600/20 bg-red-500/5`, with `dark:` variants). Each row in the panel is an action: a short explanation and a red `Button`.
- **Workspace settings (General), at the bottom:**
  - **Leave workspace**, for any member. This moves here from the members plan's section; when `workspace.canLeave` is false (the last owner) it's disabled, with the reason.
  - **Delete workspace**, only when `can(ws, "workspace.destroy")`.
  - Members, admins and viewers see only Leave.
- **Account settings, at the bottom:** **Delete account**.

### Deleting your account

- The page loads `useResource(paths.workspaces.index({ canLeave: false }))` with the rest of the page, so the button already knows which dialog to open. While it's loading, the button shows `loading`.
- **Workspaces you can't leave (you're the only owner):** a page-local `LastOwnerDialog` in `pages/app/settings.tsx` (used only there, rule 3), built on `Dialog`:

  > **Delete your workspaces first**
  > You're the only owner of these workspaces. Delete them, or make someone else an owner, before deleting your account.
  > - **Acme** →
  > - **Side project** →
  >
  > [Close]

  Each item links to that workspace's settings (`routes.workspaces.edit(ws)`), where the Danger zone and Members are. It has no delete button. A workspace you share ownership of isn't listed and doesn't block.
- **Otherwise:** `ConfirmationDialog`. Title "Delete your account?". Description: "This permanently deletes your account and removes you from N workspaces. It can't be undone." Plus a password field (`autocomplete="current-password"`).
- **A 409 from the API** (the list changed after the page loaded) closes the confirmation and opens `LastOwnerDialog` with the workspaces from the response.
- **On success:** clear the SWR cache (signing out now does the same, through one helper, so no user data stays in memory), then go to login with a success notice, "Your account has been deleted.", passed in navigation state (`authPageState`, as the reset link does). The homepage has no `Toaster`, so it can't show a toast.

### Deleting a project

- `components/app/projects/project-actions.tsx`: `ProjectActions`, a `⋯` `Dropdown` (`EllipsisHorizontalIcon`, `DropdownButton plain`, `aria-label="Actions for Website redesign"`). It is used in two places, each project table row and the project page header (next to Edit), so it lives in `components/app/projects`. It shows only when `can(ws, "project.destroy")`.
- **One item, "Delete", in red:** `DropdownItem` gains `variant="danger"` (red text and icon, `TrashIcon`). It opens a `ConfirmationDialog`. Title "Delete Website redesign?". Description: "This permanently deletes the project. It can't be undone."
- **In the table**, the `⋯` button is its own focusable control, and clicking it doesn't open the row's link.
- **After deleting:** invalidate every key in `projectRoutes(ws).matches`, which refreshes the list and the dashboard counts. From the project's own page, also remove its `show` key, so going Back shows "not found" rather than stale data, then navigate to the projects index.

### API client

- `api.projects(ws).destroy(id)`, which uncomments the existing stub;
- `api.workspaces().destroy(id, { name, password })`;
- `api.auth.destroy({ password })`;
- `paths.workspaces.index({ canLeave: false })`: `WorkspaceIndexQuery` gains `canLeave?: false`.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| A stolen session destroys a workspace or an account | Both need the password, under sign-in rate limits and hashing slots. Deleting a project doesn't, because its blast radius is one project. |
| Deleting by accident | Type-to-confirm for workspaces, checked on the server against the current name. A confirmation dialog for everything. Red, separated danger zones. |
| **Two owners strand a workspace** by deleting their accounts at once | `DeleteAccount` locks every owned workspace in `id` order and recounts owners. Every membership change takes the same lock. |
| An admin, member or viewer deletes a workspace | `workspace.destroy` is owner only (the permission matrix). The role × route matrix covers the new routes. |
| Deleting another workspace's project by ID | Scoped by `workspace_id`; a miss is 404. |
| Deleting a workspace erases its audit trail | `events.workspace_id` becomes `SET NULL`, so events survive until pruned. |
| Personal data left behind after deleting an account | Account events cascade with the user. Emails in payloads that aren't linked to the account (sign-in attempts typed before the account existed, reset requests, invitations sent to it) are removed by the 90-day prune (decision 9). |
| Cached data in open tabs | Every request rechecks the session and membership: other members get 404, and a deleted account's other tabs get 401 and go to login. This tab clears SWR. |
| CSRF on a destructive DELETE | The existing Origin check, the CSRF token, and now the JSON content type when there's a body. |

## Tests

- **Go integration:**
  - **Project:**
    - owners and admins get 204; members and viewers get 403;
    - another workspace's project, or a second delete, gets 404;
    - the row is gone, and `project.deleted` is recorded.
  - **Workspace:**
    - an owner gets 204; an admin gets 403;
    - a wrong or stale name gets 422 `name`; a wrong password gets 422 `password`;
    - the rate limit applies;
    - memberships, projects and invitations are gone;
    - its events remain with a null `workspace_id`, including `workspace.deleted`;
    - other members then get 404, and an invitation link for it no longer works;
    - concurrent with a member's role change or an invitation acceptance: they are serialised, and nothing is left pointing at the deleted workspace.
  - **Account:**
    - a wrong password gets 422 and nothing changes;
    - **the last owner of a workspace gets 409 with the workspaces listed, and nothing is deleted**, including for a workspace where you're the only member;
    - a co-owner succeeds and the workspace keeps its other owner;
    - **two co-owners of one workspace deleting their accounts concurrently: exactly one succeeds, the other gets 409, and the workspace still has an owner**;
    - two users who each co-own the same two workspaces deleting concurrently don't deadlock;
    - sessions are gone, the cookie is cleared, and the old session gets 401;
    - the account's events are gone, while other users' events keep a null actor;
    - the email can be registered again;
    - `canLeave=false` lists exactly the workspaces whose `canLeave` is false in `GET /api/workspaces` (owner alone, owner with a co-owner, admin, member, viewer), and `canLeave=true` or `canLeave=0` is 400.
  - **Content type:** a DELETE with a non-JSON body gets 415.
  - **Matrix:** the role × route matrix gains the two workspace DELETE routes.
- **Vitest:**
  - **`ConfirmationDialog`:**
    - `disabled` keeps confirm off;
    - while pending: loading, Cancel disabled, can't close;
    - a failure keeps the dialog open with the error and the fields;
    - success closes it and calls `onConfirmed`.
  - **Account Danger zone:**
    - with workspaces you can't leave, it opens the warning with links and no delete button;
    - otherwise the password confirmation;
    - a 409 switches to the warning.
  - **Workspace Danger zone:** Delete only for owners; Leave for everyone; the type-to-confirm gate.
  - **`ProjectActions`:** hidden without `project.destroy`; the danger item is red; deleting refreshes the list.
- **Playwright:**
  - delete a project from the table;
  - an owner deletes their only workspace and lands in onboarding;
  - delete account while the only owner of a workspace → follow the link → delete the workspace → delete the account → signed out, and signing in fails.

## Docs

- `docs/api/endpoints.md`: the DELETE routes, the `canLeave=false` filter, the 409 shape.
- `docs/api/pagination.md`: `canLeave` in the workspace list's parameters.
- `docs/api/database.md`: the cascade table and `events.workspace_id SET NULL`.
- `docs/api/events.md`: the three events.
- `docs/api/auth.md`: account deletion, password checks, and DELETE bodies needing JSON.
- `docs/ui/design.md`: `ConfirmationDialog`, `SettingsSection variant="danger"`, `DropdownItem variant="danger"`.
- `docs/ui/pages.md`: the danger zones.
- AGENTS.md: list `ConfirmationDialog` with the shared primitives.

## Decisions (confirmed 2026-10-31)

| # | Question | Decision |
| --- | --- | --- |
| 1 | Permanent or soft | Permanent. Soft deletes are a possible future plan. |
| 2 | Immediate, with cascades | Immediate, in one transaction, with the database's `ON DELETE CASCADE` removing what belongs to the row. |
| 3 | The audit trail | Workspace events survive their workspace (`events.workspace_id` → `SET NULL`). Account events go with the account (`events.user_id` stays `CASCADE`). |
| 4 | What confirms what | Project: the dialog only. Workspace: type its name, plus your password. Account: your password. |
| 5 | Last owner deleting their account | A warning dialog listing the workspaces you're the only owner of, with links and no delete button. Shared-ownership workspaces don't block. Enforced on the server (409) under a lock on every owned workspace, taken in `id` order. |
| 6 | How the page knows | `GET /api/workspaces?canLeave=false`, a filter on the `canLeave` field workspaces already carry, loaded with the settings page, plus the 409 as the backstop. The field, the filter and the 409 share one SQL expression. (Replaces an earlier `soleOwner=true`, which named the same condition a second way.) |
| 7 | `ConfirmationDialog` | In `components/ui`, with `title`, `description`, `confirm`, `variant` (`danger` or `default`), `disabled`, `onConfirm`, `onConfirmed` and `children`. It is used for every confirmation, including the members plan's. |
| 8 | Danger zone | `SettingsSection variant="danger"` at the bottom of workspace and account settings. Leave workspace moves into it. |
| 9 | Emails left in unlinked event payloads | Rely on the 90-day prune rather than scrubbing payloads at deletion time. Scrubbing JSON payloads by email is a separate erasure feature if it's ever needed. |
| 10 | Password in a DELETE body | Yes, with JSON required for any DELETE that has a body. The alternative, verifying first and deleting second, isn't atomic. |
| 11 | After deleting your account | Clear the SWR cache, then go to login with a success notice in navigation state. (Was `/` with a toast, but the homepage mounts no `Toaster`.) |
| 12 | Audit the password checks | Record `auth.password_checked` with the steps `destroy_workspace` and `destroy_account`, as `verify` does. |
| 13 | Clearing the SWR cache | One helper, used by account deletion and by signing out (which didn't clear it before). |

## Not included

- Soft delete, trash, restore, and grace periods.
- Exporting your data before deleting.
- Emailing a confirmation of the deletion (there's no email delivery).
- Deleting large workspaces in the background. With the 1,000-project quota, one transaction is fine.
- Deleting other people's accounts (no site admin).
- Scrubbing emails from event payloads at deletion time (decision 9).
- Deleting several projects at once.
