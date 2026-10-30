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
| `dropdown.tsx` | `Dropdown`, `DropdownButton`, `DropdownMenu`, `DropdownItem`, `DropdownHeader`, `DropdownLabel`, `DropdownDivider` |
| `table.tsx` | `Table bleed dense grid striped` · `TableHead`, `TableBody` · `TableRow href target title` (row link) · `TableHeader href sort shallow` (sortable) · `TableCell` |
| `pagination.tsx` | `Pagination`, `PaginationPrevious href`, `PaginationNext href`, `PaginationList`, `PaginationPage href current`, `PaginationGap` |
| `paginated-table.tsx` | `PaginatedTable` ([pagination.md](pagination.md)) |
| `sidebar.tsx` / `navbar.tsx` | `Sidebar*` (`SidebarItem href current`) · `Navbar*` |
| `heading.tsx` / `text.tsx` | `Heading level`, `Subheading` · `Text`, `TextLink`, `Strong`, `Code` |
| `description-list.tsx` / `divider.tsx` | `DescriptionList`, `DescriptionTerm`, `DescriptionDetails` · `Divider soft` |
| `route-progress.tsx` / `logo.tsx` | `RouteProgress` · `Logo` |

## Feature components (`components/app/projects`)

These are shared by the dashboard, projects list and project page: `ProjectTableHead`, `ProjectRow`, `ProjectEmptyState`, `ProjectAvatar`, `ProjectStatusBadge`, `formatDate`, `ProjectDialog`, `ProjectNotices`, `RefreshButton`, `useProjectEditor`, and `projectQuery` / `projectsPath` / `projectsApiPath`.
