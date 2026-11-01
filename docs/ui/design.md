# Design and components

Tailwind CSS v4 utilities in markup on top of BaseUI behaviour, in a Catalyst-like style: restrained zinc, light and dark modes following the system setting, Inter, and Heroicons.

## Styling rules

- **Tailwind utilities only.** Use the default palette (zinc), default spacing, mobile-first breakpoints (`sm:`, `lg:`) and `dark:` variants. No per-component CSS files or custom colour tokens.
- **Complete class strings.** Map variants and colours to complete strings so Tailwind can detect them, e.g. `colors[color]`, never `` `bg-${color}-500` ``.
- **`className` is for placement and spacing.** Components merge it with `cx(className, own…)`, so don't pass utilities that compete with a component's own classes.
- **Icons are Heroicons** (`@heroicons/react/16/solid` or `20/solid`). They carry `data-slot="icon"`, which the component styles size and colour.
- **Respect reduced motion.** `MotionConfig reducedMotion="user"` covers motion, and plain transitions use `motion-reduce:` variants.
- **Hover and focus backgrounds slide.** In a group of items (sidebar sections, tabs, menu items, listbox options) the hovered or focused item's background is one shared `Highlight` (`components/ui/highlight.tsx`) that slides from item to item ([Sliding highlight](#sliding-highlight)).
- **Pages fade in.** `PageTransition` (`components/layout/page-transition.tsx`) fades a new page's content in and lifts it 6px over 200 ms with CSS `@starting-style` (Tailwind's `starting:`), so it needs no JavaScript and leaves no transform behind. The app layout keys it with `usePageKey()`: the pathname (a new search or page number isn't a new page), or for tabs, the tab group's path, and `AuthLayout` wraps each sign-in page in it. Browsers without `@starting-style`, and reduced motion, show the page at once.
- **Tabs are one page.** Tabs that are pages of their own (workspace settings' General and Members) live under a layout route that renders the heading and tabs and exports `handle: TransitionHandle = { tabGroup: true }`. The tabs stay mounted, so the current tab's underline slides (`layoutId`, as in the sidebar); the app layout doesn't fade the page in again; and the layout wraps the content below in `PageTransition from="left" | "right"`, keyed by pathname, so it slides 12px in from the side of the tab chosen (enter only; the old tab goes at once). Nothing slides on arrival or when only the workspace changes.

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

Where a `Button` goes in a form, dialog, settings section or page header, and which style it takes, is in [forms-and-actions.md](forms-and-actions.md).

Files are kebab-case and export a family of named components. Keep these props stable. Components with alternatives use a `variant` prop (never `tone`, and never one component per alternative).

| File | Exports and main props |
| --- | --- |
| `button.tsx` | `Button` `color` (`dark/zinc`, or `red` for destructive actions: remove, leave, delete) \| `outline` \| `plain`, `href` (renders a React Router link, with optional navigation `state`), `loading`, `disabled`, `disabledReason` (while disabled and not loading: a tooltip on a wrapper, since a disabled button gets no pointer events, and the button's accessible description; [forms-and-actions.md](forms-and-actions.md#behaviour-same-everywhere)) · `TouchTarget` |
| `link.tsx` / `clickable.tsx` | `Link` (React Router link with data attributes; `href` instead of `to`) · `Clickable` (a link with `href`, otherwise a button) |
| `badge.tsx` | `Badge color` (`zinc`, `lime`, `purple`, `pink`, `rose`, `red`: something apart from the app, the admin dashboard's "Admin") |
| `avatar.tsx` | `Avatar src`, `initials`, `square` |
| `feedback.tsx` | `Notice variant` (`error` alert, `success` status, `info` status in blue: the invitation callout, `warning` note in amber: what a confirmation will do, read with the dialog rather than announced), `onRetry`, `onDismiss` · `ErrorNotice error` (a `useAction` failure, or nothing; a 500's request ID under it as its `Reference`) · `Reference requestId` ("Reference: req_…", selectable whole) · `EmptyState title description action` · `ListSkeleton label rows` |
| `toast.tsx` | `Toaster` (mounted once by the signed-in pages) · `toast.success(message, { duration })`, `toast.error(…)` ([Toasts](#toasts)) |
| `highlight.tsx` | `Highlight className slide` (the sliding background; `slide={false}` draws it in place) · `useHighlightGroup`, `HighlightProvider`, `useHighlight` ([Sliding highlight](#sliding-highlight)) |
| `fieldset.tsx` | `Fieldset`, `Legend`, `FieldGroup`, `Field disabled locked` ([Locked fields](#locked-fields)), `Label`, `Description`, `ErrorMessage` (wired to the control for labels and errors; a `Label className="sr-only"` adds no space above the control, so a field beside a `SettingsSection` title lines up with it) |
| `input.tsx` / `textarea.tsx` | `Input blurred onReveal` (a sensitive value drawn blurred over the input, `data-blurred`; with `onReveal`, a click anywhere on it, or on its "Click to reveal" tooltip, reveals it; [Secrets and sensitive values](#secrets-and-sensitive-values)), `InputGroup` (leading icon, or `InputActions` at the end: `InputButton aria-label onClick` for an icon action such as Copy, then a locked `Field`'s lock; at most two) · `Textarea` |
| `otp-input.tsx` | `OtpInput value onChange invalid autoSubmit autoFocus name`: a 6-digit code (an authenticator app's) drawn as six boxes, 3 + 3, over **one** invisible BaseUI input, so a pasted "123 456", a password manager's fill (`autocomplete="one-time-code"`), the `Field` label and screen readers see one field. Keeps only digits, at most six; the caret stays after the last digit unless everything is selected. `autoSubmit` submits the form (`requestSubmit`, in an effect so the handler sees the new value) whenever a complete code goes in, never for one shown already full. Not BaseUI's `PinCode`: six inputs with one label, and a paste with a space is dropped |
| `tooltip.tsx` | `Tooltip content placement onClick disabled open` (`top`, `bottom`, `left`, `right`) around a focusable child that forwards its ref. `onClick` makes the tooltip itself a pointer shortcut (it stays open while hovered), never the only way to do something; `disabled` keeps it closed without remounting the child; `open` holds it open whatever the pointer and focus do (a locked field's lock after a try at typing) |
| `listbox.tsx` | `Listbox<T> value onChange aria-label` · `ListboxOption value` · `ListboxLabel` |
| `dialog.tsx` | `Dialog open onClose size` (`xs`…`5xl`, default `lg`) · `DialogTitle`, `DialogDescription`, `DialogBody`, `DialogActions` |
| `confirmation-dialog.tsx` | `ConfirmationDialog open onClose title description confirm variant disabled disabledReason onConfirm onConfirmed onError` ([Confirmations](#confirmations)) |
| `dropdown.tsx` | `Dropdown`, `DropdownButton as plain outline`, `DropdownMenu`, `DropdownItem href keepOpen aria-current variant selected` (`danger`: red text and icon, for Delete, and Sign out in the account menus), `DropdownHeader`, `DropdownLabel`, `DropdownDivider` |
| `settings-section.tsx` | `SettingsSection title description variant stacked` (account and workspace settings; `description` is optional, see [Descriptions](#descriptions)): content beside the title, or under it at full width with `stacked`; `variant="danger"` is the danger zone, always stacked, with a red title and the content in a red-tinted panel · `DangerAction title description action`: one row of that panel, text on the left and its button on the right (below on phones) |
| `table.tsx` | `Table bleed dense grid striped` · `TableHead`, `TableBody` · `TableRow href target title` (row link) · `TableHeader href sort` (sortable) · `TableCell` |
| `pagination.tsx` | `Pagination`, `PaginationPrevious href`, `PaginationNext href`, `PaginationList`, `PaginationPage href current`, `PaginationGap` |
| `paginated-table.tsx` | `PaginatedTable` ([pagination.md](pagination.md)) |
| `search-field.tsx` | `SearchField label value onSearch`: updates the URL 300 ms after typing pauses ([pagination.md](pagination.md#url-state)) |
| `sidebar.tsx` / `navbar.tsx` | `Sidebar*` (`SidebarItem href current selected`), `SidebarToggle` ([Sidebar layout](#sidebar-layout)) · `Navbar*` (`NavbarItem current selected`, `NavbarLabel`). `current`: the item's link is this page (the sliding indicator, `aria-current`); `selected`: a grey background for a menu button whose menu holds this page ([Where you are](#where-you-are)) |
| `heading.tsx` / `text.tsx` | `Heading level`, `Subheading` · `Text`, `TextLink`, `TextButton` (a `TextLink` look for an action that doesn't navigate: "Use a recovery code"), `Strong`, `Code` |
| `description-list.tsx` / `divider.tsx` | `DescriptionList`, `DescriptionTerm`, `DescriptionDetails` · `Divider soft` |
| `reveal.tsx` | `Reveal label hideLabel announcement placeholder onReveal`: keeps a secret (a QR code, recovery codes) off the page until asked for. Hidden, it renders only a blurred `placeholder` (`aria-hidden`) with the button over it, never the secret blurred; revealing moves focus to it and announces `announcement`; Hide puts it back. Key it to start hidden again |
| `route-progress.tsx` / `logo.tsx` | `RouteProgress` · `Logo` (`href` makes it a link; the sign-in pages link it to `/`) |
| `stat.tsx` | `Stat title value`: a headline number under a rule, "–" until loaded, with a line of context as children (the dashboard's project counts, the Mail page's queue) |
| `chart.tsx` | `BarChart label summary rows series`: columns per row (a time bucket) with their series stacked, a legend for two or more series, a tooltip per column, and the numbers as a screen-reader table ([Charts](#charts)) |

## Charts

`components/ui/chart.tsx` is the only file that imports Recharts ([README.md](README.md#dependencies)); pages pass rows and series and never style Recharts themselves. Monitoring's sparklines and meters join it there.

- **Colours by series, in a fixed order, never cycled:** sky, orange, emerald, violet, pink, as complete `text-*` classes the marks fill with `currentColor` (`600` shades and `pink-500`; violet steps to `500` on dark). Checked with the dataviz skill's validator against white and zinc-900 with **every pair** compared, since any two segments of a stack can touch: full-colour ΔE ≥ 16.8 and every mark ≥ 3:1; colour-blind ΔE ≥ 7.3, the band that needs a second cue, which the gaps, legend, tooltip and table give. Amber or yellow beside orange, or blue beside violet, fail that check. A sixth series is folded into "Other" by the page. Red, amber and green as *levels* belong to monitoring and come with a word, never colour alone.
- **Quiet marks:** columns at most 24px wide, a 4px rounded end on the top segment only, square at the baseline, a 2px gap in the card's colour between stacked segments; hairline zinc gridlines, no axis lines; ticks and labels in text colours, never the series colour.
- **Accessible:** the SVG is `aria-hidden`; the `figure` carries the `summary` sentence and a `sr-only` table with every value. The tooltip shows the value first, then the series, keyed by a short line of its colour. Animation is off under reduced motion.
- **Tests** check the table's numbers: jsdom has no layout, so the SVG doesn't render there.

## Descriptions

A title doesn't need a description under it. Add one only when it tells the reader something they need that the title and the content beside it don't already make clear; never restate the title ("Password" → "Change your account password").

- **Leave it out** for settings sections whose fields, buttons and lock tooltips explain themselves: General, Workspace ID, Profile, Password, Two-factor authentication, Sign-in sessions.
- **Keep it** where it carries consequences or rules the reader can't infer: the danger zone (`SettingsSection variant="danger"` and every `DangerAction`), confirmation dialogs, and empty states.
- A reason something can't be changed goes in its `Field locked`, not in a description ([Locked fields](#locked-fields)).

## Locked fields

A value someone can see but not change (your email, a workspace ID, the workspace name for a member) is a `Field locked`: `true`, or better the reason ("Your email can’t be changed"; `true` says "Can’t be changed").

- **The `Input` turns read-only** (never `disabled`: it stays focusable, selectable and copyable) and **ends with a lock**: the last of its `InputGroup`'s `InputActions`, after an eye or Copy button, or in a group of its own when the `Input` has none. A locked `InputGroup` needs `InputActions` for the lock to go in, even empty.
- **The reason is the lock's tooltip** (on hover and keyboard focus, its accessible name) **and the input's description**, so a screen reader reads it with the field.
- **Trying to edit it answers.** A character, Backspace or Delete, Ctrl/⌘+V or X, a paste or a drop opens the lock's tooltip (`Tooltip open`) for 3 seconds after the last try. Escape or leaving the input closes it; Tab, arrows and Ctrl/⌘+C don't count.

## Secrets and sensitive values

Two kinds of value stay unreadable until asked for, in case the screen is shared or recorded, each behind an eye or a reveal button:

- **Secrets** let someone into the account: a QR code, a setup key, recovery codes. They stay out of the page until shown: `Reveal` renders only a decoy placeholder, and a masked field holds bullets, never the value. Never blur a secret; a screenshot of a blur can be undone.
- **Sensitive** values are personal but grant nothing alone: your email address in account settings. They're in the page, drawn blurred with `Input blurred`, so the field still reads as a normal field (not a password) and screen readers read it. Pass `onReveal` so a click on the field reveals it, and add an eye `InputButton` (tooltip "Reveal" / "Hide", `aria-pressed`) first in its `InputActions` for the keyboard. The blur is drawn over the input, not as a text shadow, which the input would clip to its line.

## Confirmations

Every "are you sure?" is a `ConfirmationDialog` (`components/ui/confirmation-dialog.tsx`): removing a member, leaving or deleting a workspace, deleting a project or the account. Don't build one from `Dialog` by hand.

```tsx
<ConfirmationDialog
  open={open}
  onClose={() => setOpen(false)}
  title={`Delete ${project.name}?`}
  description="This permanently deletes the project. It can’t be undone."
  confirm="Delete project"     // the button says what it does
  variant="danger"             // red confirm button; "default" is dark/zinc
  disabled={typed !== name}    // e.g. type-to-confirm
  onConfirm={() => api.projects(ws).destroy(project.id)}
  onConfirmed={() => toast.success("Project deleted.")}
  onError={showOnField}        // optional: an ApiError with a field, shown under it
>
  {/* optional fields: type-to-confirm, password */}
</ConfirmationDialog>
```

- `onConfirm` runs through `useAction`: the button shows `loading`, a double click confirms once, and Cancel, Escape and the backdrop do nothing until it settles. The fields are disabled meanwhile.
- A failure keeps the dialog open with the error (`ErrorNotice`) and the typed fields. Success closes it, then calls `onConfirmed`.
- Key the dialog per opening with `useOpenCount(open)` (`lib/use-open-count.ts`): `key={opened}`, so each opening starts empty, without the last opening's values or error. A dialog keyed by its target instead (`"new"`, a project ID) keeps whatever was typed last time; that was the "forms don't reset after submit" bug.
- A dialog whose consequences need to stand out puts them in a `<Notice variant="warning">` at the top of its fields, with a short `description` saying what to do (Delete workspace). Type-to-confirm labels show the name as `“<Code>Acme</Code>”`.
- Labels name what they act on: "Delete project", "Delete workspace", "Delete account", never a bare "Delete".
- Destructive actions sit in a danger zone at the bottom of settings (`SettingsSection variant="danger"` with `DangerAction` rows), or as a `DropdownItem variant="danger"` in a ⋯ menu (`ProjectActions`).

## Sliding highlight

`components/ui/highlight.tsx` draws one background per group that slides to the hovered or focused item (a `motion` `layoutId` with a short spring), as the sidebar does.

| Where | Group | Which item has it | Colour |
| --- | --- | --- | --- |
| `SidebarSection`, `NavbarSection` (tabs, the top bar) | `useHighlightGroup()` in its own `LayoutGroup`; cleared when the pointer leaves the section | the item the mouse is over (`useHighlight().activate()` on `pointerenter`) | `bg-zinc-950/5` / `dark:bg-white/5` |
| `DropdownMenu` | the same; cleared when focus leaves the items (the mouse leaving the menu hands focus back to its `selected` item, or else to the menu, as Headless UI does) | the focused item (items take focus on hover, so pointer and keyboard agree; the menu opens with its `selected` item focused) | `bg-zinc-950/5` / `dark:bg-white/10`; a default item's icon darkens |
| `Listbox` | none needed: its own `LayoutGroup` | the option BaseUI marks `$isHighlighted` (`ListboxOptionContent` renders it) | `bg-zinc-950/5` / `dark:bg-white/10` |

- **The selected option stays blue.** In a `Listbox` the chosen option has a static `bg-blue-500` background with white text and its check mark, above the grey highlight (`-z-5` over `-z-10`), so hovering it changes nothing and the grey slides beneath it.
- **Nothing slides while a menu or listbox opens.** BaseUI positions the panel during its first frames, and a highlight measured before that would slide in from where the panel started. So for its first 150 ms (`usePopoverSettled`, `lib/baseui.tsx`) the highlight is drawn in place (`<Highlight slide={false}>`), and then remounts in place as the sliding one.

- **Items outside a group** (a `SidebarItem` or `NavbarItem` not in a section) keep a plain `data-hover:` background.
- **Under the text while it slides.** In menus and listboxes the highlight is `-z-10` inside the panel's `isolate` stacking context, so it passes beneath every item's text. Don't make an item `isolate`: the highlight would then paint over the items before it mid-slide. In the sidebar and tabs it comes before the item's `relative` link in the DOM instead.
- **Forced colours** hide it (`forced-colors:hidden`); the items' own `forced-colors:` backgrounds apply.
- **Reduced motion** (`MotionConfig`) moves it without sliding.

## Toasts

`toast.success(message)` and `toast.error(message)` (`components/ui/toast.tsx`) show a short-lived message in a stack at the bottom right (across the bottom on phones).

- **Successes** of any action are toasts: "Workspace renamed.", "Project created.", "All other sessions have been signed out."
- **Errors** stay inline when the action is a form (`<ErrorNotice error={action.error}>` beside its fields, see [api.md](api.md#changing-data)), and load failures keep their `Notice` with Retry. An action that is only a button, with no fields (sign out, sign out other sessions), reports its error as `toast.error`.
- A toast belongs to the `Toaster` that was mounted last. A toast shown just before navigating to a page outside the app layout (onboarding, login) is lost with the layout, and React Router commits a redirect after `navigate()` resolves. Say it on the next page instead, through navigation state: login's "Your account has been deleted.", onboarding's "<workspace> deleted." and "You left <workspace>.". Arriving in the app layout from outside it works the same way: `arrivalState` carries `joinedWorkspace` or `createdWorkspace`, and the layout's `ArrivalToast` shows "You joined Acme." or "Acme created." (creating from the switcher uses the same form, so it goes this way too).
- **Every finished action says so.** Creating, renaming, leaving and deleting a workspace; creating, editing and deleting a project; inviting, changing a role, removing a member, revoking an invitation; profile, password and other sessions. Signing out and deleting the account end on login, which is its own confirmation (login says the account was deleted).
- A toast hides after 5 seconds, paused while it's hovered or focused. Pass `{ duration }` in milliseconds to change that, or `Infinity` to keep it until dismissed.
- It's BaseUI's `toaster` (timer, stacking) with its slots replaced by `slot()`: a zinc panel with the `Notice` variant's icon and role (`messageVariants` in `feedback.tsx`, so the two stay alike), fading and sliding on `data-closed`. BaseUI's close icon can't be reached by keyboard, so the toast renders our `DismissButton` instead.
- `<Toaster />` is mounted by `pages/app/layout.tsx` and onboarding only, so signing out unmounts it and any open toasts. Calling `toast` where no `Toaster` is mounted throws in development (in tests, render a `<Toaster />`).

## Sidebar layout

`SidebarLayout` (`components/layout/sidebar-layout.tsx`) is the signed-in chrome; `ApplicationLayout` fills it with the workspace menu, navigation and account menu.

- **Where the menus live.** The workspace menu is in the sidebar's header and, duplicated, at the right of the top bar, so switching workspace is one tap on a phone and with the sidebar hidden. The account menu is only in the sidebar's footer, on phones too (open the slide-over sidebar). "Getting started" is always in the sidebar, so the account menu doesn't repeat it.

- **Desktop:** the sidebar is docked on the left. `<SidebarToggle />`, beside the workspace menu, hides it: the sidebar slides out, the content widens, and a top bar opens with the workspace menu. All three move together over 300 ms.
- **One toggle that glides.** The toggle is a single button that never leaves: hiding the sidebar glides it from beside the workspace menu into the top bar's corner, and showing glides it back. It stays visible (with `visible`) while the sidebar is invisible, its label switches between "Hide sidebar" and "Show sidebar", and focus stays on it. Its glide distance (`desktopToggleClasses`) follows the sidebar header's padding and the button's size, so change them together.
- **Remembered on this device.** `useDockedSidebar` (`lib/use-docked-sidebar.ts`) stores `lewcontext:sidebar=hidden` in `localStorage` (docked is the default and isn't stored). It reads the value while rendering, so the first paint is already right: the app layout renders only in the browser (never prerendered), and no effect flips it after mount, which would flash the sidebar and play its slide. Without storage (blocked, private windows) the sidebar starts docked and the toggle still works.
- **Hidden means out of reach.** The hidden sidebar turns `invisible` (out of the tab order and the accessibility tree) once it has slid out, and visible at once when it returns; the top bar does the same while the sidebar is docked.
- **Mobile:** a top bar with the menu button on the left and the workspace menu on the right (avatar, truncated name, chevron); the account menu is at the bottom of the slide-over sidebar. Opening another page closes the slide-over, also from a menu in it (`SidebarLayout` watches the pathname). The slide-over sidebar's close button sits where the desktop toggle does, beside the workspace menu (`SidebarToggleContext` tells `SidebarToggle` which sidebar it's in).
- **Matching heights.** `SidebarToggle` is an icon-only `SidebarItem`, so it's exactly as tall as the workspace menu (36px, 44px on phones). Icon-only items keep their full-size icon: the trailing-chevron sizing skips an item whose icon is its only content (`*:not-nth-2:last:`), in both `SidebarItem` and `NavbarItem`.

## Where you are

The sidebar has only Overview and Projects (plus Getting started); the settings pages live in menus. So that you can still see where you are:

- **A page in the sidebar** is its `SidebarItem current`: the indicator bar slides to it.
- **A page in a menu** selects the menu's button (`DropdownButton as={SidebarItem | NavbarItem} selected`, a grey background). Workspace settings (General and Members) select the workspace menu, in the sidebar header and in the top bar (phones, or the desktop sidebar hidden); account settings select the account menu in the sidebar footer. Inside the menu the page's item is `DropdownItem selected aria-current="page"`, a default focus: the menu opens with it focused (and so highlighted; Enter opens it, the arrow keys move on from it), hovering another item takes focus and the highlight away, and the mouse leaving the menu returns them to it.
- **The workspace you're in** keeps its check in the switcher (and `aria-current`).
- Don't add sidebar items for settings pages.

## Feature components (`components/app/<feature>`)

- **`projects`**, shared by the dashboard, projects list and project page: `ProjectTableHead`, `ProjectRow` (links within `useWorkspace()`; for members who may edit or delete projects (`hasProjectActions`), a last column with `ProjectActions`, whose Edit project calls the row's `onEdit`), `ProjectActions` (the ⋯ menu: Edit project with `onEdit` and `project.update`, Delete project with `project.destroy` and a confirmation; also in the project page's header, which has no separate Edit button), `ProjectEmptyState`, `ProjectAvatar`, `ProjectStatusBadge`, `ProjectDialog`, `ProjectNotices` (load error with Retry), `useProjectEditor`, `StatusField` (the status filter, also on the admin workspace page), and `projectQuery` / `projectsPath(workspace, query)` / `projectParams` / `indexQuery`.
- **`workspaces`**, shared by onboarding, the switcher and workspace settings: `WorkspaceAvatar` (the name's first letter; also in the account's last-owner warning), `WorkspaceNameField`, `useCreateWorkspace` and `CreateWorkspaceDialog`.
- **`invitations`**, shared by the invitation page, login and register: `InvitationCallout` (an `info` Notice, "<workspace> has invited you to join." with who invited you and the role; a skeleton while checking, an error for a dead link) and `withArticle(role)`.
- **`admin`**, shared by the admin dashboard's Users, Workspaces and Email blocks: `adminListQuery` / `adminListPath` (URL state), `AdminTableHead` (sortable columns, also the workspace page's projects; a `hidden` column is named for screen readers only, as the ⋯ menus' is), `MfaBadge` (lime "MFA", or zinc "No MFA"), `EmailBadge` (zinc "Not verified"; lime "Verified" too unless `quiet`, as in the list) and `AdminBadge` (purple); by Events, an event's page and Mail: `DeliveryStatusBadge` (lime Delivered, zinc Pending, pink Retrying, rose Failed) and `DeliveryTime`, `EventTable` (`events.tsx`, with `eventSummary`, `AccountLink`, `WorkspaceLink`, `IpLink`; a deleted record shows its ID alone) and `CursorNav` (Older / Newer, [pagination.md](pagination.md#cursor-lists)); and `AdminActionDialog` (`admin-action-dialog.tsx`: an account-and-access action's confirmation, with a reason and, when the API asks, a code that steps up and retries; [forms-and-actions.md](forms-and-actions.md)), used by Email blocks' Unblock and kept for the actions plan's. The admin chrome is `AdminLayout` (`components/layout/admin-layout.tsx`): the sidebar layout with the app name and an "Admin" badge, the admin pages, a warning `Notice` above every page while its key is being retired, and an account menu saying who is signed in, with Sign out.
- **`mfa`**, shared by account settings, login, the admin door and the admin actions' step-up: `CodeStep` (`code-step.tsx`: a sign-in's code or recovery code step, given its `verify` call and where to go next), `QrCode` (an SVG path, always dark on white, quiet zone included), `MfaScanStep` (the QR code behind `Reveal`; the setup key behind "Use setup key instead", then in a read-only field that starts masked, with Copy (works masked) and an eye to show or mask it inside the field, and "Hide setup key" under it to close the section; and the first code), `CodeField` (an authenticator code in `OtpInput`'s six boxes, `autoSubmit` where it's the form's only field (the scan step, sign-in), or with `recovery` a recovery code in a text field; where either code works, `AppOrRecoveryCode` (with an optional `description` and `autoFocus`) puts "Lost your device? Use a recovery code" under it to switch: account settings' MFA dialogs and the admin dashboard's step-up) and `RecoveryCodeList` (hidden until revealed, with Copy and Download; `onSaved` fires on the first of the three, and the setup's final button waits for it). The page supplies the form, buttons and API calls.
- **`email-verification`**, shared by a sign-up's code page and the verify-email page: `EmailCodeStep` (`email-code-step.tsx`: the heading, "Enter the 6-digit code we sent to …", `CodeField` with autofill from Mail or Messages and `autoSubmit`, full-width Verify, then Send a new code (an inline success notice, since neither page has a `Toaster`) above the page's own links; `verify` goes through `useAuthSubmit`; a 401 or 410 calls `onEnded` with the API's reason, a 409 `onConflict`). Account settings' dialog uses `CodeField` directly.
- **`account`**, shared by account settings, onboarding and the reset link page: `PasswordField`, `NewPasswordFields`, and `DeleteAccountDialog` (password confirmation; a 409 goes to `onLastOwner`).

The workspace switcher (`WorkspaceDropdown` in `application-layout.tsx`) shows the current workspace and lists every workspace newest first, loading more as the list scrolls ([pagination.md](pagination.md#infinite-lists)).
