# Shared helpers (reducing repeated code)

Status: **decided 2026-10-30; steps 1–3 of 4 done (web helpers, web test helpers, API helpers).** Build before [events](events.md) step 2.

## Why

A review of `apps/api` and `apps/web` after change password and events step 1. Most code is already factored well (the typed API client, `useResource`, `Paginate[T]`, factories, `slot()`, the `record` helper). The repetition is in a few patterns copied from file to file. Each one below lists where it repeats, the proposed helper, and what it removes. Ordered by value for risk.

Behaviour stays the same in every item; the existing unit, integration and end-to-end tests cover each one.

## Web (`apps/web`)

### 1. A mutation hook: `useAction` (high value)

**Repeats in 6 places:** `ProjectForm` (project-dialog.tsx), `RenameForm` (`[workspace]/settings.tsx`), `useCreateWorkspace` (workspace-form.tsx), `useAuthSubmit`, and in `pages/app/settings.tsx` `SessionControls` and `ChangePasswordDialog` (twice). Each has its own:

```ts
const [saving, setSaving] = useState(false);
const error = useActionError();
setSaving(true);
try { await …; error.clear(); … } catch (e) { error.fail(e); } finally { setSaving(false); }
```

**Proposal:** `lib/use-action.ts`. One hook owns the pending flag (`loading`), the double-submit guard and the error; the caller only says what the action does.

```ts
const save = useAction(async (input: ProjectInput) =>
  api.projects(workspace.id).update(project.id, input),
);

<form onSubmit={(event) => { event.preventDefault(); void save.run(form); }}>
  <ErrorNotice error={save.error} />
  …
  <Button type="submit" loading={save.loading}>Save changes</Button>
</form>

const result = await save.run(form); // { ok: true, value } | { ok: false }
if (result.ok) { onSaved(result.value); onClose(); }
```

What `run` does, in order:

1. **Guard:** if a run is already in flight, return `{ ok: false }` straight away. The flag is a ref, not state, so two clicks in the same tick can't both get through (state updates only land on the next render, which is how a double submit slips past `if (saving) return` today).
2. **Pending:** set `loading` (state; named like `<Button loading>`, which it feeds, and it disables the fields).
3. **Act:** await the action with the arguments given, including anything it awaits after the request, such as `navigate(...)` to the new workspace.
4. **Success:** clear the error and resolve `{ ok: true, value }` with the action's return value.
5. **Failure:** pass the error to `onError` if given; unless that returns `true` ("handled"), store it in `error` (the existing `useActionError()` value) for `<ErrorNotice>`. Resolve `{ ok: false }`; `run` never throws.
6. **Always:** clear `loading` and the in-flight flag.

How each current site maps onto it:

| Site | Today | With `useAction` |
| --- | --- | --- |
| `ProjectForm`, `RenameForm` | try/catch/finally + notice | `run`, then `onSaved` / toast on `ok` |
| `useCreateWorkspace`, `useAuthSubmit` | stay busy after success until the next page opens | The action awaits `navigate(...)`, so `loading` lasts until the new page has loaded. It clears after that, when the form is already gone (onboarding unmounts; the dialog is closing and ignores clicks). **No `keepBusy` option needed.** |
| `SessionControls` | errors as a toast | `onError: (e) => { toast.error(errorMessage(e)); return true; }` |
| `ChangePasswordDialog` | field errors under inputs, others in a notice | `onError` routes `ApiError.field` errors to the fields and returns `true`; anything else falls through to `error` and the notice |

- Removes about 8–12 lines per site, and every mutation behaves the same way under double clicks.
- Unit tests for the hook itself: a second `run` while one is pending does nothing; `loading` across the run; error stored, cleared on success, kept out by `onError` returning `true`; `run` resolves `{ ok: false }` instead of throwing.

### 2. One error notice: `<ErrorNotice error={…} />` (small, easy)

**Repeats in 7 places** (login, register, onboarding, account settings, workspace settings, project dialog, workspace dialog):

```tsx
{error.message && (
  <Notice key={error.key} variant="error" className="mb-6">{error.message}</Notice>
)}
```

**Proposal:** `ErrorNotice` in `components/ui/feedback.tsx` next to `Notice`. It takes the `useActionError()` value, renders nothing without a message, and keys itself so a repeated failure is announced again (the comment in `use-action-error.ts` currently asks every caller to remember that).

### 3. Date formatting in `lib/format.ts` (small, easy)

`formatDate(value, "short" | "long")` lives in `components/app/projects/project-display.tsx`, but `pages/app/settings.tsx` formats "Member since" and "Signed in" itself, with its own `toLocale…String("en-US", { …, timeZone: "UTC" })` calls. Move `formatDate` to `lib/format.ts`, add `formatDateTime` for the sign-in time, and use both everywhere. One place decides "UTC, en-US", as the existing comment intends.

### 4. Shared test helpers in `src/test/` (medium value)

**`fetch` is stubbed 9 ways in 9 test files**, and **a `Session` fixture is written out in 5.** Each re-implements "answer this request with that JSON/status".

**Proposal:** `src/test/api.ts`:

```ts
const api = stubApi({
  "POST /api/auth/password/verify": [{ status: 204 }],
  "PUT /api/auth/password": [{ status: 200, body: rotated }],
});
api.sent("PUT");   // [{ url, body, csrf }]
```

plus `testSession(overrides)`, and (added while building) `cleanup()` / `vi.unstubAllGlobals()` once in `tests/setup.ts` instead of an `afterEach` in 16 files. This is the router-style stub `settings.test.tsx` already has, generalised, with support for a held response (`gate`) that the pending-state tests use. Tests with special needs (the pagination outage) keep theirs.

### 5. A form-in-dialog component: not doing (Q2)

Three dialogs repeat `Dialog > form > title, description, body, actions`, but the password dialog's two steps need escape hatches. Items 1 and 2 remove most of the repetition inside them.

## API (`apps/api`)

### 6. The session cookie's digest (small, easy)

`r.Cookie(auth.CookieName(s.Config.Production))` followed by `auth.Digest(cookie.Value)` repeats in 5 places (`session`, `startSession`, `logout`, `revokeOthers`, `changePassword`). Proposal: `s.sessionDigest(r) string` in `middleware.go`.

### 7. Decode and validate a body (small)

`createWorkspace`, `updateWorkspace`, `createProject` and `updateProject` repeat:

```go
var in store.ProjectInput
if !decode(w, r, &in) { return }
if !validProject(&in) { fail(w, 422, "Check the project name, …"); return }
```

Proposal: `in, ok := decodeValid(w, r, validProject, "Check the project name, …")`, generic over the input type. Auth and password handlers keep their own: they validate in steps with different messages.

### 8. Store errors in handlers (Q3)

Handlers end most store calls with one of two shapes:

```go
// 15 times: any error is a 500.
if err != nil {
	internalError(w, err)
	return
}

// 2 times (GET and PATCH a project): no row is a 404, anything else a 500.
if errors.Is(err, sql.ErrNoRows) {
	fail(w, 404, "Project not found.")
	return
}
if err != nil {
	internalError(w, err)
	return
}
```

The store returns `sql.ErrNoRows` when a record doesn't exist **or isn't in the member's workspace**, so "not found" and "not yours" give the same answer (AGENTS: same 404 for both). Workspaces and sessions don't need this in handlers: `requireWorkspace` answers 404 and `requireSession` 401 before the handler runs.

Options:

- **(a) Leave it.** Explicit `if err != nil` blocks are the usual Go style, and there are only 2 not-found sites.
- **(b) `notFound(w, err, "Project") bool`** for the 404 shape only: `if notFound(w, err, "Project") { return }`, answering 404 or 500 and reporting whether it did. Saves about 6 lines at each of the 2 sites; worth it once tasks or comments add more.
- **(c) One `failed(w, err) bool`** for every store error, mapping `sql.ErrNoRows` to a 404 with a caller-supplied message and everything else to a 500. Covers all 15 sites (about 2 lines each), but makes every handler depend on a mapping that is easy to get wrong: a store call that can return `sql.ErrNoRows` for a different reason would silently become a 404.

Suggested: **(a) now, (b) when a third resource needs a 404.** Your call.

### 9. Go test helpers (medium value)

The Go tests have 72 `t.Fatal(err)` and 40 `if err != nil {` lines, and the integration tests have 5 one-off SQL lookups (`SELECT email FROM users WHERE id=?` twice, user/workspace ID by public ID, workspace name).

**Proposal:**

- `must`, a generic helper that fails the test or returns the value: `user := must(factory.Users.CreateOne(ctx, db, f))(t)`. (Go only lets a two-result call fill a function's whole argument list, so `must` takes the value and error and returns a function of `t`; `must(t, f())` wouldn't compile.)
- Lookups next to `signedIn` in `integration_test.go`: `emailOf`, `idOf("users" | "workspaces", publicID)`.
- Optionally a test harness for the setup every integration test repeats (Q4, below).

**Q4, the harness.** All 19 integration tests start with the same four lines, and pass `t` and the handler to every request (98 `call(t, h, …)` sites):

```go
s, db := integrationServer(t)
h := s.Routes()
ctx := context.Background()
f := factory.TestFaker(t)
session := signedIn(t, db, f)
call(t, h, "PUT", path, body, session.CSRFToken, session.Cookie(), 200)
```

A harness bundles them into one value:

```go
x := newHarness(t) // server, routes, db, ctx, faker, t
session := x.signedIn()
x.call("PUT", path, body, session.CSRFToken, session.Cookie(), 200)
```

- **Gain:** 3 lines per test (about 60) and shorter calls.
- **Cost:** every integration test changes (19 tests, 98 call sites), a large but mechanical diff, and two styles side by side unless all are moved at once. Go methods can't be generic, so `must` still takes `t` separately: `must(...)(x.t)`.
- Suggested: **not now.** `must` and the lookups remove most of the noise (the `if err != nil { t.Fatal(err) }` blocks) without touching every call. Revisit if the test files keep growing.

## Not worth centralising

- **Store transactions that record an event.** Each runs `RunInTx` + change + `record`, but the changes differ, and `record` is already the shared part. A wrapper would hide the transaction boundary that makes the outbox work.
- **`requireSession` / `requirePageSession`.** They look alike but answer differently (401 JSON vs a redirect before any HTML), on purpose.
- **Validation functions** (`validWorkspace`, `validProject`, `credentials`). They share "trim, count characters", but a `textBetween(s, min, max)` helper would save little and each mirrors its own column's CHECK.

## Order

Before events step 2, so the listener and worker code uses the new helpers.

1. Web: `useAction` + `ErrorNotice` + `lib/format.ts`, and the six sites moved onto them (`ui:`).
2. Web tests: `src/test/api.ts` and `testSession` (`test:`).
3. API: `sessionDigest` + `decodeValid` (`refactor:`).
4. Go tests: `must` and the lookups (`test:`).

Each step runs `make test`, `make check` and the integration tests; step 1 also the end-to-end tests.

## Decisions

| # | Question | Decision |
| --- | --- | --- |
| 1 | `useAction`, and a `keepBusy` option for workspace creation? | Build `useAction`, with the pending flag named `loading` (it feeds `<Button loading>`). No `keepBusy`: `run` awaits the whole action, navigation included (item 1). |
| 2 | Build a `FormDialog`? | No. |
| 3 | Store errors in handlers (item 8) | (a) Leave as is for now; (b) `notFound` when a third resource needs a 404. |
| 4 | Go test helpers | `must` + lookups; no harness for now (item 9). |
| 5 | Before or after events step 2? | Before. |
