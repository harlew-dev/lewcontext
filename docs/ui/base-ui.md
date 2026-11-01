# BaseUI

[Base Web](https://baseweb.design) (`baseui`, by Uber) is the React component library under our interactive controls. We use it for **behaviour**: focus management, keyboard handling, overlays and positioning, ARIA. Our own Tailwind components in `components/ui` draw everything you see. How those components are styled is in [design.md](design.md#baseui-underneath).

Pinned at `baseui` 18.2.0 with React 18.3.1. BaseUI 18 needs React 18, which is why React Router stays on v7 ([README.md](README.md#dependencies)).

## Setup

| Where | What |
| --- | --- |
| `root.tsx` | `<BaseProvider theme={theme}>` around every page. It provides the theme and BaseUI's `LayersManager`, the element overlays are rendered into. |
| `lib/theme.ts` | `createTheme` with Inter and our zinc/blue primaries, for the little BaseUI still draws itself (font, focus, fallbacks). |
| `entry.client.tsx`, `lib/styletron.ts` | BaseUI styles itself with Styletron (CSS-in-JS). The browser engine picks up the styles already in the prerendered HTML instead of adding them again. |
| `entry.server.tsx` | At build time, collects each prerendered page's Styletron CSS into `<style class="_styletron_hydrate_">` in `<head>`. This is why the CSP allows inline CSS (but never inline scripts). |
| `package.json` `overrides` | Unify the React 18 types and pin patched versions of BaseUI's older dependencies (`react-uid`, `react-window`, `react-virtualized-auto-sizer`). Check them when upgrading BaseUI. |

## What we use

| BaseUI component | Our component | What BaseUI provides |
| --- | --- | --- |
| `Button` | `Button` (`button.tsx`) | Hover, press and focus-visible tracking |
| `Input`, `Textarea` | `Input`, `Textarea` | The input; `Field` wires id, description and disabled state |
| `Select` | `Listbox` | Combobox keyboard handling, option highlighting, popover positioning |
| `Select` (`multi`, `searchable`) | `Combobox` (`combobox.tsx`) | Typing to search, ↑ ↓ and Enter, toggling an option chosen twice, the popover; kept open while picking (`closeOnSelect={false}`), the search kept (`onSelectResetsInput={false}`), Backspace removing nothing |
| `Calendar` (`baseui/datepicker`) | `Calendar` (`calendar.tsx`) | The month grid, arrow keys, picking a day or a range, `minDate` / `maxDate` |
| `Popover` (`StatefulPopover`) | `Dropdown` | Click trigger, positioning, focus lock, Escape and outside-click dismissal, menu ARIA |
| `Tooltip` (`StatefulTooltip`) | `Tooltip` (`tooltip.tsx`) | Hover and keyboard-focus trigger with delays, positioning, Escape, `role="tooltip"` and `aria-describedby` |
| `Modal` | `Dialog` | Focus lock, Escape, backdrop click, enter/leave timing |
| `Drawer` | the mobile sidebar (`sidebar-layout.tsx`) | The same as `Modal`, sliding from the left |
| `toaster` | `toast`, `Toaster` | Timer (paused on hover/focus) and the stack |

Don't add a second component library beside it. A new interactive control starts from the BaseUI component that has its behaviour.

## How BaseUI components are customised

Each BaseUI component is built from named parts, its **override slots** (`Root`, `Dialog`, `DialogContainer`, `Body`, `Inner`…). `overrides` replaces a slot's component:

```tsx
<Modal
  overrides={{
    Root: { component: DialogBackdrop },
    Dialog: { component: DialogPanel, props: { className: cx(sizes[size], className) } },
    Close: { component: NoCloseButton },
  }}
>
```

- BaseUI passes each slot its state as `$`-prefixed props (`$isHovered`, `$isOpen`, `$isVisible`…).
- `slot(tag, className)` (`lib/baseui.tsx`) builds a slot component that renders `tag` with Tailwind classes and turns those `$` props into data attributes (`data-hover`, `data-focus`, `data-closed`…) for the classes to target. The `$` props never reach the DOM.
- A slot BaseUI renders but we don't want gets a component that returns `null` (`NoCloseButton`).
- Don't use `style` overrides, except `transparentSurface` to neutralise a BaseUI surface.

### Overlays and layers

`Modal`, `Drawer` and `Popover` render through BaseUI's `Layer`, which portals them into the `LayersManager` host from `BaseProvider`. `Layer` creates its own container `<div>` there when it mounts, and removes it when it unmounts.

`Modal` and `Drawer` animate with two state props:

| Moment | `$isOpen` | `$isVisible` | `slot()` sets |
| --- | --- | --- | --- |
| Rendered, about to enter | true | false | `data-closed` |
| Entered (one animation frame later) | true | true | nothing |
| Leaving (kept mounted for 500 ms) | false | true | `data-closed` |

So our classes transition on `data-closed` in both directions. Popovers instead get `popoverOverrides`, `popoverPanel` and `animateOutTime={popoverExit}` (see [design.md](design.md#baseui-underneath)).

## Known issues and workarounds

| Issue | Workaround (where) |
| --- | --- |
| `Button`'s `isLoading` collapses the content to 0px wide | `Button` draws its own spinner over the hidden label (`button.tsx`) |
| `Input` doesn't report hover | Input styles use `hover:` rather than `data-hover:` (`input.tsx`) |
| A popover fades by animating its Body's opacity, which turns off `backdrop-blur` in the panel inside | The Body stays opaque, and the panel fades itself (`popoverOverrides`, `lib/baseui.tsx`) |
| `returnFocus` loses track of the opener (focus landed on "Skip to content") | `returnFocus={false}` and `<ReturnFocus opener>` (`lib/return-focus.tsx`) |
| The toast's close icon can't be reached by keyboard | Our `DismissButton` replaces it (`toast.tsx`) |
| `Select` positions its panel below the button | A popper.js modifier, `alignSelection`, opens it over the button (`listbox.tsx`) |
| `Select` names the control "Selected <option's `labelKey`>. <aria-label>", which reads "Selected [object Object]." when the label is markup | Each option also gets `text`, the text inside its children, and `labelKey="text"` (`listbox.tsx`, `listbox.test.tsx`) |
| The `Select`'s options carry `aria-selected` for the **highlighted** row, not the chosen one | `Combobox` gives each row `aria-checked` (`true`, `false` or `mixed`) for what's picked (`ComboboxOptionItem`), and says it in its text for screen readers too |
| `Select` with `multi` renders each chosen value as its `Tag` (the `MultiValue` slot), whose remove is `onActionClick`, not `removeValue` | `ComboboxChip` takes `onActionClick` (`combobox.tsx`) |
| The datepicker's `Calendar` hands back **local `Date`s at local midnight**: `toISOString()` on one moves the day for anyone east of UTC | Read the year, month and day (`getFullYear`, `getMonth`, `getDate`) and build the UTC day from them (`utcDay` in `components/app/admin/event-filters.ts`; tested with `TZ` east and west of UTC) |
| The `Calendar` picks a range only when `value` is an array; with `null` every click is a single day | `Calendar` passes `[]` for nothing picked (`calendar.tsx`) |
| The `Calendar`'s month and year are menus in their own popovers | Replaced by plain text (`MonthYearSelectPopover`, `MonthYearSelectButton`); the arrows change month |
| With two months shown, the `Calendar` opens on today's month and the next, which `maxDate` may disable | With nothing picked, `highlightedDate` opens it a month earlier (`calendar.tsx`) |
| Dialogs don't animate open under `make dev` | Not fixed; production animates. See below |

### Dialogs don't animate open in dev

Found 2026-10-30. A side effect of BaseUI's `Layer` and React's development-only [StrictMode](strict-mode.md).

**Symptom.** Under `make dev` a `Dialog` appears with no opening transition, while closing still fades and scales out. In the production build both directions animate.

| Build | What the dialog panel does on open (sampled every frame) |
| --- | --- |
| Dev (:3000) | Inserted closed (opacity 0), then about 45 ms later its element is **replaced** by a new one that is already open: opacity 0 jumps to 1 in one frame. |
| Production (the embedded build served by Go) | The same element goes from opacity 0 to 0.33, 0.69, 0.88, 0.97 and 1, and scale from 0.95 to 1, over ~150 ms. |

**Cause.**

1. A CSS transition only runs when the browser sees the same element change style. The dialog uses `data-closed:opacity-0 data-closed:scale-95` (`components/ui/dialog.tsx`), set while `$isVisible` is false.
2. `Modal` (`node_modules/baseui/modal/modal.js`, `didOpen`) renders the dialog closed, then sets `isVisible: true` in a `requestAnimationFrame`, which removes `data-closed` and starts the transition.
3. The dialog renders inside `Layer` (`node_modules/baseui/layer/layer.js`). Its `componentDidMount` creates a new container (`addContainer`) and portals into it; `componentWillUnmount` removes the container.
4. StrictMode mounts, unmounts and mounts every component again when it first appears. So `Layer` creates container A, removes it, and creates container B. Moving a portal to a new container makes React rebuild everything inside it.
5. By then `Modal` has already set `isVisible: true`, so the rebuilt panel starts out open. The browser never sees it closed, so there's nothing to animate.

Closing isn't affected, because nothing is rebuilt while the dialog leaves.

**Also affected (unverified).** `Drawer` (the mobile sidebar) uses the same `Layer` and the same `requestAnimationFrame` start, so it very likely has the same dev-only problem. It hasn't been measured.

**Possible fixes.**

- **Animate the entrance with CSS `@starting-style`** (Tailwind v4's `starting:` variant), for example `starting:opacity-0 starting:scale-95` on the panel (`starting:translate-y-12` on phones) and `starting:opacity-0` on the backdrop. The browser animates any newly inserted element from those styles, however often React rebuilds it, and the exit stays on `data-closed`. Needs Chrome 117+, Safari 17.5+ or Firefox 129+; older browsers open without the animation.
- **Pass `mountNode` to the Modal.** `Layer` skips creating its own container when given one and portals straight into it, so the remount wouldn't move the dialog. Untested: it moves the dialog out of the `LayersManager` host, which may change how it stacks with popovers (a `Listbox` inside a dialog).
- **Don't turn StrictMode off** to hide this. It would hide every other bug it catches too.
