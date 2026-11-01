# Pages and routing

The web app is a single-page app on React Router v7 (framework mode, `ssr: false`), built by Vite. Public pages listed in `react-router.config.ts` (`prerender`) are rendered to HTML at build time; every other URL gets one shell (`__spa-fallback.html`) and renders in the browser. In production the Go server serves both ([api/README.md](../api/README.md#serving-the-web-app)).

It is built twice (`web-build.ts`): the app build has every page but the admin dashboard's; the admin build (`WEB_BUILD=admin`) has only the admin pages, its files under `/admin-assets/` and its one shell `index.html`, which Go serves only at the right key ([../api/admin.md](../api/admin.md#the-admin-build)).

## URLs

Route constants are grouped by resource with the page actions (`routes.projects.index(ws)`, `routes.projects.show(ws, prj)`, `routes.workspaces.edit(ws)`), and resource pages' default exports are `<Resource><Action>` (`ProjectIndex`); see [../naming.md](../naming.md#web-app).

Every page is registered with its URL in `src/routes.ts`; there is no file-system routing. It holds two lists, `appRoutes` and `adminRoutes`: `react-router build` takes one (by `WEB_BUILD`) and its not-found page, while `react-router typegen` and the dev servers take both, since typegen writes one set of types for the whole program and every dev server runs it. A new admin page goes in `adminRoutes`, a new app page in `appRoutes`.

| Path | Page | Notes |
| --- | --- | --- |
| `/` | `pages/home.tsx` | Public homepage, **prerendered** for search engines |
| `/auth/login`, `/auth/register` | `pages/auth/*` | Signed out. Login's `?notice=account-exists` shows a fixed notice, "You already have an account. Sign in here, or reset your password." (from the "You already have an account" email, or a sign-up's 409); any other `notice` shows nothing, and nothing from the URL is ever shown |
| `/auth/register/verify` | `pages/auth/register/verify.tsx` (`RegisterVerify`) | A sign-up's code step, with `EMAIL_VERIFICATION` on: register's 202 opens it, keeping `?next=` and the invitation state. Its loader (`registerVerifyLoader`) asks `register/check` for the email, so a reload (a phone switching to the mail app) resumes it; with no live sign-up it says so with Back to sign up. The code (`EmailCodeStep`), Send a new code, a line everyone sees ("If you blocked this address, no code can reach it: unblock it from the link in our email first."), Use a different email, Sign in. The right code goes to onboarding, or back to an invitation link; a 409 goes to login with the account-exists notice and the email in navigation state ([../api/auth.md](../api/auth.md#email-verification)) |
| `/auth/forgot-password` | `pages/auth/forgot-password.tsx` | Signed out: asks for a reset link. Login passes the typed email in navigation state, not the URL (`authPageState`) |
| `/auth/forgot-password/<token>` | `pages/auth/forgot-password/[token].tsx` | The reset link: checks it, then sets a new password and opens login with the email filled in. No `guestLoader`, so it works while signed in ([auth.md](../api/auth.md#password-reset-by-link)) |
| `/auth/invitations/<token>` | `pages/auth/invitations/[token].tsx` | An invitation's link. Its own `clientLoader` loads the session if there is one (no `guestLoader`), and it checks the link: signed out it offers Sign in to accept and Create an account (both return here); signed in as the invited email, **Join** (at once when arriving from those buttons, `{invitation: true}` state); as anyone else, Sign out and switch. Joining opens the workspace's dashboard, where the layout toasts "You joined Acme." (`arrivalState`); an account that must verify its email first goes to `/app/verify-email` with the link as `next`, and joins when it comes back ([auth.md](../api/auth.md#invitations)) |
| `/auth/email-blocks/<token>` | `pages/auth/email-blocks/[token].tsx` (`EmailBlockShow`) | A block link, from an invitation's footer or `List-Unsubscribe`. No loader, so it works signed in or out; malformed tokens are dead without a request. It checks the link, then: **Block this address** (the address and what blocking does), **Unblock** (blocked, with "Keep this email: its link lets you unblock this address for a year."), an address with an account (delete it first, with Sign in), or expired. Each switch shows "Address blocked." / "Address unblocked."; a failure keeps the page with the error ([../api/auth.md](../api/auth.md#blocking-an-email-address)). Never a `?next=` |
| `/app` | `pages/app/index.tsx` | Its loader redirects to the newest workspace's dashboard, onboarding, or login |
| `/app/verify-email` | `pages/app/verify-email.tsx` (`VerifyEmail`) | With `EMAIL_VERIFICATION_EXISTING=required`, an account that never verified its email lands here from every signed-in page (the loaders and `useResource` turn the API's 403 `verificationRequired` into a redirect, with `?next=`). Outside the app layout, like onboarding (`AuthLayout`). "You must verify your email to continue": Send code (nothing is sent until then), then `EmailCodeStep`, then `?next=` or `/app`; Sign out. A verified account, or verification off, goes straight on (`verifyEmailLoader`) |
| `/app/onboarding` | `pages/app/onboarding.tsx` | Signed in without a workspace: create the first one (`AuthLayout`). A ⋯ menu at the top right says who is signed in and offers Sign out and Delete account (settings need a workspace, so an account without one deletes itself here). After deleting or leaving the last workspace it says "<name> deleted." or "You left <name>." (`onboardingState`). Creating the workspace opens its dashboard, where the layout toasts "<name> created." (`arrivalState`) |
| `/app/settings` | `pages/app/settings.tsx` | Account & security (per user). With email verification on, the email's label ends in a Verified (lime) or Not verified badge, read as part of its name, and an unverified one's description offers Verify email: a dialog that sends a code (Send code) and takes it (renewing the session, "Email verified."), going back to Send code with the reason when the code has ended. The email address is read-only and starts blurred (`Input blurred`; in case the screen is shared): a click on it or the eye reveals it; the sidebar's account menu shows only the name. **Your data:** Download your data (`outline`), a `ConfirmationDialog` taking the password (a refusal on the field, a 429 kept in the dialog), then the API's JSON saved as `lewcontext-export-<date>.json` with a toast ([../compliance.md](../compliance.md#download-my-data)). Danger zone: Delete account, which loads `GET /api/workspaces?canLeave=false` with the page and, while you're the only owner of any workspace, opens a warning linking to each one's settings instead of the confirmation (the API's 409 switches to it too) |
| `/app/wsp_…/dashboard` | `pages/app/[workspace]/dashboard.tsx` | |
| `/app/wsp_…/projects` | `pages/app/[workspace]/projects.tsx` | New project only with `project.store`; each row's ⋯ menu offers Edit project and Delete project as on the project page (also on the dashboard's recent projects) |
| `/app/wsp_…/projects/prj_…` | `pages/app/[workspace]/projects/[project].tsx` | The ⋯ menu offers Edit project (`project.update`) and Delete project (`project.destroy`, returning to Projects); with neither, no menu. The details list shows the status as its badge |
| `/app/wsp_…/settings` | `pages/app/[workspace]/settings.tsx` (index of `settings/layout.tsx`) | Rename (`workspace.update`), copy the ID, and a danger zone: Leave (hidden when `workspace.canLeave` is false: the only owner) and Delete workspace (`workspace.destroy`: type the name, plus your password, under a warning callout). After either, the next workspace's dashboard opens with a toast, or onboarding. It loads no member data; only the Members tab does |
| `/app/wsp_…/settings/members` | `pages/app/[workspace]/settings/members.tsx` (in `settings/layout.tsx`) | Owners and admins (`workspace.member.index`): search members, change a role, remove; **Invite** (left of the search box) (`workspace.invitation.store`, an email and one of your `assignableRoles` but owner); and Pending invitations above the members while there are any ("Sending…", "Sent <date>" or Expired, polling while one is sending; Revoke invitation in the row's ⋯ menu). Anyone else sees a notice and nothing is fetched. Both settings pages share `SettingsTabs` (General / Members), shown only with that permission |
| `/admin/<key>` | `pages/admin/[key]/index.tsx` | The admin sign-in (`AdminLogin`), in the admin build: password, then the code, or setting MFA up (QR code hidden until revealed, then the recovery codes). Its loader checks the key with the API first: a wrong one is the not-found page ([../api/admin.md](../api/admin.md#web-app)). Links from the admin pages to the app (the logo, Back to app) land on the admin build's `pages/admin/not-found.tsx`, which loads them as a new page (`redirectDocument`) |
| `/admin/<key>/users`, `users/usr_…`, `workspaces`, `workspaces/wsp_…` | `pages/admin/[key]/*` inside `pages/admin/layout.tsx` | The admin dashboard (`AdminUserIndex`, `AdminUserShow`, `AdminWorkspaceIndex`, `AdminWorkspaceShow`): a user's and a workspace's page end with an **Actions** danger zone for operators (suspend, sign out, reset MFA; revoke pending invitations), each through `AdminActionDialog` ([../api/admin.md](../api/admin.md#actions)). Lists search and sort in the URL; rows open their page. A user's and a workspace's page link to their Activity (Events, filtered, `window=all`); a workspace's lists its projects. Not prerendered, never a `?next=` |
| `/admin/<key>/events`, `events/evt_…`, `mail` | `pages/admin/[key]/events.tsx`, `events/[event].tsx`, `mail.tsx` | The audit trail (`AdminEventIndex`: the time range, Filters and **Download CSV** in the header (the current filters' newest 10,000 rows from `GET /api/admin/events/export`, saved as a file, the toast saying how many, and to narrow the filters when the API says it left rows out (`X-Export-Truncated`); a refusal as a toast), the other filters in a dialog shown as removable badges, the charts from the newest page, all in the URL (`window`, `name` as comma-separated tokens, `outcome`, …; each change a navigation, so Back walks through them), Older / Newer by cursor), one event (`AdminEventShow`), and the mail queue (`AdminDeliveryIndex`: `?window=7d`, `?status=`, `?before=`) ([../api/admin.md](../api/admin.md#web-app)) |
| `/admin/<key>/email-blocks` | `pages/admin/[key]/email-blocks.tsx` | The block list (`AdminEmailBlockIndex`): search and sort in the URL, each row's ⋯ menu with Show activity (Events `?email=`, `window=all`) and, for operators, Unblock (`AdminActionDialog`: a reason, then a code when the API asks) ([../api/admin.md](../api/admin.md#email-blocks)) |
| `/admin/<key>/ip-blocks` | `pages/admin/[key]/ip-blocks.tsx` | The live IP blocks (`AdminIPBlockIndex`): Network, Reason, By, Expires, each row's ⋯ menu with Show activity (Events `?ip=`) and, for operators, Unblock; **Block an address** in the header (operators): the address or range, how long, a reason, then a code when the API asks ([../api/admin.md](../api/admin.md#ip-blocks)) |
| `/admin/<key>/monitoring`, `monitoring/security`, `monitoring/limits` | `pages/admin/[key]/monitoring/index.tsx`, `security.tsx`, `limits.tsx` | The Monitoring pages (`AdminMonitoringOverview`, `AdminMonitoringSecurity`, `AdminMonitoringLimits`), under the sidebar's Monitoring heading: the time range in the header and the URL (`?window=1h|7d`), one recorded read per load, signals as tiles linking to their Events rows ([../api/monitoring.md](../api/monitoring.md#web-app)) |
| `/admin/<key>/monitoring/ips`, `monitoring/ips/<ip>`, `monitoring/accounts`, `monitoring/workspaces`, `monitoring/usage` | `pages/admin/[key]/monitoring/ips.tsx`, `ips/[ip].tsx`, `accounts.tsx`, `workspaces.tsx`, `usage.tsx` | The Monitoring pages of part 2 (`AdminMonitoringIPs`, `AdminMonitoringAddress`, `AdminMonitoringAccounts`, `AdminMonitoringWorkspaces`, `AdminMonitoringUsage`): one address's page takes an address or an IPv6 /64 with its slash escaped (`2001:db8::%2F64`, `addressSegment`), its own recorded read; the IP list's sort and IPv6 switch are in the URL (`?sort=`, `?ipv6=each`) and only rearrange what was read ([../api/monitoring.md](../api/monitoring.md#ip-addresses)) |
| `/admin/<key>/monitoring/data` | `pages/admin/[key]/monitoring/data.tsx` | The Monitoring Data page (`AdminMonitoringData`): retention per prune target, the database's state, growth, tables and what fills the log ([../api/monitoring.md](../api/monitoring.md#data)) |
| `/admin/<key>/monitoring/integrity`, `monitoring/performance`, `monitoring/errors`, `monitoring/errors/<err>`, `monitoring/configuration` | `pages/admin/[key]/monitoring/integrity.tsx`, `performance.tsx`, `errors.tsx`, `errors/[error].tsx`, `configuration.tsx` | The Monitoring pages of part 3 (`AdminMonitoringIntegrity`, `AdminMonitoringPerformance`, `AdminMonitoringErrors`, `AdminMonitoringError`, `AdminMonitoringConfiguration`): one error group's page is its own recorded read (Errors current in the sidebar on it); the routes table's sort is in the URL (`?sort=p95`) and only rearranges what was read. Read-only: resolving a group waits for the admin actions ([../api/monitoring.md](../api/monitoring.md#errors)) |
| `/admin/<key>/admins` | `pages/admin/[key]/admins.tsx` | Who holds admin (`AdminAdminIndex`): roles, MFA, last admin sign-in (a warning after 90 days), live sessions, Download CSV and Sign out my other sessions ([../api/admin.md](../api/admin.md#the-admins-page)) |
| anything else | `pages/not-found.tsx` | Go answers it with status 404 |

`app/settings` and `app/onboarding` are fixed segments, so React Router ranks them above `app/:workspace/...`, and workspace IDs always start with `wsp_`, so they can't collide. There are no redirects from older URLs (`/app/dashboard`, `/app/projects/42`, `/login`).

## Signed-in pages

Pages under `pages/app/` (except `/app` and onboarding) sit inside the **layout route** `pages/app/layout.tsx` in `routes.ts`. It wraps them in `ApplicationLayout`, which stays mounted between pages (including when switching workspaces), so sidebar state and the sliding current-page indicator survive navigation. A page renders only its content, reads the session with `useSession()` and the workspace with `useWorkspace()`, and fetches its own data. The layout wraps each page in `PageTransition` keyed by pathname, so a new page fades in ([design.md](design.md#styling-rules)):

```tsx
// src/routes.ts, inside layout("pages/app/layout.tsx", [...])
route("app/:workspace/tasks", "pages/app/[workspace]/tasks.tsx"),

// pages/app/[workspace]/tasks.tsx
export default function TaskIndex() {
  const workspace = useWorkspace();
  const [search] = useSearchParams();
  const list = useResource(paths.tasks(workspace.id).index({ page: page(search) }));
  if (!list.data) return list.loading ? <ListSkeleton label="Loading tasks" /> : null;
  // …render list.data, list.error with Retry (Notice), an EmptyState
}
```

**Tabs share a layout.** Workspace settings' General and Members are children of `pages/app/[workspace]/settings/layout.tsx` (`route("app/:workspace/settings", layout, [index(general), route("members", members)])`), which renders the heading and `SettingsTabs` (current tab from the URL) and marks itself a tab group, so switching tabs slides the content instead of fading the whole page ([design.md](design.md#styling-rules)). The pages render only what's under the tabs.

**Moving between pages never waits.** Pages have no loaders: they render on the first frame and show skeletons while `useResource` loads ([api.md](api.md#reading-data-useresource-libuse-resourcets)). Revisiting a page shows SWR's cached data at once and refreshes it in the background. A record that doesn't exist (`useResource(...).missing`) renders "not found" inside the layout.

## Route loaders (`lib/loaders.ts`)

Loaders run in the browser before a route renders. They only decide what a page can't render without: the session and where to send the visitor.

| Loader | Route | Does |
| --- | --- | --- |
| `signedInLoader` | the app layout | Loads the session and page 1 of the member's workspaces, once (`shouldRevalidate` is false). 401 → `/auth/login?next=<this page>`; no workspace → `/app/onboarding`; API unreachable or failing → `ServiceUnavailable` (with a 500's reference: `unavailable(error)`) |
| `workspaceHomeLoader` | `/app` | Redirects to the newest workspace's dashboard or onboarding |
| `onboardingLoader` | `/app/onboarding` | Needs a session; accounts with a workspace go on to `/app` |
| `guestLoader` | login, register | Signed-in visitors go on to `?next=` or `/app`; if the API is down the form still renders |
| `registerVerifyLoader` | `/auth/register/verify` | `guestLoader`, then the browser's sign-up (`register/check`): `{pending}`, or `{ended}` with the API's reason |
| `verifyEmailLoader` | `/app/verify-email` | Needs a session (401 → login); a verified account, or verification off, goes on to `?next=` or `/app` |

`signedInLoader`, `workspaceHomeLoader` and `onboardingLoader` send an account that must verify its email (the API's 403 with `verificationRequired`) to `/app/verify-email`, with `?next=` from the layout's. The session carries `emailVerification` (`off`, `optional`, `required`) and the user `emailVerifiedAt`.
| `adminLoginLoader`, `adminLoader` (in `lib/admin.tsx`, out of the app build) | the admin sign-in page, the admin layout | `GET /api/admin/me` with the URL's key: 404 → the not-found page (a wrong key); signed in → Users, or the session (with its role, and `previousKey` from `X-Admin-Key-Status`) for the layout; 401 → the form, or back to it; API unreachable → `ServiceUnavailable` |

While the first page's loader runs, the root `HydrateFallback` shows an empty page. Loaders use `loaderApi()`, a read-only client ([api.md](api.md)).

**Go checks the session first.** For `/app` and `/app/*`, the Go server sends visitors without a valid session to `/auth/login?next=…` before serving any HTML. The 401 handling in the loaders covers a session that expires afterwards, and the Vite dev server, which has no such gate.

## The session and the workspace

The layout route puts the loaded session in `SessionProvider` (`lib/session.tsx`) and the current workspace in `WorkspaceProvider` (`lib/workspace.tsx`). The current workspace is the one in the URL, or the newest on pages outside a workspace (`/app/settings`). A workspace already in the switcher's list shows at once while `useResource(paths.workspaces.show(id))` refreshes it, so switching doesn't wait; a malformed `wsp_` ID or someone else's workspace renders the not-found page. Never copy the session or workspace into module-level variables.

| Hook | Returns | Use in |
| --- | --- | --- |
| `useSession()` | `Session`; throws when signed out | Signed-in UI (the name, `signedInAt`) |
| `useIsGuest()` | `true` when signed out | Guest UI under a `SessionProvider` |
| `useOptionalSession()` | `Session \| null` | Hooks that work everywhere |
| `useRenewSession()` | `(session) => void`: replaces the loaded session | After the API rotates the session (changing the password), so later requests send the new CSRF token |
| `useWorkspace()` | `Workspace` with `role`, `permissions` and `assignableRoles`; throws outside signed-in pages | Paths and links (`workspace.id`), the name, hiding what the API would refuse (`can(workspace, "project.store")`) |
| `useWorkspaceList()` | All the member's workspaces, a page at a time | The switcher ([pagination.md](pagination.md#infinite-lists)) |
| `useRefreshWorkspaces()` | `(id?) => void`: reloads the list, and `id`'s record | After creating or renaming a workspace |

`useApi()` and `useResource()` read the CSRF token and user ID from the session ([api.md](api.md)), so pages don't pass `session`, `csrfToken` or `userId` down.

## Returning after sign-in (`?next=`)

Signed-out visits to a signed-in page go to `/auth/login?next=<path and query>`, and signing in returns there. With two-factor authentication on, login asks for the code on the same page after the password (`{mfaRequired}`), keeping `next`; when the challenge ends (5 minutes, 5 wrong codes) it's back to the password, saying why. Login and register link to each other with the same `next`. Registering goes to onboarding (a new account has no workspace yet), unless `next` is an invitation link: then it returns there to accept. With email verification on, the code page sits between them and keeps `next` and the invitation state.

**Invitation links as `next`.** `returnRoutes` also accepts `/auth/invitations/<43-character token>`, the single-use exception AGENTS.md names. While `next` is one, login and register show `InvitationCallout` ("Acme has invited you to join.", loaded with `useInvitation`, so it survives a reload and switching between them) and fill in the invited email. The invitation page's buttons add `{invitation: true}` navigation state, which the switch links and `useAuthSubmit` pass on, so the invitation is accepted as soon as the visitor is signed in; a crafted `next` alone only reaches the page's Join button.

`returnPath(next)` in `lib/routes.ts` accepts only a path starting with `/` that stays on our origin and matches one of `returnRoutes` (`/app`, `/app/settings` and the workspace pages, with well-formed `wsp_`/`prj_` IDs). Anything else (other sites, `//host`, `/\host`, `/auth/*`, `/api/*`, `/app/onboarding`, numeric IDs) falls back to `/app`. It is only used as a page navigation, so it can't submit anything. **Add a pattern to `returnRoutes` for each new signed-in page.**

## Routes

- **Page URLs:** `routes` in `lib/routes.ts`, grouped by resource with the page actions `index`, `show`, `create` and `edit`. Workspace pages take the workspace ID: `routes.projects.show(workspace.id, project.id)` → `/app/wsp_…/projects/prj_…`, `routes.workspaces.edit(workspace.id)` → `/app/wsp_…/settings`. Pages that aren't a resource keep plain names (`routes.dashboard(ws)`, `routes.settings`).
- **Delete is never a page:** it's a confirmation dialog that calls the client's `destroy`.
- **API paths** live with their resource in `lib/api/*` and are exported as `paths`, e.g. `paths.projects(workspace.id).show(id)`.
- **Auth links:** `loginPath(path)` and `authPath(page, next)` build login/register URLs that carry an allowed `next`.
- **Dynamic IDs:** read `:workspace`/`:project` from `useParams()` with `parsePublicId("wsp" | "prj", value)` from `lib/ids.ts`. It returns `null` (render "not found") unless the value is that prefix and 14 base62 characters.
- **Links:** `Link`, `Button href`, `TextLink` and table rows render React Router links. `preventScrollReset` keeps the scroll position (sortable headers use it); `useNavigate()` navigates from code.
- **Sidebar:** `navigation(workspace.id)` in `routes.ts` lists the items. An item is current on its own path and on sub-paths (`/app/wsp_…/projects/prj_…` highlights Projects), compared against the location being navigated to.

## Public pages and prerendering

`pages/home.tsx` is listed in `react-router.config.ts` under `prerender`, so `react-router build` writes `build/client/index.html` with its content, `<title>` and meta description (from `root.tsx`'s `meta`, or the page's own `meta` export). It is built without a session, so it shows "Log in" and switches to "Dashboard" once the browser finds a session (`GET /api/auth/me`).

To add a public page: register it in `routes.ts`, add its path to `prerender`, and keep it free of user data. A prerendered route may have a `loader`, which runs at build time.

BaseUI styles itself with Styletron. `src/entry.server.tsx` runs only at build time: it renders each prerendered page (and the fallback) with a fresh Styletron server engine and writes the collected CSS into `<head>` as `<style class="_styletron_hydrate_">`. In the browser, `lib/styletron.ts` starts from those tags (`entry.client.tsx`).

## Signed-out pages

`pages/auth/*` export `clientLoader` → `guestLoader(request)`, wrap their content in `AuthLayout` and submit with `useAuthSubmit(api.auth().login)` (or `.register` with `routes.onboarding` as the destination, with `const api = useApi()`), which runs through `useAction` (busy, and loading after 200 ms, until the next page has loaded, `error` for `<ErrorNotice>`) and opens `?next=` or `/app` on success.

## Status pages

`StatusPage` in `components/layout/status-page.tsx` renders `NotFound` ("This page has wandered off.", linking to `/app`) and `ServiceUnavailable` (retry; with `requestId`, a 500's "Reference: req_…" under its description). The root `ErrorBoundary` shows `NotFound` for a 404 route error and `ServiceUnavailable` otherwise, with the error's reference when it has one, and reports the crash ([api.md](api.md#reporting-the-apps-own-errors)). `RouteProgress` shows a top bar when a navigation waits more than 200 ms on a loader (`useDelayedLoading`, so it also stays at least 400 ms).
