# Base UI

[Base UI](https://base-ui.com) (`@base-ui/react`, by MUI) is the headless React library under our interactive controls. It renders unstyled elements and exposes their state as data attributes and CSS variables; we use it for **behaviour** (focus management, keyboard handling, overlays and positioning, ARIA) and draw everything you see with Tailwind classes in `components/ui`. How those components are styled is in [design.md](design.md#base-ui-underneath).

It isn't `baseui` (Uber's Base Web), which this app used before: the names are close, the API is not. Pinned at `@base-ui/react` 1.9.0, which supports React 17, 18 and 19; we stay on React 18.3.1 ([README.md](README.md#dependencies)).

## Setup

| Where | What |
| --- | --- |
| `root.tsx` | `<CSPProvider disableStyleElements>` and `<Tooltip.Provider delay={200}>` around every page. There is no theme or style provider. `disableStyleElements` keeps Base UI from injecting a `<style>` (a `Select` that aligns its popup with the trigger would, to hide a native scrollbar); the rule is `.base-ui-disable-scrollbar` in `styles/base.css`. |
| `components/ui/toast.tsx` | `Toast.Provider` lives in `Toaster`, not `root.tsx`: only the signed-in pages show toasts. |
| `lib/transitions.ts` | `popoverPanel` (the fade of a menu, listbox or tooltip) and `popoverExit`. |
| `entry.server.tsx` | A plain render of each prerendered page. React Router's default needs `@react-router/node`, which nothing else here uses. There is no client entry: React Router's default hydrates. |

Import each part from its own path (`@base-ui/react/dialog`), so the app and admin builds bundle only what they use. ESLint allows `@base-ui/react/*` only in `components/ui`, `components/layout`, `lib` and `root.tsx`, and `baseui` and `styletron-*` nowhere (`eslint.config.mjs`): pages use our components.

## What we use

| Base UI part | Our component | What Base UI provides |
| --- | --- | --- |
| `button` | `Button` (`button.tsx`) | The button element and its disabled handling; `useInteractive` adds `data-hover`, `data-active` and `data-focus` |
| `input` | `Input`, `OtpInput`, `InputGroup` | The input; the password eye, blur overlay, lock and the `Field` wiring are ours. **Not** `OTPField`: it is six inputs, and we chose one input for paste, autofill and one label |
| (a styled `<textarea>`) | `Textarea` | Base UI has no Textarea part; `Field` wires id, description and disabled state |
| `checkbox` | `Checkbox`, `CheckboxField` | The checkbox role, its keyboard and the hidden input that carries `name`, `required` and the form value, inside the row's `<label>` |
| `select` | `Listbox` (`listbox.tsx`) | Combobox keyboard handling, option highlighting, typeahead, positioning. The popup opens over the button with the selected option on it (`alignItemWithTrigger`, off for touch) |
| `combobox` (`multiple`) | `Combobox` (`combobox.tsx`) | Typing to search, ↑ ↓ and Enter, the popup kept open while picking. The tree logic (groups toggling their leaves, chips, `aria-checked`) is ours |
| `menu` | `Dropdown` | Click trigger, positioning, arrow, Home and End keys, Escape and outside-click dismissal, menu ARIA, scroll lock |
| `tooltip` | `Tooltip` | Hover and keyboard-focus trigger with delays, positioning, Escape |
| `collapsible` | `SettingsSection variant="danger"` | Trigger `aria-expanded`/`aria-controls`, `data-panel-open`, the panel's `--collapsible-panel-height` and start/end styles for the height animation; `keepMounted` keeps a closed panel in the DOM, `hidden` |
| `dialog` | `Dialog`, `ConfirmationDialog`, the mobile sidebar | Focus trap, Escape, backdrop click, scroll lock, `aria-labelledby` and `aria-describedby` from its title and description |
| `toast` | `toast`, `Toaster` | The timer (paused while the stack is hovered or focused), stacking with `--toast-index` and friends, a module-level manager |
| none | `Calendar` (`calendar.tsx`) | Base UI has no calendar: ours is a month grid on `Date` and `Intl` |

Don't add a second component library beside it. A new interactive control starts from the Base UI part that has its behaviour.

## How parts are styled

Each part renders one element, and is ours to style with `className`. Its state is already in data attributes, which our Tailwind classes target:

| Attribute | On |
| --- | --- |
| `data-starting-style`, `data-ending-style` | An overlay entering and leaving; it stays mounted until its transitions end |
| `data-highlighted` | The menu item, option or row the pointer or arrows are on |
| `data-selected`, `data-checked`, `data-disabled`, `data-popup-open` | The option, checkbox or trigger in that state |

Our own `data-invalid` and `data-blurred` mark an invalid or blurred input. Plain links and buttons get `data-hover`, `data-active` and `data-focus` from `useInteractive`.

Popups position themselves with CSS variables: `--anchor-width` (a listbox's minimum width), `--available-height` (a menu's maximum height) and `--transform-origin`. A part's `render` prop swaps its element: `Menu.LinkItem render={<Link />}`, `Select.Item render={(props, state) => …}` (where the highlight and the check mark are drawn from `state.highlighted` and `state.selected`).

### Overlays and layers

Overlays portal into `document.body`, so one opened from another (a `Listbox` in a `Dialog`) sits above it by z-index: dialogs and their backdrops `z-50`, menu, listbox and combobox positioners `z-[60]`, tooltips `z-[70]`, toasts `z-[80]`.

While a modal overlay is open (dialog, menu, select) Base UI hides the rest of the page from assistive technology and locks scrolling, hiding the page scrollbar without shifting the layout.

Focus returns to the element that opened a menu or select. For a `Dialog` it is ours (`lib/return-focus.tsx`, `finalFocus={false}`): a dialog opened from a menu item has no opener of its own, so `Dialog` remembers the element focused as it opened and refocuses it when the dialog is removed. A `DropdownItem` closes its menu first and runs its `onClick` once focus is back on the menu's button.

## Known issues and workarounds

| Issue | Workaround (where) |
| --- | --- |
| jsdom has no `PointerEvent` (a checkbox forwards its click to its hidden input as one) or `ResizeObserver` | Stubs in `tests/setup.ts` |
| A listbox or combobox option commits a click only if it began with a press on it (a click on an item that slid under a still pointer is ignored) | Tests use `pick(option)` (`src/test/pick.ts`: a `pointerdown`, then the click) |
| Tooltips open on pointer events that carry a pointer type, not on a bare `mouseenter` | Tests use `hover(element)` (`src/test/hover.ts`) |
| Every toast is a `role="dialog"`, which would answer a search for the page's real dialogs | `Toast.Root role="presentation"`; the message inside has its own `status` or `alert` role (`toast.tsx`) |
| While a combobox's list is open Base UI hides the rest of the page from assistive technology, so a button outside it isn't found by role in a test | Tests pass `hidden: true` for that click (`events.test.tsx`) |
| A single toast has `Toast.Provider`'s default limit of 3 | `Toaster` sets `limit={5}` |
| The `Combobox` selection is the chips (a group chip stands for its leaves), so Base UI's `aria-selected` can't say which leaves are picked | Each row also gets `aria-checked` (`true`, `false` or `mixed`) and says it in text for screen readers |
| The `Calendar` hands back **local `Date`s at local midnight**: `toISOString()` on one moves the day for anyone east of UTC | Read the year, month and day (`getFullYear`, `getMonth`, `getDate`) and build the UTC day from them (`utcDay` in `components/app/admin/event-filters.ts`; tested with `TZ` east and west of UTC) |

Remote Combobox popups keep `BaseCombobox.Empty` mounted while loading or showing no results. Besides announcing the empty/loading state, its presence makes Escape close only the combobox rather than propagating to the containing dialog when the result list is empty. Infinite-scroll controls live beside the List inside the popup’s scrolling element, with the observer rooted there. Mount the observer component inside the portal: an effect in the control’s parent can run before portal refs exist and never attach for an already-loaded first page.
