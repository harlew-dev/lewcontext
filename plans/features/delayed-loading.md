# Delayed loading states

Status: **implemented 2026-10-30**, as planned with the recommended answers to the open questions: the hook is `useDelayedLoading`, 200 ms delay and 400 ms minimum, errors show at once, and fields stay editable until `loading`. `workspace-settings.test.tsx` needed no change (its disabled check is before submitting). The change-password dialog's Back button also checks `busy`.

## Why

Loading states appear too quickly. A wrong-password sign-in takes about 180 ms, and in that time the button shows a spinner and the fields fade to 50% (disabled), then everything snaps back. It looks like a flicker.

`useAction` (`lib/use-action.ts`) sets `loading` the moment a request starts, and everything reads it: `<Button loading>`, `<Field disabled>`, the dialogs' "don't close while saving" guards. `RouteProgress` already avoids this for page loads with an inline 200 ms timer.

## Decision

`useAction` returns two flags (option A):

| Flag | When it's true | Use it for |
| --- | --- | --- |
| `busy` | At once, from the start of `run` until it settles | Logic: refusing to close a dialog mid-save, ignoring a second submit |
| `loading` | Only once `busy` has lasted 200 ms, then for at least 400 ms | What the user sees: `<Button loading>`, disabled fields |

```tsx
const save = useAction((input: ProjectInput) => api.projects(workspace.id).create(input));

<Dialog open={open} onClose={() => !save.busy && onClose()}>
  <Field disabled={save.loading}>…</Field>
  <Button type="submit" loading={save.loading}>Create project</Button>
</Dialog>
```

Double submission stays blocked by `useAction`'s `running` ref, which doesn't depend on either flag.

## Design

### The hook (`lib/use-delayed-loading.ts`)

```ts
// true once `busy` has lasted `delay` ms; once shown, stays at least `minDuration` ms
export function useDelayedLoading(busy: boolean, { delay = 200, minDuration = 400 } = {}): boolean
```

- Not busy for long: never shows (a 180 ms sign-in shows nothing).
- Busy for 220 ms: shows from 200 ms until 600 ms, so it doesn't flash for 20 ms instead.
- Unmounting clears its timers.
- `RouteProgress` uses it in place of its own timer (`useDelayedLoading(navigation.state !== "idle")`).

### `useAction`

- Today's `loading` state becomes `busy`; `loading = useDelayedLoading(busy)`.
- `useAuthSubmit` and `useCreateWorkspace` pass both through.

### Call sites that must use `busy`

| Site | Why |
| --- | --- |
| `project-dialog.tsx:84`, `workspace-form.tsx:106` (`onClose` guard) | Escape or a backdrop click in the first 200 ms of a save must not close the dialog |
| `project-dialog.tsx:149`, `workspace-form.tsx:122` (Cancel) | Cancel calls `onClose` directly, so it gets the same `busy` check (it still *looks* disabled from `loading`) |
| `pages/app/settings.tsx:145` (`if (loading) return`) | Logic, not display |

Everything else (`loading={…}`, `disabled={…}` on fields and buttons) stays on `loading`.

### Tests

- `use-action.test.tsx`: `busy` is true at once; `loading` isn't true until 200 ms, then stays at least 400 ms (fake timers); a quick action never sets `loading`.
- A test for the hook itself: never shown, shown after the delay, held for the minimum.
- `workspace-settings.test.tsx:40` expects the button disabled straight after submitting; it moves to `waitFor` or fake timers. `workspace-form.test.tsx:75` already waits.

### Docs

- `docs/ui/api.md` ("Run mutations through `useAction`"): `busy` vs `loading`, with the dialog example above.
- `docs/ui/pages.md:104`: `RouteProgress` uses the shared hook.
- `docs/ui/README.md:19`: list `use-delayed-loading` among the `lib/*` helpers.

## Out of scope

- SWR's `refreshing` (the refresh button spinner, `PaginatedTable` dimming): option B, not chosen for now. The same hook would apply later.
- The dialog's opening animation in dev (StrictMode): a separate issue.

## Open questions

1. **Name.** `useDelayedLoading`, or your `useLoading`?
2. **Timings.** 200 ms delay and 400 ms minimum?
3. **Error notice and the minimum.** If a request fails at 250 ms, the spinner is held until 600 ms, but the error notice could appear at 250 ms, while the spinner is still showing. Show the error at once (simpler), or hold it until the spinner goes?
4. **Fields during the first 200 ms.** They stay editable until `loading` turns on. Edits made then aren't in the request that was sent. Fine as is, or make the inputs read-only while `busy` (no visual change)?
