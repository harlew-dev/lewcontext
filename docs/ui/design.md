# Design and components

Tailwind CSS v4 utilities in markup on top of BaseUI behaviour, in a Catalyst-like style: restrained zinc, light and dark modes following the system setting, Inter, and Heroicons.

## Styling rules

- **Tailwind utilities only.** Use the default palette (zinc), default spacing, mobile-first breakpoints (`sm:`, `lg:`) and `dark:` variants. No per-component CSS files or custom colour tokens.
- **Complete class strings.** Map variants and colours to complete strings so Tailwind can detect them, e.g. `colors[color]`, never `` `bg-${color}-500` ``.
- **`className` is for placement and spacing.** Components merge it with `cx(className, own…)`, so don't pass utilities that compete with a component's own classes.
- **Icons are Heroicons** (`@heroicons/react/16/solid` or `20/solid`). They carry `data-slot="icon"`, which the component styles size and colour.
- **Respect reduced motion.** `MotionConfig reducedMotion="user"` covers motion, and plain transitions use `motion-reduce:` variants.

## BaseUI underneath

What BaseUI is, how it's set up and its known issues are in [base-ui.md](base-ui.md). This section is how we style it.

BaseUI provides behaviour (focus, keyboard, overlays) for Button, Input, Textarea, Select (→ `Listbox`), Modal (→ `Dialog`), Popover (→ `Dropdown`) and Drawer (→ mobile sidebar).

- Style BaseUI by replacing its override slots with `slot()` from `lib/baseui.tsx`. It renders your element with Tailwind classes and maps BaseUI state props to data attributes: `data-hover`, `data-active`, `data-focus`, `data-disabled`, `data-invalid`, `data-open`, `data-selected`, `data-loading` and `data-closed`.
- Plain links and buttons get the same attributes from `useInteractive` (`lib/interactive.ts`), via `Link` and `Clickable`.
- Don't use `style` overrides, except `transparentSurface` to neutralise a BaseUI surface.
- **Overlays animate in and out.**
  - `Dialog` and the mobile sidebar transition on `data-closed`, which `slot()` also sets while a Modal or Drawer is leaving (`$isOpen` false). Closed overlays get `data-closed:pointer-events-none`, since BaseUI keeps them mounted for 500 ms. Under `make dev` a `Dialog` doesn't animate open, only closed (the mobile sidebar very likely too): a side effect of BaseUI's `Layer` and React StrictMode ([base-ui.md](base-ui.md#dialogs-dont-animate-open-in-dev)).
  - Popovers (`Dropdown`, `Listbox`) use `popoverOverrides`, `popoverPanel` on the panel, and `animateOutTime={popoverExit}` (0 with reduced motion). The panel fades itself, not BaseUI's Body: an ancestor with opacity below 1 turns off `backdrop-blur`.
- **Open overlays stop the page scrolling.** `Dialog` and the mobile sidebar call `useScrollLock(open)`, and open popovers (`Dropdown`, `Listbox`) call `usePopoverScrollLock(open)` (`lib/scroll-lock.ts`), so a menu can't drift from its trigger as the page scrolls.
  - The lock hides the page scrollbar and pads `<html>` by its width so nothing shifts (as Catalyst/Headless UI do).
  - It keeps the page's scroll position, because focus moving into an overlay can otherwise scroll the page to the top.
  - It counts holders, so a listbox inside a dialog doesn't unlock the page early.
  - The popover version also blocks wheel and touch scrolling outside the panel (`data-popover-panel`), including in a dialog's own scrolling content.
- **Overlays give focus back to whatever opened them.** BaseUI's own `returnFocus` loses track (focus landed on "Skip to content"), so `Dialog` and `Dropdown` set `returnFocus={false}`, remember the opener (`focusedElement()`) and render `<ReturnFocus opener>` (`lib/return-focus.tsx`), which refocuses it when BaseUI removes the overlay. A `DropdownItem` closes its menu first and runs its `onClick` once focus is back on the trigger, so a dialog opened from a menu returns focus to the menu's button. `keepOpen` items ("Show more", "Retry") act inside the open menu.
- **`Listbox` opens over its button** with the selected option on top of it and the option text in line with the button text (Headless UI's "selection" anchor). This is a popper.js modifier (`alignSelection`), and the panel is kept 16px inside the viewport.
- Classes target the attributes: `data-hover:bg-zinc-950/5 data-focus:outline-2 data-disabled:opacity-50`.

## Component APIs (`components/ui`)

Files are kebab-case and export a family of named components. Keep these props stable. Components with alternatives use a `variant` prop (never `tone`, and never one component per alternative).

| File | Exports and main props |
| --- | --- |
| `button.tsx` | `Button` `color` (`dark/zinc`, or `red` for destructive actions: remove, leave) \| `outline` \| `plain`, `href` (renders a React Router link), `loading`, `disabled` · `TouchTarget` |
| `link.tsx` / `clickable.tsx` | `Link` (React Router link with data attributes; `href` instead of `to`) · `Clickable` (a link with `href`, otherwise a button) |
| `badge.tsx` | `Badge color` (`zinc`, `lime`, `purple`, `pink`, `rose`) |
| `avatar.tsx` | `Avatar src`, `initials`, `square` |
| `feedback.tsx` | `Notice variant` (`error` alert, `success` status), `onRetry`, `onDismiss` · `ErrorNotice error` (a `useAction` failure, or nothing) · `EmptyState title description action` · `ListSkeleton label rows` |
| `toast.tsx` | `Toaster` (mounted once by the signed-in pages) · `toast.success(message, { duration })`, `toast.error(…)` ([Toasts](#toasts)) |
| `fieldset.tsx` | `Fieldset`, `Legend`, `FieldGroup`, `Field disabled`, `Label`, `Description`, `ErrorMessage` (wired to the control for labels and errors) |
| `input.tsx` / `textarea.tsx` | `Input`, `InputGroup` (leading icon, or `InputActions` at the end: `InputLock label` for a read-only value that can't change (`label` says why; default "Can’t be changed"), `InputButton aria-label onClick` for an icon action such as Copy; at most two) · `Textarea` |
| `tooltip.tsx` | `Tooltip content placement` (`top`, `bottom`, `left`, `right`) around a focusable child that forwards its ref |
| `listbox.tsx` | `Listbox<T> value onChange aria-label` · `ListboxOption value` · `ListboxLabel` |
| `dialog.tsx` | `Dialog open onClose size` (`xs`…`5xl`, default `lg`) · `DialogTitle`, `DialogDescription`, `DialogBody`, `DialogActions` |
| `dropdown.tsx` | `Dropdown`, `DropdownButton as plain outline`, `DropdownMenu`, `DropdownItem href keepOpen aria-current`, `DropdownHeader`, `DropdownLabel`, `DropdownDivider` |
| `settings-section.tsx` | `SettingsSection title description` (account and workspace settings) |
| `table.tsx` | `Table bleed dense grid striped` · `TableHead`, `TableBody` · `TableRow href target title` (row link) · `TableHeader href sort` (sortable) · `TableCell` |
| `pagination.tsx` | `Pagination`, `PaginationPrevious href`, `PaginationNext href`, `PaginationList`, `PaginationPage href current`, `PaginationGap` |
| `paginated-table.tsx` | `PaginatedTable` ([pagination.md](pagination.md)) |
| `search-field.tsx` | `SearchField label value onSearch`: updates the URL 300 ms after typing pauses ([pagination.md](pagination.md#url-state)) |
| `sidebar.tsx` / `navbar.tsx` | `Sidebar*` (`SidebarItem href current`), `SidebarToggle` ([Sidebar layout](#sidebar-layout)) · `Navbar*` (`NavbarItem`, `NavbarLabel`) |
| `heading.tsx` / `text.tsx` | `Heading level`, `Subheading` · `Text`, `TextLink`, `Strong`, `Code` |
| `description-list.tsx` / `divider.tsx` | `DescriptionList`, `DescriptionTerm`, `DescriptionDetails` · `Divider soft` |
| `route-progress.tsx` / `logo.tsx` | `RouteProgress` · `Logo` (`href` makes it a link; the sign-in pages link it to `/`) |

## Toasts

`toast.success(message)` and `toast.error(message)` (`components/ui/toast.tsx`) show a short-lived message in a stack at the bottom right (across the bottom on phones).

- **Successes** of any action are toasts: "Workspace renamed.", "Project created.", "All other sessions have been signed out."
- **Errors** stay inline when the action is a form (`<ErrorNotice error={action.error}>` beside its fields, see [api.md](api.md#changing-data)), and load failures keep their `Notice` with Retry. An action that is only a button, with no fields (sign out, sign out other sessions), reports its error as `toast.error`.
- A toast hides after 5 seconds, paused while it's hovered or focused. Pass `{ duration }` in milliseconds to change that, or `Infinity` to keep it until dismissed.
- It's BaseUI's `toaster` (timer, stacking) with its slots replaced by `slot()`: a zinc panel with the `Notice` variant's icon and role (`messageVariants` in `feedback.tsx`, so the two stay alike), fading and sliding on `data-closed`. BaseUI's close icon can't be reached by keyboard, so the toast renders our `DismissButton` instead.
- `<Toaster />` is mounted by `pages/app/layout.tsx` and onboarding only, so signing out unmounts it and any open toasts. Calling `toast` where no `Toaster` is mounted throws in development (in tests, render a `<Toaster />`).

## Sidebar layout

`SidebarLayout` (`components/layout/sidebar-layout.tsx`) is the signed-in chrome; `ApplicationLayout` fills it with the workspace menu, navigation and account menu.

- **Desktop:** the sidebar is docked on the left. `<SidebarToggle />`, beside the workspace menu, hides it: the sidebar slides out, the content widens, and a top bar opens with the account menu (avatar, name and chevron). All three move together over 300 ms.
- **One toggle that glides.** The toggle is a single button that never leaves: hiding the sidebar glides it from beside the workspace menu into the top bar's corner, and showing glides it back. It stays visible (with `visible`) while the sidebar is invisible, its label switches between "Hide sidebar" and "Show sidebar", and focus stays on it. Its glide distance (`desktopToggleClasses`) follows the sidebar header's padding and the button's size, so change them together.
- **Remembered on this device.** `useDockedSidebar` (`lib/use-docked-sidebar.ts`) stores `lewcontext:sidebar=hidden` in `localStorage` (docked is the default and isn't stored). It reads the value while rendering, so the first paint is already right: the app layout renders only in the browser (never prerendered), and no effect flips it after mount, which would flash the sidebar and play its slide. Without storage (blocked, private windows) the sidebar starts docked and the toggle still works.
- **Hidden means out of reach.** The hidden sidebar turns `invisible` (out of the tab order and the accessibility tree) once it has slid out, and visible at once when it returns; the top bar does the same while the sidebar is docked.
- **Mobile:** a top bar with the menu button and the account menu (avatar, truncated name, chevron). The slide-over sidebar's close button sits where the desktop toggle does, beside the workspace menu (`SidebarToggleContext` tells `SidebarToggle` which sidebar it's in).
- **Matching heights.** `SidebarToggle` is an icon-only `SidebarItem`, so it's exactly as tall as the workspace menu (36px, 44px on phones). Icon-only items keep their full-size icon: the trailing-chevron sizing skips an item whose icon is its only content (`*:not-nth-2:last:`), in both `SidebarItem` and `NavbarItem`.

## Feature components (`components/app/<feature>`)

- **`projects`**, shared by the dashboard, projects list and project page: `ProjectTableHead`, `ProjectRow` (links within `useWorkspace()`), `ProjectEmptyState`, `ProjectAvatar`, `ProjectStatusBadge`, `ProjectDialog`, `ProjectNotices` (load error with Retry), `RefreshButton`, `useProjectEditor`, and `projectQuery` / `projectsPath(workspace, query)` / `indexQuery`.
- **`workspaces`**, shared by onboarding, the switcher and workspace settings: `WorkspaceAvatar` (the name's first letter), `WorkspaceNameField`, `useCreateWorkspace` and `CreateWorkspaceDialog`.

The workspace switcher (`WorkspaceDropdown` in `application-layout.tsx`) shows the current workspace and lists every workspace newest first, loading more as the list scrolls ([pagination.md](pagination.md#infinite-lists)).
