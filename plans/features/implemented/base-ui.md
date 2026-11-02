# Rebuild the UI on Base UI (MUI)

Status: **implemented 2026-11-02**, all steps at once (the reviewer asked for one pass, not step by step, with no visible change). Decision 12 was changed by the reviewer to **stacked toasts**. Docs updated: [base-ui.md](../../../docs/ui/base-ui.md), [design.md](../../../docs/ui/design.md), [README.md](../../../docs/ui/README.md), [pages.md](../../../docs/ui/pages.md), [strict-mode.md](../../../docs/ui/strict-mode.md), [testing.md](../../../docs/ui/testing.md), [auth.md](../../../docs/api/auth.md), AGENTS.md and README.md.

### Where the build differs from the plan

- **One pass.** Steps 1 to 6 were done together; the step 0 spike was folded in (the Vitest suites, `make build` and the e2e run were its checks). Not measured: gzip size of the entry chunks before and after, and the manual phone, dark-mode, reduced-motion and keyboard-only checks.
- **`entry.server.tsx` stays**, as a plain render of each prerendered page, without Styletron. React Router's default server entry needs `@react-router/node`, which nothing else uses, and a new dependency wasn't in the plan ([decision 9](#decisions)). `entry.client.tsx` is gone (the default hydrates).
- **Scroll lock** is Base UI's (`useScrollLock`, `usePopoverScrollLock` and their test are gone). **Focus return** is Base UI's for menus and selects, but a `Dialog` keeps `ReturnFocus` with `finalFocus={false}`, since a dialog opened from a menu item has no opener of its own ([decision 6](#decisions)).
- **`Combobox` chips** are our `Badge`s with their own remove button, not `Combobox.Chip`, which keeps the old removal and drops arrow-key movement into the chips. A pick keeps the list open by cancelling the close on `item-press`; Backspace in an empty search removes nothing (`preventBaseUIHandler`).
- **The trigger's accessible name** for `Listbox` is its `aria-label` (or its `Field`'s label); the selected text is the combobox's value, no longer part of the name ("Selected Planned soon. Filter by status").
- **Toasts** are `role="presentation"` roots (Base UI's default `dialog` would answer a search for the page's dialogs), swipe-to-dismiss is off, the stack shows five, and a `toast` call with no `Toaster` mounted shows nothing instead of throwing.
- **`Calendar`** is our own, with Base UI's `Date`-based props kept (`value onChange range minDate maxDate`).
- **Tests:** jsdom stubs for `PointerEvent` and `ResizeObserver` in `tests/setup.ts`, `pick` and `hover` helpers in `src/test`, and combobox, toast and Events tests that changed what they query or how they press (see [base-ui.md](../../../docs/ui/base-ui.md#known-issues-and-workarounds)). The e2e CSP test now asserts a linked stylesheet.

## Why

The UI's behaviour layer is Uber's Base Web (`baseui` 18.2.0). Its README says Uber is "limiting our engagement with this repository" and its OpenSSF "Maintained" score is 4/10. It also costs us a lot:

- **A CSS-in-JS runtime beside Tailwind.** Styletron (`styletron-react`, `styletron-engine-atomic`), a `BaseProvider` theme, and a build-time collector in `entry.server.tsx` so prerendered pages have their styles. Every `baseui` component is restyled by `slot()` (`lib/baseui.tsx`), which exists only to turn BaseUI's `$isHovered`-style props into the data attributes our Tailwind classes target.
- **130 packages** (30 direct) and three pinned compatibility overrides (`react-uid`, `react-window`, `react-virtualized-auto-sizer`).
- **Workarounds for its quirks** ([base-ui.md](../../../docs/ui/base-ui.md#known-issues-and-workarounds)): `returnFocus` losing the opener, `Select` aria names reading `[object Object]`, popovers fading their Body, dialogs not animating open under `make dev`, a popper.js modifier to open a `Listbox` over its button.

[Base UI](https://base-ui.com) (`@base-ui/react`, by MUI) is headless: it renders unstyled elements and exposes state as data attributes and CSS variables, which is how this app already styles everything. It's stable (1.0 in December 2025, 1.9.0 on 2026-10-09, monthly releases), MIT, has 5 direct dependencies and no provider, and its peer range includes React 18.3.1. It isn't the same library as `baseui`: the names are close but the API, packages and docs are different.

## How it fits

Base UI has no provider and no style engine, so it can sit **beside `baseui` while we migrate**. The migration is component by component, each step shippable, with `baseui` removed in the last. The one coupling is overlays: `baseui` portals into its `LayersManager` host and Base UI portals into `document.body`, so an overlay inside another (a `Listbox` in a `Dialog`) must come from the same library. All overlays therefore move together, in one step.

The public component APIs in [design.md](../../../docs/ui/design.md#component-apis-componentsui) don't change (`Button`, `Dialog`, `Dropdown`, `Listbox`, `Combobox`, `Tooltip`, `Checkbox`, `toast`…). Pages, `useAction`, `ConfirmationDialog`, the sliding `Highlight` (`motion`) and every Tailwind class that targets `data-*` stay. Only what's inside `components/ui` and `components/layout/sidebar-layout.tsx` changes.

### Styling model

| Today (`slot()`) | Base UI |
| --- | --- |
| `slot("div", "classes")` replaces an override slot, `$isOpen` → `data-open` | The part is ours: `<Dialog.Popup className="…">`; its state is already `data-open`, `data-closed`, `data-starting-style`, `data-ending-style`, `data-highlighted`, `data-selected`, `data-checked`, `data-disabled` |
| `data-closed` set by a 500 ms `Layer` unmount timer | Enter/exit are CSS transitions on `data-starting-style` / `data-ending-style`; the popup stays mounted until they end. Our existing durations and `motion-reduce:` stay |
| Inline `$width` for the listbox's `--button-width` | Base UI sets `--anchor-width`, `--available-height`, `--transform-origin`, `--collapsible-panel-height` itself |
| `popoverOverrides`, `popoverExit`, `usePopoverSettled` | Gone: no BaseUI Body to keep opaque, and positioning is settled before first paint |

`lib/baseui.tsx` is deleted. The few shared class strings (`popoverPanel`) move to a small `lib/transitions.ts`. `lib/interactive.ts` (`useInteractive`) stays and now covers plain buttons too.

## Component map

Import each part from its own path (`@base-ui/react/dialog`) so the app and admin builds only bundle what they use (the package is `sideEffects: false`).

| Our component | Now | Base UI | Notes |
| --- | --- | --- | --- |
| `Button` | `baseui/button` | `button` (the element) | `loading`, `disabledReason` wrapper and `href` stay ours; `data-hover`/`data-active` come from `useInteractive`, as `Link` already does. Drops the `isLoading` zero-width workaround |
| `Input`, `InputGroup`, `OtpInput` | `baseui/input` | `input` | `blurred`, `onReveal`, locked/guarded fields and the single invisible OTP input stay. **Not** Base UI's `OTPField`: it's six inputs, and we chose one input for paste, autofill and one label ([design.md](../../../docs/ui/design.md#component-apis-componentsui)) |
| `Textarea` | `baseui/textarea` | a styled `<textarea>` wired through our `Field` | Base UI has no Textarea part |
| `Fieldset`, `Field`, `Label`, `Description`, `ErrorMessage` | own | **unchanged** ([decision 4](#decisions)) | `useField()` is read by `Listbox`, `Combobox`, `Checkbox` |
| `Checkbox`, `CheckboxField` | `baseui/checkbox` | `checkbox` (`Root`, `Indicator`) | `data-checked`, `data-indeterminate`, `invalid` via our `Field`. Same box, same label markup |
| `Dialog`, `ConfirmationDialog` | `baseui/modal` | `dialog` (`Root`, `Portal`, `Backdrop`, `Viewport`, `Popup`, `Title`, `Description`) | `onClose` ← `onOpenChange(false)`; `ConfirmationDialog`'s "nothing closes it while confirming" is `details.cancel()` in `onOpenChange` plus `disablePointerDismissal`. `Title`/`Description` give `aria-labelledby`/`describedby`, replacing `DialogContext` ids. Same `sizes`, same gutter, same sheet-on-phones classes |
| Mobile sidebar | `baseui/drawer` | `dialog` styled as the same left slide-over ([decision 5](#decisions)) | Close button stays `SidebarToggle`, `SidebarCloseContext` unchanged |
| `Dropdown*` | `baseui/popover` | `menu` (`Root`, `Trigger`, `Portal`, `Positioner`, `Popup`, `Item`, `Separator`, `GroupLabel`) | `anchor="bottom end"` → `side`/`align`; `keepOpen` → `closeOnClick={false}`; `href` items render our `Link` (`render`); `danger`, `selected` stay ours. Items take focus on hover, and Menu marks the active one `data-highlighted`, which drives the sliding `Highlight` (`useHighlight().activate`) |
| `Listbox*` | `baseui/select` | `select` (`Root`, `Trigger`, `Value`, `Icon`, `Portal`, `Positioner`, `Popup`, `List`, `Item`, `ItemText`, `ItemIndicator`) | `alignItemWithTrigger` (the default) opens the popup over the button with the selected item on it, **replacing our popper.js `alignSelection`** (it turns itself off for touch). Deletes the `labelKey="text"` workaround: `Select.Value` renders the selected item's own label |
| `Combobox` | `baseui/select` (`multi`, `searchable`) | `combobox` (`Root multiple`, `Input`, `Chips`, `Chip`, `ChipRemove`, `Popup`, `List`, `Group`, `Item`) | The tree logic stays ours: group rows toggle their leaves, `compressPicked` shapes the chips, rows keep `aria-checked` `true`/`false`/`mixed`. Removes the "options carry `aria-selected` for the highlighted row" workaround |
| `Tooltip` | `baseui/tooltip` | `tooltip` (`Provider`, `Root`, `Trigger`, `Positioner`, `Popup`) | `content`, `placement`, `open`, `disabled`, `onClick` unchanged. `Tooltip.Provider` in `root.tsx` for shared delays |
| `Toaster`, `toast` | `baseui/toast` | `toast` (`Provider`, `Portal`, `Viewport`, `Root`, `Title`, `Close`) with `Toast.createToastManager()` | The imperative `toast.success(message, { duration })` is kept: the module-level manager is what `toast` calls, `duration` → `timeout` (`0` for `Infinity`). Pauses on hover/focus and keyboard-reachable close are built in, so `DismissButton` workaround goes. Stacked look, as in Base UI's demo: toasts overlap and scale behind the newest, and expand on hover/focus (`Toast.Root` `data-expanded`, `--toast-index`, `--toast-offset-y`; [decision 12](#decisions)) |
| `Calendar` | `baseui/datepicker` | **none** | See [Calendar](#calendar) |
| `SettingsSection collapsible` | not built | `collapsible` (`Root`, `Trigger`, `Panel`) | Not part of this migration: built afterwards in [collapsible-danger-zones.md](collapsible-danger-zones.md) |

### Calendar

Base UI has no calendar. `components/ui/calendar.tsx` is used only by the admin Events page's custom range and ships only in the admin build. Options:

1. **Our own** month grid on `Date` and `Intl` (roving-tabindex grid, arrows, Page Up/Down, Home/End, range highlight, `minDate`/`maxDate`, one month on phones and two from `sm:`), keeping today's props (`value onChange range minDate maxDate`). No dependency. About 250 lines and its own keyboard tests, in a file the page doesn't care about.
2. `react-day-picker`: a new dependency (needs its own plan section and audit) for a week of styling work.
3. Two native `<input type="date">`, the fallback already named in the Events plan ([decision 14](admin/events-page.md)): least code, least polish.

Recommended: option 1. Its day cells use the same classes as today, and the local-midnight pitfall stays documented because the page still gets local `Date`s.

### Danger zones

Collapsing the four danger zones behind a ghost **Show** / **Hide** button, on Base UI's `Collapsible`, is its own plan, [collapsible-danger-zones.md](collapsible-danger-zones.md). It needs this migration finished first, and nothing here depends on it.

## What goes away

| Removed | Because |
| --- | --- |
| `baseui`, `styletron-react`, `styletron-engine-atomic` | Replaced by `@base-ui/react` |
| `package.json` `overrides` for `react-uid`, `react-window`, `react-virtualized-auto-sizer` | They were BaseUI's dependencies. The `@types/react*` overrides stay (React 18 types) |
| `lib/baseui.tsx`, `lib/theme.ts`, `lib/styletron.ts` | No `slot()`, theme or style engine |
| `BaseProvider`, `StyletronProvider` | Base UI needs no provider (apart from `Tooltip.Provider` and `CSPProvider`) |
| `src/entry.server.tsx`, `src/entry.client.tsx` | Only existed to collect and rehydrate Styletron CSS. React Router's default entries prerender and hydrate (checked by `make build` and the e2e CSP test) |
| `useScrollLock`, `usePopoverScrollLock`, `ReturnFocus` / `focusedElement` | Replaced by Base UI's modal scroll lock and `finalFocus`, **only if** the spike shows they keep today's behaviour ([decision 6](#decisions)); otherwise they stay and wrap Base UI's parts |

## Security and CSP

- **No new origin, script or network call.** `style-src` keeps `'unsafe-inline'` ([auth.md](../../../docs/api/auth.md#transport-and-headers) names Styletron as the reason, which becomes the style *attributes* `motion`, Base UI's positioner and Recharts set through React). Script hashes are untouched: Base UI's one inline script is the `Tabs.Indicator` prehydration script and we don't use that part.
- **`CSPProvider disableStyleElements`** in `root.tsx`. A `Select` with `alignItemWithTrigger` injects a `<style>` to hide native scrollbars; with `disableStyleElements` it doesn't, and we add the two-line `.base-ui-disable-scrollbar` rule to `base.css`. This keeps working if `style-src` is ever tightened.
- **Supply chain:** drops about 130 packages for 5 direct and 3 transitive ones; `@base-ui/react` is MIT with SLSA provenance. `npm audit` before and after, lockfile preserved.
- **Behavioural risk is accessibility, not data:** no auth, session, API, token or route changes. The risks are focus return, focus trapping, Escape/backdrop handling while an action is pending (`ConfirmationDialog`), and nested overlays. They're covered by [Tests](#tests).
- **Admin build isolation** is unchanged: the `no-admin-modules` build check and `scripts/check-app-build.sh` still run, and `Calendar` and `Combobox` stay out of the app bundle.

## Steps

Each step ends green on `make check` and `make test`, and can be merged on its own.

0. **Spike (gate).** On a throwaway branch: Dialog, Select, Menu and Toast from Base UI on React 18.3.1 under `make dev` (StrictMode) and `make serve`, in Vitest/jsdom, and in a prerendered page. Answers: does Dialog animate open in dev (the known bug, a section of the old docs/ui/base-ui.md, gone with `baseui`); what jsdom needs (`ResizeObserver`, `matchMedia`, `getAnimations` stubs in `tests/setup.ts`); does Base UI's scroll lock hide the scrollbar and pad `<html>` as ours does; does `finalFocus` return to the opener after a menu item opens a dialog; does a module-level toast manager work with `Toaster` mounted as a sibling. If any answer is bad, stop and report.
1. **Foundations.** Add `@base-ui/react`, `CSPProvider`, `Tooltip.Provider`, `lib/transitions.ts`, the ESLint import rule ([decision 7](#decisions)). `baseui` stays installed.
2. **Leaf controls:** `Button`, `Input`/`InputGroup`/`OtpInput`, `Textarea`, `Checkbox`.
3. **Overlays, together:** `Dialog` (and so `ConfirmationDialog`), the mobile sidebar, `Dropdown`, `Listbox`, `Combobox`, `Tooltip`. The largest step; also removes `useScrollLock`/`ReturnFocus` if the spike allows.
4. **Toast.**
5. **Calendar** (own implementation, [decision 3](#decisions)), with the Events page's custom range tests.
6. **Remove `baseui`:** the providers, `theme.ts`, `styletron.ts`, `lib/baseui.tsx`, both entry files, the overrides, the three packages; fix the CSP comment; docs and AGENTS.md.

## Docs

The docs use "BaseUI" for Base Web; afterwards "Base UI" means MUI's, and `docs/ui/base-ui.md` keeps its file name.

- **`docs/ui/base-ui.md`:** rewritten: the package, setup (none beyond `CSPProvider`/`Tooltip.Provider`), what we use (the table above), how parts are styled (data attributes and variables, no `slot()`), overlays and layers, and a trimmed known-issues table (the Select, popover, focus, Calendar rows go; new ones found in the spike are added). The dev-only dialog section is removed if the spike confirms it's gone.
- **`docs/ui/design.md`:** "BaseUI underneath" (overlay animation, scroll lock, return focus, Listbox alignment, sliding-highlight sources), the `Dialog`, `Dropdown`, `Listbox`, `Combobox`, `Calendar`, `toast`, `Tooltip` rows.
- **`docs/ui/README.md`:** `lib/*` list, the entries row, Dependencies (`@base-ui/react`, removed overrides, `baseui` and Recharts notes, "React Router stays on v7" still true while React stays 18).
- **`docs/ui/pages.md`** (the Styletron paragraph), **`docs/ui/strict-mode.md`** (`StyletronProvider`), **`docs/ui/testing.md`** (e2e coverage line mentioning Styletron), **`docs/api/auth.md`** (CSP sentence), **`README.md`** (three lines), **`apps/api/internal/web/csp.go`** (comment).
- **`AGENTS.md`:** Product and stack, rule 6 (`slot()` and `$` props paragraph).
- **Implemented plans** that name BaseUI (`spa.md`, `admin/events-page.md`) stay as history.
- This plan moves to `plans/features/implemented/` with its `Status:` and any differences noted.

## Tests

- **Vitest:** the 11 `components/ui/*.test.tsx` suites keep their assertions on behaviour (keyboard, `aria-*`, values, `onChange`) and change only what they query (the `Listbox`, `Combobox`, `Calendar`, `Toast` and `ConfirmationDialog` ones will). New: `Dialog` (focus trap, Escape, backdrop, pending guard, focus returned), `Dropdown` (arrows, Home/End, `keepOpen`, `href`, focus returned after an item opens a dialog), a `Listbox` inside a `Dialog`, the Calendar's keyboard and range, and `Toast` (duration, `Infinity`, pause, dismiss).
- **Playwright** (`make test-e2e E2E_PORT=auto`): unchanged flows, plus the CSP check now asserting the prerendered page links a stylesheet instead of `_styletron_hydrate_` (`workspace.spec.ts`), no CSP violations with menus, selects, dialogs and the calendar open, and dialogs animating open in dev and production.
- **Size and audit:** `npm audit`, and gzip size of the app and admin entry chunks before and after (expected: smaller, since Styletron and `baseui` leave).
- **Manual:** phone and desktop widths, light and dark, reduced motion, keyboard-only through every dialog, menu and the Events filters ([forms-and-actions.md](../../../docs/ui/forms-and-actions.md#checking-your-change)).
- `make test`, `make check`, `make build`.

## Decisions

Each needs your confirmation before step 1.

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Migrate at all? | **Yes, but not urgent.** `baseui` works today; the case is that Uber has limited engagement, the 130-package tree, the CSS-in-JS layer and a dozen workarounds. If none of that bothers you, the alternative is to stay, and build the collapsible danger zones by hand (about 20 lines) instead of waiting for [collapsible-danger-zones.md](collapsible-danger-zones.md) |
| 2 | Strategy | **Incremental**, as in [Steps](#steps), overlays in one step. The alternative is one branch that swaps everything, which has the same end state and a harder review |
| 3 | Calendar | **Our own** month grid. Alternatives: `react-day-picker` (new dependency), or two native date inputs |
| 4 | Field wiring | **Keep our `Field`/`useField`** and put Base UI parts inside. Adopting Base UI's `Field.Root`/`Label`/`Description`/`Error` is possible but a second rewrite of every form with no visible gain |
| 5 | Mobile sidebar | **`Dialog` styled as the same slide-over.** Base UI's `Drawer` adds swipe-to-close; a behaviour change we can do later |
| 6 | Scroll lock and focus return | **Use Base UI's if the spike shows the same behaviour** (no layout shift, scroll position kept, focus back on the opener, including a dialog opened from a menu); otherwise keep our hooks around Base UI's parts |
| 7 | Guardrail | **Add an ESLint `no-restricted-imports`**: `@base-ui/react/*` only from `components/ui`, `components/layout`, `lib` and `root.tsx`, and `baseui` and `styletron-*` from nowhere. A new check, so it's yours to approve |
| 8 | React version | **Stay on 18.3.1.** Base UI supports 17, 18 and 19, so this migration doesn't force the React 19 and React Router 8 upgrade; that stays a separate decision |
| 9 | Entry files | **Delete `entry.client.tsx` and `entry.server.tsx`** and use React Router's defaults, if the build and the e2e CSP test pass |
| 10 | Collapsible danger zones | **A separate plan, built after this one:** [collapsible-danger-zones.md](collapsible-danger-zones.md), on Base UI's `Collapsible` |
| 11 | OTP | **Keep one invisible input** on Base UI `Input`, not `OTPField` |
| 12 | Toast look | **Stacked toasts from Base UI** (the reviewer's change, replacing the earlier recommendation of today's flat stack). Collapsed they stack and scale behind the newest; hover or focus expands them. Reduced motion drops the transitions |

## Not included

- Upgrading React or React Router, or moving to Base UI's `Drawer` swipe, `NumberField`, `Menubar`, `NavigationMenu` or `Tabs`. Tabs stay our own with `motion`'s sliding underline.
- Adopting Base UI's `Field`, `Form` or `Fieldset` ([decision 4](#decisions)).
- Any visual redesign: sizes, colours, spacing and copy stay.
- Any API, Go, auth, rate-limit, route or data change.
- Tightening `style-src`.
