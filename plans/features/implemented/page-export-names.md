# Page export names

Status: **implemented 2026-11-03**. Built as planned. Test import bindings needed no changes (the descriptive local aliases still work, as expected), and `docs/api/monitoring.md`'s page list was also updated because it named the old exports. Docs updated: [naming.md](../../../docs/naming.md#web-app), [checks.md](../../../docs/checks.md#whats-checked), [ui/pages.md](../../../docs/ui/pages.md#urls), [ui/README.md](../../../docs/ui/README.md), [ui/testing.md](../../../docs/ui/testing.md), [api/admin.md](../../../docs/api/admin.md), [api/monitoring.md](../../../docs/api/monitoring.md#web-app), and AGENTS.md rule 4.

## Why

Every page currently invents its own default export name: `PrivacyPolicy`, `AccountSettings`, `AdminMonitoringOverview`, `WorkspaceEdit`, `ResetPassword`. The names come from two different ideas at once — the resource and the action — and plain pages use a third idea, "what the page is". That makes them inconsistent, and the current check only covers pages in a resource group of `lib/routes.ts`, so `pages/policy/privacy-policy/index.tsx` (a plain page) is not checked at all.

The file already says what the page is: `index.tsx` is the page for its folder's URL, `[<param>].tsx` is the page for a record, and `layout.tsx` is the route layout. The default export should say the same thing, so the function name in React DevTools, stack traces and test imports always matches the file you opened. This is a naming cleanup only: no URL, rendering, loader, API or build behaviour changes.

## The rule

**A page's default export is named from its file:**

| File            | Default export |
| --------------- | -------------- |
| `index.tsx`     | `Index`        |
| `[<param>].tsx` | `Show`         |
| `layout.tsx`    | `Layout`       |

- This applies to **every page and layout route** under `src/pages`, including the public pages, auth pages, signed-in app pages and the admin build's pages. File names and URLs don't change.
- `Edit` (and any future action) is used only when a page file is actually named `edit.tsx`. No page is today; workspace settings (`settings/index.tsx`) and account settings (`settings/index.tsx`) export `Index`.
- A record page named after its route param exports `Show` even when its page is an action rather than a read (`[token].tsx` for resetting a password or completing an email block).
- Only the **default** export is renamed. Named exports (`loader`, `clientLoader`, `meta`, `ErrorBoundary`) and local helper components keep their names.
- Test imports may keep descriptive local binding names (`import ProjectShow from "..."`), because the binding is local to the test. This is not part of the checked rule.

## Changes

### Rename every page's default export

The 60 route files as of 2026-11-03. Recheck `src/pages` and `src/routes.ts` before starting, because admin work may have added pages.

#### Admin (29)

| File                                                  | Now                     | Becomes |
| ----------------------------------------------------- | ----------------------- | ------- |
| `pages/admin/[key]/admins/index.tsx`                  | `AdminAdminIndex`       | `Index` |
| `pages/admin/[key]/alerts/[alert].tsx`                | `AdminAlertShow`        | `Show`  |
| `pages/admin/[key]/alerts/index.tsx`                  | `AdminAlertIndex`       | `Index` |
| `pages/admin/[key]/email-blocks/index.tsx`            | `AdminEmailBlockIndex`  | `Index` |
| `pages/admin/[key]/events/[event].tsx`                | `AdminEventShow`        | `Show`  |
| `pages/admin/[key]/events/index.tsx`                  | `AdminEventIndex`       | `Index` |
| `pages/admin/[key]/index.tsx`                         | `AdminLogin`            | `Index` |
| `pages/admin/[key]/ip-blocks/index.tsx`               | `AdminIPBlockIndex`     | `Index` |
| `pages/admin/[key]/mail/index.tsx`                    | `AdminDeliveryIndex`    | `Index` |
| `pages/admin/[key]/monitoring/accounts/index.tsx`     | `AdminMonitoringAccounts` | `Index` |
| `pages/admin/[key]/monitoring/configuration/index.tsx` | `AdminMonitoringConfiguration` | `Index` |
| `pages/admin/[key]/monitoring/data/index.tsx`         | `AdminMonitoringData`   | `Index` |
| `pages/admin/[key]/monitoring/errors/[error].tsx`     | `AdminMonitoringError`  | `Show`  |
| `pages/admin/[key]/monitoring/errors/index.tsx`       | `AdminMonitoringErrors` | `Index` |
| `pages/admin/[key]/monitoring/index.tsx`              | `AdminMonitoringOverview` | `Index` |
| `pages/admin/[key]/monitoring/integrity/index.tsx`    | `AdminMonitoringIntegrity` | `Index` |
| `pages/admin/[key]/monitoring/ips/[ip].tsx`           | `AdminMonitoringAddress` | `Show`  |
| `pages/admin/[key]/monitoring/ips/index.tsx`          | `AdminMonitoringIPs`    | `Index` |
| `pages/admin/[key]/monitoring/limits/index.tsx`       | `AdminMonitoringLimits` | `Index` |
| `pages/admin/[key]/monitoring/performance/index.tsx`  | `AdminMonitoringPerformance` | `Index` |
| `pages/admin/[key]/monitoring/security/index.tsx`     | `AdminMonitoringSecurity` | `Index` |
| `pages/admin/[key]/monitoring/usage/index.tsx`        | `AdminMonitoringUsage`  | `Index` |
| `pages/admin/[key]/monitoring/workspaces/index.tsx`   | `AdminMonitoringWorkspaces` | `Index` |
| `pages/admin/[key]/users/[user].tsx`                  | `AdminUserShow`         | `Show`  |
| `pages/admin/[key]/users/index.tsx`                   | `AdminUserIndex`        | `Index` |
| `pages/admin/[key]/workspaces/[workspace].tsx`        | `AdminWorkspaceShow`    | `Show`  |
| `pages/admin/[key]/workspaces/index.tsx`              | `AdminWorkspaceIndex`   | `Index` |
| `pages/admin/layout.tsx`                              | `AdminLayoutRoute`      | `Layout` |
| `pages/admin/not-found/index.tsx`                     | `AdminNotFound`         | `Index` |

#### App (19)

| File                                                     | Now                       | Becomes |
| -------------------------------------------------------- | ------------------------- | ------- |
| `pages/app/[workspace]/dashboard/index.tsx`              | `Dashboard`               | `Index` |
| `pages/app/[workspace]/projects/[project].tsx`           | `ProjectShow`             | `Show`  |
| `pages/app/[workspace]/projects/index.tsx`               | `ProjectIndex`            | `Index` |
| `pages/app/[workspace]/settings/api-keys/[key].tsx`      | `WorkspaceApiKeyShow`     | `Show`  |
| `pages/app/[workspace]/settings/api-keys/index.tsx`      | `WorkspaceApiKeyIndex`    | `Index` |
| `pages/app/[workspace]/settings/events/[event].tsx`      | `EventShow`               | `Show`  |
| `pages/app/[workspace]/settings/events/index.tsx`        | `EventIndex`              | `Index` |
| `pages/app/[workspace]/settings/index.tsx`               | `WorkspaceEdit`           | `Index` |
| `pages/app/[workspace]/settings/layout.tsx`              | `WorkspaceSettingsLayout` | `Layout` |
| `pages/app/[workspace]/settings/members/[member].tsx`    | `MemberShow`              | `Show`  |
| `pages/app/[workspace]/settings/members/index.tsx`       | `MemberIndex`             | `Index` |
| `pages/app/index.tsx`                                    | `WorkspaceHome`           | `Index` |
| `pages/app/layout.tsx`                                   | `AppLayout`               | `Layout` |
| `pages/app/onboarding/index.tsx`                         | `Onboarding`              | `Index` |
| `pages/app/settings/api-keys/[key].tsx`                  | `UserApiKeyShow`          | `Show`  |
| `pages/app/settings/api-keys/index.tsx`                  | `UserApiKeyIndex`         | `Index` |
| `pages/app/settings/index.tsx`                           | `AccountSettings`         | `Index` |
| `pages/app/settings/layout.tsx`                          | `AccountSettingsLayout`   | `Layout` |
| `pages/app/verify-email/index.tsx`                       | `VerifyEmail`             | `Index` |

#### Auth (7)

| File                                          | Now             | Becomes |
| --------------------------------------------- | --------------- | ------- |
| `pages/auth/email-blocks/[token].tsx`         | `EmailBlockShow` | `Show`  |
| `pages/auth/forgot-password/[token].tsx`      | `ResetPassword` | `Show`  |
| `pages/auth/forgot-password/index.tsx`        | `ForgotPassword` | `Index` |
| `pages/auth/invitations/[token].tsx`          | `InvitationShow` | `Show`  |
| `pages/auth/login/index.tsx`                  | `Login`         | `Index` |
| `pages/auth/register/index.tsx`               | `Register`      | `Index` |
| `pages/auth/register/verify/index.tsx`        | `RegisterVerify` | `Index` |

#### Public (5)

| File                                        | Now              | Becomes |
| ------------------------------------------- | ---------------- | ------- |
| `pages/docs/index.tsx`                      | `Documentation`  | `Index` |
| `pages/home/index.tsx`                      | `Home`           | `Index` |
| `pages/not-found/index.tsx`                 | `NotFoundPage`   | `Index` |
| `pages/policy/privacy-policy/index.tsx`     | `PrivacyPolicy`  | `Index` |
| `pages/policy/terms-of-service/index.tsx`   | `TermsOfService` | `Index` |

### Update test imports

The default import binding is local, so tests keep descriptive aliases where they import more than one page (`import ProjectIndex from "..."`, `import ProjectShow from "..."`). Only the module's exported function name changes. Files:

- `apps/web/src/components/app/workspaces/workspace-settings.test.tsx`
- `apps/web/src/components/app/api-keys/key-page.test.tsx`
- `apps/web/src/components/app/api-keys/key-dialog.test.tsx`
- `apps/web/src/components/app/api-keys/key-list.test.tsx`
- `apps/web/src/pages/app/settings/index.test.tsx`
- `apps/web/src/pages/app/[workspace]/projects/index.test.tsx`
- `apps/web/src/pages/app/[workspace]/settings/members/[member].test.tsx`
- `apps/web/src/pages/app/[workspace]/settings/members/index.test.tsx`
- `apps/web/src/pages/app/[workspace]/settings/events/index.test.tsx`
- `apps/web/src/pages/app/[workspace]/settings/events/[event].test.tsx`
- `apps/web/src/pages/admin/*.test.tsx` (`admin`, `actions`, `admins`, `alerts`, `events`, `email-blocks`, `monitoring*`)

An import alias need not match the exported name, so these edits are only needed where a binding reads wrong or collides; TypeScript will not break on the rename itself. The plan is to leave descriptive aliases in place and let the new check enforce the module's own default name.

### Rewrite `apps/web/src/pages/naming.test.ts`

The current test maps `lib/routes.ts` resource groups to their page and expects `<Resource><Action>`. That misses plain pages and the admin build. Replace it with a filesystem walk, like `layout.test.ts`:

- A pure `pageExportName(file)` returning `Index`, `Show` or `Layout` from the basename.
- A pure `badPageExport(file, exported)` returning the message, naming `docs/naming.md#web-app` and the fix.
- A table test: `index.tsx` + `Index`, `[project].tsx` + `Show`, `layout.tsx` + `Layout` pass; `index.tsx` + `Home`, `[project].tsx` + `ProjectShow`, `layout.tsx` + `AppLayout` fail.
- A run over `import.meta.glob(["./**/*.tsx", "!./**/*.test.tsx"])`: import every route file, require a function default export, compare `module.default.name`, and fail with the file and fix. Include `pages/admin/**` (the current glob excludes it).
- Assert a minimum count (60 today) so a moved or emptied tree can't pass quietly.
- Remove the now-unused `routes`, `registry`, `resourceName`, `capitalize`, `flatten`, `matches` and sample-ID machinery.

`pages/layout.test.ts`, `lib/routes.test.ts` and `lib/api/naming.test.ts` keep their current rules.

## Docs and enforcement for future pages

The docs are part of the change, not an afterthought: a new page must be born following the rule, not fixed later. Update these in the same commit as the code:

- **`docs/naming.md#web-app`:** replace both default-export rows (`<Resource><Action>` and "what the page is") with the file-derived rule and its three cases. Update the page ↔ API example table's Page column (`Index`, `Show`) and the sentence below it. The section already says "New code follows this page", so the new pages inherit the rule.
- **`docs/checks.md`:** the "Page export names" row (now file-derived, every page and layout, including admin) and the "Reading a failure" row for a page export, with the new example message. The "Adding a check" rules stay as they are.
- **`docs/ui/pages.md`:** line 9 (page default exports); the new-page snippet under Signed-in pages (`export default function TaskIndex()` becomes `export default function Index()`); and every parenthetical export name in the URL table (`(TermsOfService, PrivacyPolicy)`, `(RegisterVerify)`, `(VerifyEmail)`, `(EmailBlockShow)`, `(MemberShow …)`, `(EventIndex …)`, `(AdminLogin)`, `(AdminUserIndex …)`, `(AdminEventIndex …)`, `(AdminDeliveryIndex …)`, `(AdminMonitoring…)`, `(AdminAdminIndex)`, and the rest).
- **`docs/ui/README.md`:** the "Where code goes" and "Rules of thumb" sections get one line: a page's default export is `Index`, `Show` or `Layout` from its file, enforced by `pages/naming.test.ts` — so the guidance a new page is written from is correct.
- **`docs/ui/testing.md`:** the naming-checks paragraph, which currently says `pages/naming.test.ts` checks `<Resource><Action>` through `src/routes.ts`.
- **`docs/api/admin.md`:** the routes list's export names (every page there is `Index` or `Show`).
- **`AGENTS.md` rule 4:** replace "resource pages export `<Resource><Action>` (`ProjectIndex`)" with the file-derived rule, including that it applies to every page and layout and is checked by `pages/naming.test.ts`.
- Implemented plans under `plans/features/implemented/` stay as history and are not edited.

**Why this keeps future pages honest:** the rewritten check walks the filesystem (`src/pages/**/*.tsx`), not a list of resource groups, so a newly added `index.tsx`, `[param].tsx` or `layout.tsx` is checked automatically with no exception list to remember. The docs above are where that page is created from, so updating them (especially the copyable snippet in `pages.md` and rule 4 in `AGENTS.md`) is as important as the check itself.

## Security risks and how they're avoided

None new. No URL, route registration, loader, header, API call, build output or data flow changes. Only the JS function name of the default export changes. The admin build separation is unaffected: the new test imports admin modules at test time only, which existing admin tests already do, and `scripts/check-app-build.sh` still keeps them out of the app build.

## Tests

- The rewritten `apps/web/src/pages/naming.test.ts`: check that it fails on the current tree before renaming anything, then passes after.
- The existing Vitest suites, unchanged apart from import aliases.
- `make check` (TypeScript, ESLint, Prettier, doc links), `make test` (Vitest and Go) and `make build` (both the app and admin builds).
- `make test-e2e E2E_PORT=auto` is optional: no user flow changes, but it exercises both builds and the Go page gate.

## Decisions

| #   | Question                                                                                             | Decision / recommendation                                                                                                                                                                                                                    |
| --- | ---------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Is the export named from the file or from the route action?                                          | **Confirmed: the file name only.** `index.tsx` → `Index`, `[<param>].tsx` → `Show`, `layout.tsx` → `Layout`.                                                                                                                                |
| 2   | Are `layout.tsx` routes included?                                                                    | **Confirmed: yes.** All four layouts export `Layout`.                                                                                                                                                                                        |
| 3   | What about settings pages, whose route action is `edit`?                                             | **Recommendation:** they stay `Index`, because their file is `index.tsx`. `Edit` is reserved for a future `edit.tsx`; this is the cost of the file-name rule.                                                                                 |
| 4   | What about `[token].tsx` pages that do something rather than show a record (`ResetPassword`, `EmailBlockShow`)? | **Recommendation:** they export `Show`, because the file name is the rule. The page folder and URL carry the meaning.                                                                                                              |
| 5   | Do plain and admin pages join the same rule?                                                         | **Recommendation:** yes. This closes the current gap where `privacy-policy/index.tsx` and every admin page are unchecked.                                                                                                                   |
| 6   | How is the rule checked?                                                                             | **Recommendation:** a filesystem walk that imports every `index.tsx`, `[<param>].tsx` and `layout.tsx` under `src/pages`, including `admin`, rather than only the resource groups in `lib/routes.ts`.                                       |
| 7   | Do test imports have to use the new names?                                                           | **Recommendation:** no. Local import bindings stay descriptive (`ProjectIndex`, `ProjectShow`) where a test imports several pages; only the default export's function name is checked.                                                      |

## Not included

- Renaming page files, folders, URLs, route constants, API paths, Go controllers or store methods.
- Renaming named exports (`loader`, `clientLoader`, `meta`, `ErrorBoundary`) or local helper components.
- Changing the examples, route constants or plain-page list in `lib/routes.ts`; only the export names change.
- Editing implemented plans, which are history.
