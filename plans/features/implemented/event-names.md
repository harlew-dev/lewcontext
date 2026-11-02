# Event names

Status: **implemented 2026-10-31**, decisions as recommended. Follows the [CRUD naming convention](crud-routing-convention.md) (implemented). Docs updated: [`docs/naming.md`](../../../docs/naming.md#events-internalevents), [`docs/api/events.md`](../../../docs/api/events.md), [`docs/api/auth.md`](../../../docs/api/auth.md), [`docs/api/testing.md`](../../../docs/api/testing.md); `database.md`, `mail.md`, `deployment.md` and `AGENTS.md` had no old names. Built as planned; the CHECK test is `TestEventNameCheck` in `internal/database`, the regex test `TestEventNamePattern`, and `naming.md` also notes that listener names don't follow event renames.

## Why

Event names are `<area>.<past_tense>` with snake_case after the one dot, so a sub-resource is glued to its fact: `workspace.member_updated`, `auth.password_reset_sent`, and soon `workspace.invitation_created`. Permissions already name the same resources with dots (`workspace.member.update`, `workspace.invitation.store`), so one resource has two spellings depending on where you look.

With a dot between the resource and the fact:

- **One spelling per resource.** `workspace.invitation` in the permission `workspace.invitation.store` and in the event `workspace.invitation.created`.
- **The last segment is always the fact**, and everything before it is what the fact is about. Reading or filtering by resource is a prefix (`workspace.invitation.%`), which a future webhook or audit page can use.
- **It scales** to more facts about the same resource (`workspace.invitation.sent`, `.accepted`, `.revoked`, `.expired`) without longer underscore chains.

## The rule

`<resource>.<fact>`:

- **The resource** is the thing the event is about, written as in its permission: a top-level resource is one segment (`project`, `workspace`), and a resource inside another adds one (`workspace.member`, `workspace.invitation`, `auth.password_reset`). Segments are snake_case.
- **The fact** is the last segment: past tense, snake_case, a fact rather than a command (`created`, `updated`, `removed`, `left`, `sent`).
- **The account and its session** are the `auth` area. Facts about them as a whole stay one segment under `auth`: `auth.registered`, `auth.logged_in`, `auth.login_attempted`, `auth.profile_updated`, `auth.password_changed`. A login attempt, a profile or a password check isn't a stored record of its own, so it gets no segment.
- **At most three segments.** Nothing is nested deeper than one sub-resource today. A deeper one would need this rule revisited, not a fourth dot.
- **CRUD actions:** `store` records `<resource>.created`; `update` records `<resource>.updated` or a more specific fact (`workspace.renamed`); `destroy` records `<resource>.deleted` or a more specific fact (`workspace.member.removed`, `workspace.member.left`, `workspace.invitation.revoked`).
- One event type per fact, with variable details in the payload, never in the name (`rate_limit.reached` with a `limit` field, not one event per limit). Unchanged.

## Renames

| Now | After | Struct (unchanged) | Recorded in |
| --- | --- | --- | --- |
| `auth.password_reset_requested` | `auth.password_reset.requested` | `PasswordResetRequested` | `httpapi/password_reset.go` |
| `auth.password_reset_sent` | `auth.password_reset.sent` | `PasswordResetSent` | `mail/password_reset.go` (listener) |
| `auth.password_reset_completed` | `auth.password_reset.completed` | `PasswordResetCompleted` | `store/password_resets.go` |
| `workspace.member_updated` | `workspace.member.updated` | `WorkspaceMemberUpdated` | `store/members.go` |
| `workspace.member_removed` | `workspace.member.removed` | `WorkspaceMemberRemoved` | `store/members.go` |
| `workspace.member_left` | `workspace.member.left` | `WorkspaceMemberLeft` | `store/members.go` |

Every other event keeps its name: `auth.registered`, `auth.login_attempted`, `auth.logged_in`, `auth.rate_limited`, `auth.logged_out`, `auth.other_sessions_revoked`, `auth.password_checked`, `auth.password_changed`, `auth.profile_updated`, `workspace.created`, `workspace.renamed`, `project.created`, `project.updated`.

**Listener names don't change.** `mail.password_reset` names a listener, not an event, and is stored with each delivery; renaming it would orphan queued deliveries.

## Changes

1. **Event types** (`internal/events/auth.go`, `internal/events/workspaces.go`): the six `EventName()` strings above, and any comment that quotes them.
2. **The `events.name` CHECK** (`migrations/007_create_events.tx.up.sql`, squashed, edited in place): `name ~ '^[a-z]+\.[a-z_]+$'` becomes `name ~ '^[a-z][a-z_]*(\.[a-z][a-z_]*){1,2}$'` (two or three snake_case segments, each starting with a letter). Rebuild local databases with `make migrate-fresh CONFIRM=1 && make seed`, including the e2e database if one is kept. Integration tests use fresh schemas and need nothing. Rows already in a local `events` table keep their old names; nothing reads them by name.
3. **The test regex** (`internal/events/events_test.go`, `eventName`): `^(auth|workspace|project)\.[a-z_]+$` becomes `^(auth|workspace|project)(\.[a-z][a-z_]*){1,2}$`, matching the CHECK with the known areas.
4. **Comments and tests** that quote the old names: `store/members.go`, `store/password_resets.go`, `httpapi/password_reset.go`, `mail/password_reset.go`, `httpapi/members_integration_test.go`, `httpapi/password_reset_integration_test.go`, and any other hit from `grep -rnE "password_reset_(requested|sent|completed)|member_(updated|removed|left)" apps/`.
5. **Docs** (below), in the same commit.
6. **Open plans** that name events in the old style (implemented plans are history and keep theirs):
   - `workspace-invitations.md`: `workspace.invitation_created`, `_sent`, `_revoked`, `_accepted` → `workspace.invitation.created`, `.sent`, `.revoked`, `.accepted`, in the flow, the store section, the events table and decision 1;
   - `client-ip.md`: `auth.password_reset_requested` → `auth.password_reset.requested`;
   - re-grep `plans/features/` for any open plan added since (`grep -rnE "\.[a-z]+_(created|updated|deleted|removed|left|sent|revoked|accepted|requested|completed)" plans/features/`), and check each hit against the rule. `auth.account_deleted` in `deletion.md` follows it already (a fact about the account as a whole, decision 2).

## Docs

- **`docs/naming.md`, "Events":** replace the section with the rule above (resource, fact, the `auth` area, three segments at most, the CRUD mapping, one type per fact), with examples `workspace.invitation.created`, `workspace.member.removed`, `auth.password_reset.requested`, `project.updated`, `auth.logged_in`. Note that the resource is spelled as in its permission (`workspace.member.update` → `workspace.member.updated`).
- **`docs/api/events.md`:**
  - the catalogue: the six renamed rows;
  - the `name` row of the columns table: `<resource>.<fact>`, e.g. `project.updated`, `workspace.member.removed`, with a link to naming.md;
  - "Adding an event", step 1: `EventName()` returns `<resource>.<fact>` (naming.md), not `<area>.<past_tense>`;
  - mention that the `name` CHECK allows two or three segments.
- **`docs/api/auth.md`:** the three password reset event names in "Password reset by link".
- **`docs/api/database.md`:** the `events` table row, if it describes the name format.
- **`docs/api/mail.md`**, **`docs/api/testing.md`** and **`docs/deployment.md`**: any old name (grep).
- **`AGENTS.md`**: any old name or the old `<area>.<past_tense>` wording (grep).
- **`plans/README.md`**: mark this plan implemented in the open plans table.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| A listener stops firing after the rename | Listeners are bound to the Go type (`Listen[E]`), not the name string, and the queued delivery is keyed by listener name, which doesn't change. The password reset integration and e2e tests send a real link through the listener. |
| A bad name gets into the audit trail | The `events.name` CHECK and the test regex both allow only two or three snake_case segments. |
| Payload changes | None: only names change. `TestPayloadsHoldNothingSensitive` and the payload tests run unchanged. |

## Tests

- `TestCatalogueNames` with the new regex: every catalogued name is well formed and unique.
- **New, `events_test.go`:** the regex accepts `workspace.member.removed` and `auth.logged_in`, and rejects `workspace.member_removed.x.y`, `Workspace.created`, `workspace..created`, `workspace.created.` and `workspace.1created`.
- **New, integration (`database` or `store`):** the migrated `events` table accepts a three-segment name and rejects a four-segment one (a factory event with each name).
- The existing event assertions (`onlyEvent(t, db, "workspace.member.removed")`, the password reset tests) use the new names and otherwise pass unchanged.
- Run `gofmt`, `go vet`, `make test`, `make test-integration`, `make check`, `make build` and `make test-e2e` against `make serve` (the forgot-password e2e test goes through the renamed events and the listener).

## Decisions (confirmed: as recommended)

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Dots between resource and fact | Yes: `<resource>.<fact>`, with the resource spelled as in its permission. |
| 2 | Facts about the account as a whole | Stay one segment under `auth` (`auth.logged_in`, `auth.profile_updated`, `auth.account_deleted`). Only stored sub-resources (password reset links, members, invitations) get a segment. |
| 3 | Depth | At most three segments, enforced by the CHECK and the test. |
| 4 | Listener names (`mail.password_reset`) | Unchanged. They name listeners, not events, and are stored with deliveries. |
| 5 | Old rows in local databases | Left as they are; the rebuilt CHECK accepts them, and nothing reads events by name. Production has no events yet. |
| 6 | When | One commit, before workspace invitations or any plan that adds events. |

## Not included

- Renaming event Go types (`PasswordResetSent` stays).
- Rewriting stored event rows.
- Subscribing listeners by name prefix, or webhooks. The prefix only makes them possible later.
- Renaming `auth.rate_limited`; the [rate limits plan](rate-limits.md) replaces it with `rate_limit.reached`.
