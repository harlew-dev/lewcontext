# Workspace members

Status: **implemented 2026-10-31**. The decisions were built as recommended. Needs [workspace permissions](workspace-permissions.md). [Workspace invitations](workspace-invitations.md) adds pending invitations and the Invite button to the page built here.

## Why

Nobody can see who else is in a workspace. Workspace settings needs a **Members** list (name, email, role, date added), sorted by role (owner, admin, member, viewer). **Only owners and admins can see it.** Members and viewers never see who else is in the workspace or their emails. Once people can be invited, owners and admins also need to change a role, remove someone, and let someone leave.

## Flow

```
/app/<wsp>/settings            General  |  Members      ← Members tab for owners and admins only
/app/<wsp>/settings/members
  GET /api/workspaces/{ws}/members?page=&q=             workspace.member.index (owners, admins)
  table: Member (name, email, "You") · Role · Added · ⋯
    ⋯ Change role  → Dialog + Listbox (assignableRoles)
                     PATCH …/members/{usr} {role}   (workspace.member.update)
    ⋯ Remove       → confirm Dialog
                     DELETE …/members/{usr}         (workspace.member.destroy)
/app/<wsp>/settings  "Leave workspace" → confirm
                     DELETE …/membership            (any member, not the last owner)
                     → /app (next workspace, or onboarding)
```

## API

| Endpoint | Requires | Success | Failure |
| --- | --- | --- | --- |
| `GET /api/workspaces/{ws}/members` | `workspace.member.index` | one page of members ([pagination.md](../../docs/api/pagination.md)) | 400 bad params · 403 member or viewer |
| `PATCH /api/workspaces/{ws}/members/{member}` | `workspace.member.update`, CSRF, Origin | 200 with the member | 403 role not assignable · 404 not a member of this workspace · 409 last owner · 422 bad role |
| `DELETE /api/workspaces/{ws}/members/{member}` | `workspace.member.destroy`, CSRF, Origin | 204 | 403 role not assignable · 404 · 409 last owner · 422 yourself ("To leave, use Leave workspace.") |
| `DELETE /api/workspaces/{ws}/membership` | `auth.AnyMember`, CSRF, Origin | 204 | 409 last owner |

`{member}` is the user's `usr_` public ID. Leaving is a `destroy` of *your own* membership, so it's a singleton resource, not a verb (CRUD plan).

```json
{ "id": "usr_2yEHYLwiwPVhLW", "name": "Ada Lovelace", "email": "ada@example.com",
  "role": "admin", "addedAt": "2026-10-30T12:00:00Z" }
```

- **List parameters:** `page`, `perPage`, `q` (name or email, same rules and escaping as projects). There is no `sort` parameter. The order is always role, then name: `OrderExpr("array_position(?::text[], m.role), lower(u.name), u.id", pgdialect.Array(auth.Roles))`. The order comes from `auth.Roles` through a placeholder, so it can't drift from Go and no SQL text comes from input.
- **`addedAt`** is `workspace_user.created_at`.
- **Change a role:** body `{role}`, unknown fields rejected. Both the target's current role and the new role must be in the actor's `AssignableRoles()`, otherwise 403. Setting the same role is a 200 with no event.
- **Scope:** the member is looked up by `workspace_id` (from `requireWorkspace`) and `users.public_id`. A user in another workspace, or no user, gets the same 404.

### Store

- `IndexMembers(ctx, workspaceID, filter, req)`: `workspace_user` joined to `users`, scoped first by workspace.
- `UpdateMemberRole(ctx, workspaceID, actor, memberPublicID, role)`, `RemoveMember(…)` and `LeaveWorkspace(ctx, workspaceID, userID)` each run in one transaction:
  1. lock the **workspace row** `FOR UPDATE`, which serialises every membership change in that workspace, including accepting an invitation;
  2. load the target's membership;
  3. if the change would leave no owner (demoting, removing or leaving as the only owner), return `ErrLastOwner` → 409 "A workspace needs at least one owner.";
  4. write, and record the event.

  Without step 1, two owners demoting each other at the same moment could both see "another owner exists" and leave none.

### Events

| Event | Payload | Actor |
| --- | --- | --- |
| `workspace.member_updated` | `workspace`, `user`, `changes` (`role: [from, to]`) | who changed it |
| `workspace.member_removed` | `workspace`, `user`, `role` | who removed them |
| `workspace.member_left` | `workspace`, `role` | the member |

All three are scoped to the workspace. There's no email in the payloads, only public IDs.

## What removal does

- The removed user's next request to that workspace gets 404. In the web app, their open tab shows the existing workspace not-found page, and the switcher drops the workspace on its next refresh. Their sessions and account are untouched.
- Their projects stay in the workspace. Projects have no owner column.
- After leaving, the web app refreshes the workspace list and goes to `/app`, which opens the next workspace or onboarding.

## Web app

- `src/routes.ts`: `app/:workspace/settings/members` → `pages/app/[workspace]/settings/members.tsx`. `lib/routes.ts`: `routes.members.index(ws)`, and the `?next=` allow-list gains the path.
- **Settings tabs:** `components/app/workspaces/settings-tabs.tsx` (General, Members), built on the existing `Navbar` items with `current`. It is shared by two pages, so it goes in `components/app/` (rule 3). The Members tab shows only when `can(ws, "workspace.member.index")`. Members and viewers see General with no tabs.
- **Opening the members URL without the permission** shows `<Notice>` "Only owners and admins can see this workspace's members." It doesn't fetch, so there's no 403 round trip.
- **Members page** (`MemberIndex`):
  - `PaginatedTable` with a search input, following [docs/ui/pagination.md](../../docs/ui/pagination.md);
  - `ListSkeleton` while loading; Retry on failure, keeping the shown page; `EmptyState` for no matches;
  - each row shows an initials `Avatar`, the name with a "You" badge on your own row, the email, a role `Badge`, and "Added" via `lib/format.ts`;
  - a `⋯` `Dropdown` shows only on rows you can act on: `can(ws, "workspace.member.update" | "workspace.member.destroy")`, the row's role in `assignableRoles`, and not yourself.
- **Change role:** a `Dialog` with a `Listbox` of `assignableRoles` and `<Button loading>`. On success: toast, refresh the page. When the change is your own role, also refresh the workspace (permissions change).
- **Remove:** a confirm `Dialog` ("Remove Ada from Acme? They lose access at once. Their projects stay."), with a destructive button. `Button` has only `dark/zinc` today, so it gains a `red` colour (a complete class string, like the existing one). Remove, Leave and Revoke are the app's first destructive actions.
- **Leave workspace:** a section at the end of General settings, with a confirm dialog. The last owner sees the section disabled with the reason.
- `lib/api/members.ts`: `index`, `update`, `destroy`, and `leave` on `api.workspaces()` (or `membership.destroy`, see decision 5), with typed paths in `paths`.

## Seed

The demo seed creates Acme with four more members (one admin, two members, one viewer). They are fake `@example.test` users with random unusable passwords, created only when the seed creates Acme, so an existing account is still left unchanged. Without them the page would show one row.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| Admin escalates (promotes self, demotes or removes an owner) | `AssignableRoles` checks both the current and the new role. Admins can't target admins or owners. Acting on yourself goes through Leave, not `members.destroy`. |
| Workspace left with no owner | `ErrLastOwner` under a workspace row lock. |
| Removing a member of another workspace by ID | Lookup scoped by `workspace_id`; the same 404 for "not here" and "no such user". |
| Probing which users exist | `usr_` IDs are unguessable, and a miss is always 404. Only owners and admins see members' IDs and emails. |
| Members and viewers learn who else is in the workspace, or their emails | The list needs `workspace.member.index` (owners and admins); everyone else gets 403 and no tab. No other response carries other members' details. Projects have no creator field, and member events are only readable in the database. |
| A removed member keeps cached data in an open tab | Every API call rechecks membership. SWR keys include the user ID. The tab shows not-found on its next fetch. |

## Tests

- **Go integration:**
  - **List:**
    - the order is owner → admin → member → viewer, then by name;
    - pagination and `q` (escaping `%`);
    - `addedAt` matches `created_at`;
    - another workspace's members never appear;
    - owners and admins may list;
    - members and viewers get 403;
    - a non-member gets 404.
  - **Change a role:**
    - an owner promotes a member to owner;
    - an admin changes member ↔ viewer;
    - an admin can't target an admin or owner, or assign admin (403);
    - the only owner demoting self gets 409;
    - two owners demoting each other concurrently leaves one owner;
    - an unknown role or extra field gets 422 or 400;
    - a viewer gets 403;
    - a member of another workspace gets 404;
    - the event records `changes`.
  - **Remove:**
    - the removed user then gets 404 on the workspace;
    - yourself gets 422;
    - the last owner gets 409;
    - removing a user who isn't a member gets 404;
    - the event is recorded.
  - **Leave:**
    - a viewer may leave;
    - the last owner gets 409;
    - afterwards the workspace is gone from `GET /api/workspaces`.
  - The role × route matrix from the permissions plan gains these routes.
- **Vitest:**
  - members and viewers get no Members tab, and the members URL shows the notice without a request;
  - the menu shows only on manageable rows, never on your own;
  - the role listbox offers only `assignableRoles`;
  - the remove dialog's pending and failure states.
- **Playwright:** an owner (demo seed) changes a member to viewer, then removes them, and the row disappears.

## Docs

`docs/api/endpoints.md` (routes, the member shape), `docs/api/auth.md` (removal, last owner), `docs/api/events.md` (catalogue), `docs/api/pagination.md` (member parameters), `docs/api/seeding.md` and AGENTS.md (the demo seed's members), and `docs/ui/pages.md` (settings tabs, members page).

## Decisions

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Where Members goes | Its own settings page, `/app/<wsp>/settings/members`, behind General / Members tabs. It needs its own `?page=` and `?q=`, and pending invitations add a second list. |
| 2 | Who can see the member list and emails | Owners and admins only (`workspace.member.index`). Members and viewers can still leave from General settings. |
| 3 | Order | Role (owner, admin, member, viewer), then name. No user-chosen sort. |
| 4 | Change role and leave | Include both. Neither was asked for, but without them an invite can't be undone except by removal, and nobody can leave. |
| 5 | Leave route | `DELETE /api/workspaces/{ws}/membership` (your own membership as a singleton resource), rather than `POST …/leave`. |
| 6 | Last owner | 409, under a workspace row lock. Owners can promote another owner first, which doubles as ownership transfer. |
| 7 | Demo members | Add four fake members to Acme in the demo seed (this changes AGENTS.md's description of the seed). |

## Not included

- Ownership transfer as its own flow (promote, then demote yourself covers it).
- Deleting the workspace when its last member leaves (the last owner can't leave).
- Notifying someone by email when they are removed or their role changes.
- Member profiles or avatars beyond initials.

## As built

Where the build differs from the plan above:

- **The actor's role is read again under the workspace lock**, not taken from `requireWorkspace`, so an admin demoted a moment earlier can't still act. Store methods take the actor's user ID (`UpdateMemberRole(ctx, w, actorID, memberID, role)`), and `auth.Role.Manages(target)` checks a role against `AssignableRoles()`.
- **Two owners demoting each other:** one gets 200, the other 403 (by then they're a member, so they can no longer act on an owner) or 409. One owner always remains. The test fails without the lock.
- **Removing the last owner** can't happen through the API (only owners can remove owners, and you can't remove yourself), so the store keeps the check as a guard and the tests cover it through role changes and leaving.
- **`canLeave` in the workspace response:** every workspace response (list, show, store, update) carries `canLeave`, false only for a workspace's only owner. It's read in the same query as the role (`m.role <> 'owner' OR EXISTS` another owner), so General settings disables Leave without loading members; only the Members tab loads them. After a role change or removal the members page refreshes the workspace too, since another owner appearing or going changes `canLeave`.
- **No toast after leaving:** `/app` sends you to your next workspace or to onboarding, which has no toaster, so the page you land on is the confirmation.
- **Store list functions are `Index<Resources>`:** `IndexMembers`, with `ProjectPage` and `WorkspacePage` renamed to `IndexProjects` and `IndexWorkspaces` to match (recorded in the CRUD convention plan).
- **Names:** the rest of the CRUD convention isn't implemented yet, so the page route is `routes.members(ws)`, the client is `api.members(ws).index/update/destroy` and leaving is `api.workspaces().leave(id)`.
- **Moved to shared code**, now that two pages use them: `SearchField` (`components/ui/search-field.tsx`), `cleanSearch` (`lib/pagination.ts`), `initials` (`lib/format.ts`), and `RoleBadge` (`components/app/workspaces/role-badge.tsx`). `DropdownButton` accepts `plain`/`outline`, and `Button` gained `color="red"`.
- **The no-permission notice** uses `Notice variant="error"`; there is no neutral variant.
- **Demo members** get random fake names from the `Users` factory and a password hash of random text (`unusable:…`), which never verifies.
- **Playwright** signs up its own owner and adds the member in SQL, instead of changing the demo seed's members, so it stays self-contained and repeatable.

Docs updated: [endpoints.md](../../docs/api/endpoints.md), [auth.md](../../docs/api/auth.md#workspaces-and-roles), [events.md](../../docs/api/events.md), [api/pagination.md](../../docs/api/pagination.md), [seeding.md](../../docs/api/seeding.md), [api/testing.md](../../docs/api/testing.md), [ui/pages.md](../../docs/ui/pages.md), [ui/design.md](../../docs/ui/design.md), [ui/pagination.md](../../docs/ui/pagination.md), [ui/README.md](../../docs/ui/README.md), [ui/testing.md](../../docs/ui/testing.md) and AGENTS.md.
