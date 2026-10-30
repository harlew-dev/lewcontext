# Design and components

Tailwind CSS v4 utilities in markup on top of BaseUI behaviour, in a Catalyst-like style: restrained zinc, light and dark modes following the system setting, Inter, and Heroicons.

## Styling rules

- **Tailwind utilities only.** Use the default palette (zinc), default spacing, mobile-first breakpoints (`sm:`, `lg:`) and `dark:` variants. No per-component CSS files or custom colour tokens.
- **Complete class strings.** Map variants and colours to complete strings so Tailwind can detect them, e.g. `colors[color]`, never `` `bg-${color}-500` ``.
- **`className` is for placement and spacing.** Components merge it with `cx(className, own…)`, so don't pass utilities that compete with a component's own classes.
- **Icons are Heroicons** (`@heroicons/react/16/solid` or `20/solid`). They carry `data-slot="icon"`, which the component styles size and colour.
- **Respect reduced motion.** `MotionConfig reducedMotion="user"` covers motion, and plain transitions use `motion-reduce:` variants.

## BaseUI underneath

BaseUI provides behaviour (focus, keyboard, overlays) for Button, Input, Textarea, Select (→ `Listbox`), Modal (→ `Dialog`), Popover (→ `Dropdown`) and Drawer (→ mobile sidebar).

- Style BaseUI by replacing its override slots with `slot()` from `lib/baseui.tsx`. It renders your element with Tailwind classes and maps BaseUI state props to data attributes: `data-hover`, `data-active`, `data-focus`, `data-disabled`, `data-invalid`, `data-open`, `data-selected`, `data-loading` and `data-closed`.
- Plain links and buttons get the same attributes from `useInteractive` (`lib/interactive.ts`), via `Link` and `Clickable`.
- Don't use `style` overrides, except `transparentSurface` to neutralise a BaseUI surface.
- **Overlays animate in and out.**
  - `Dialog` and the mobile sidebar transition on `data-closed`, which `slot()` also sets while a Modal or Drawer is leaving (`$isOpen` false). Closed overlays get `data-closed:pointer-events-none`, since BaseUI keeps them mounted for 500 ms.
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
| `button.tsx` | `Button` `color` \| `outline` \| `plain`, `href` (renders a link; also `shallow`, `scroll`), `loading`, `disabled` · `TouchTarget` |
| `link.tsx` / `clickable.tsx` | `Link` (Next link with data attributes) · `Clickable` (a link with `href`, otherwise a button) |
| `badge.tsx` | `Badge color` (`zinc`, `lime`, `purple`, `pink`, `rose`) |
| `avatar.tsx` | `Avatar src`, `initials`, `square` |
| `feedback.tsx` | `Notice variant` (`error` alert, `success` status), `onRetry`, `onDismiss` · `EmptyState title description action` · `ListSkeleton label rows` |
| `fieldset.tsx` | `Fieldset`, `Legend`, `FieldGroup`, `Field disabled`, `Label`, `Description`, `ErrorMessage` (wired to the control for labels and errors) |
| `input.tsx` / `textarea.tsx` | `Input`, `InputGroup` (leading icon) · `Textarea` |
| `listbox.tsx` | `Listbox<T> value onChange aria-label` · `ListboxOption value` · `ListboxLabel` |
| `dialog.tsx` | `Dialog open onClose size` (`xs`…`5xl`, default `lg`) · `DialogTitle`, `DialogDescription`, `DialogBody`, `DialogActions` |
| `dropdown.tsx` | `Dropdown`, `DropdownButton`, `DropdownMenu`, `DropdownItem href keepOpen aria-current`, `DropdownHeader`, `DropdownLabel`, `DropdownDivider` |
| `settings-section.tsx` | `SettingsSection title description` (account and workspace settings) |
| `table.tsx` | `Table bleed dense grid striped` · `TableHead`, `TableBody` · `TableRow href target title` (row link) · `TableHeader href sort shallow` (sortable) · `TableCell` |
| `pagination.tsx` | `Pagination`, `PaginationPrevious href`, `PaginationNext href`, `PaginationList`, `PaginationPage href current`, `PaginationGap` |
| `paginated-table.tsx` | `PaginatedTable` ([pagination.md](pagination.md)) |
| `sidebar.tsx` / `navbar.tsx` | `Sidebar*` (`SidebarItem href current`) · `Navbar*` |
| `heading.tsx` / `text.tsx` | `Heading level`, `Subheading` · `Text`, `TextLink`, `Strong`, `Code` |
| `description-list.tsx` / `divider.tsx` | `DescriptionList`, `DescriptionTerm`, `DescriptionDetails` · `Divider soft` |
| `route-progress.tsx` / `logo.tsx` | `RouteProgress` · `Logo` |

## Feature components (`components/app/<feature>`)

- **`projects`**, shared by the dashboard, projects list and project page: `ProjectTableHead`, `ProjectRow` (links within `useWorkspace()`), `ProjectEmptyState`, `ProjectAvatar`, `ProjectStatusBadge`, `formatDate`, `ProjectDialog`, `ProjectNotices`, `RefreshButton`, `useProjectEditor`, and `projectQuery` / `projectsPath(workspace, query)` / `indexQuery`.
- **`workspaces`**, shared by onboarding, the switcher and workspace settings: `WorkspaceAvatar` (the name's first letter), `WorkspaceNameField`, `useCreateWorkspace` and `CreateWorkspaceDialog`.

The workspace switcher (`WorkspaceDropdown` in `application-layout.tsx`) shows the current workspace and lists every workspace newest first, loading more as the list scrolls ([pagination.md](pagination.md#infinite-lists)).
