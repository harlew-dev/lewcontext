# Collapsible danger zones

Status: **implemented 2026-11-02.** Built differently from the plan: a danger zone has no description and no Show/Hide word; its header is the red title with a ghost chevron button at the far right, and the panel fades from the card background while it opens and closes. `collapsible` is not a prop: `variant="danger"` is always collapsed.

## Why

Every danger zone shows its destructive buttons all the time: **Delete account**, **Leave workspace**, **Delete workspace**, and on the admin pages **Sign out everywhere**, **Reset two-factor authentication**, **Suspend account** and **Revoke pending invitations**. They sit in a red panel at the bottom of a page you open for something else. Collapsing them keeps the page quiet and puts one deliberate click between you and the red buttons. It's a convenience, not a safeguard: each action still asks for its own confirmation, password, code or reason.

[design.md](../../../docs/ui/design.md#component-apis-componentsui) and [forms-and-actions.md](../../../docs/ui/forms-and-actions.md#where-the-action-goes) already say the account and workspace danger zones start collapsed behind a chevron, but `SettingsSection` has no `collapsible` prop, so nothing is built. This plan builds it, on Base UI instead of the hand-written grid-rows version those docs describe, and updates the docs to match.

## Design

The danger zone's header row keeps its title (red) and description on the left. At the **far right of the same row** is a ghost button (`Button plain`) reading **Show**, with a chevron, that opens the red panel **below** the header. Open, it reads **Hide** and the chevron turns up. The panel's height animates open and shut.

```
Danger zone                                              [ Show  v ]
Deleting your account takes effect at once and can’t be undone.
                                                  (collapsed: nothing below)

Danger zone                                              [ Hide  ^ ]
Deleting your account takes effect at once and can’t be undone.
┌──────────────────────────────────────────────────────────────────┐
│ Delete account … Deletes your account …         [ Delete account ]│
└──────────────────────────────────────────────────────────────────┘
```

- **Base UI parts.** `Collapsible.Root` is the `<section>`, `Collapsible.Trigger` renders our `Button plain` (through `render`, as the Base UI migration does for every control), and `Collapsible.Panel` holds the red panel. Trigger and panel get `aria-expanded`, `aria-controls` and `id` from Base UI.
- **The button.** Visible text **Show** / **Hide**, with the section's title as a screen-reader suffix (`<span className="sr-only"> danger zone</span>`), so the accessible name is "Show danger zone" / "Hide danger zone" ("Show actions" on the admin pages) and still contains the visible word. The chevron is Heroicons' `ChevronDownIcon` (`16/solid`, `data-slot="icon"`), rotated by `group-data-panel-open:rotate-180`, which Base UI sets on the trigger. `plain` is the established ghost style ([forms-and-actions.md](../../../docs/ui/forms-and-actions.md#order-and-style)). It doesn't loading-spin, so no `loading` or `disabledReason`.
- **Layout.** The header is `flex items-start justify-between gap-4`; the title block is `min-w-0`, the button `shrink-0` with a small negative top margin so its text lines up with the title's first line at both heights (36 px desktop, 44 px phone). The description wraps in the left column only.
- **Spacing.** Today the section is a grid with `gap-y-6` between header and panel. Collapsed, that gap would leave 24 px of dead space under the header, so a collapsible section drops the grid gap and the panel carries its own `pt-6` **inside** the animated element, collapsing with it.
- **Animation.** `Collapsible.Panel` animates its height with Base UI's `--collapsible-panel-height`, in the same 200 ms ease-in-out the navbar and sidebar use:

  ```tsx
  <Collapsible.Panel
    keepMounted
    className="h-(--collapsible-panel-height) overflow-hidden transition-[height] duration-200 ease-in-out data-ending-style:h-0 data-starting-style:h-0 motion-reduce:transition-none"
  >
    <div className="pt-6">{redPanel}</div>
  </Collapsible.Panel>
  ```

  Reduced motion opens and closes at once. The red panel's `rounded-xl border` is the panel's full width, so `overflow-hidden` clips nothing; the buttons inside are 16 px from its edge, more than their 2 px outline plus 2 px offset. Both are checked at phone width.
- **Closed means out of reach.** With `keepMounted`, Base UI hides the closed panel with the `hidden` attribute: its buttons are out of the tab order and the accessibility tree. The panel isn't `hiddenUntilFound`: Ctrl+F must not open a danger zone.
- **Always starts closed**, on every visit and every workspace (the workspace danger zone is already `key={workspace.id}`). Nothing is remembered in storage or the URL.
- **The description stays visible when collapsed.** It carries the consequences ("Deleting your account takes effect at once and can’t be undone."), which [design.md](../../../docs/ui/design.md#descriptions) says to keep for danger zones, so the collapsed zone still warns.

### Code

```tsx
// components/ui/settings-section.tsx (after the Base UI migration)
import { Collapsible } from "@base-ui/react/collapsible";
import { ChevronDownIcon } from "@heroicons/react/16/solid";

export function SettingsSection({
  title, description, variant = "default", stacked = false,
  collapsible = false, children,
}: {
  title: string;
  description?: string;
  variant?: "default" | "danger";
  stacked?: boolean;
  // The content starts hidden behind a Show button at the title's right.
  collapsible?: boolean;
  children: ReactNode;
}) {
  const danger = variant === "danger";
  const heading = (
    <div className="min-w-0 space-y-1">
      <Subheading>{danger ? <span className="text-red-600 dark:text-red-400">{title}</span> : title}</Subheading>
      {description && <Text>{description}</Text>}
    </div>
  );
  const content = danger ? <div className="divide-y … rounded-xl border …">{children}</div> : children;
  if (!collapsible)
    return (
      <section className={cx("grid gap-x-8 gap-y-6", !(stacked || danger) && "sm:grid-cols-2")}>
        {heading}
        {content}
      </section>
    );
  return (
    <Collapsible.Root render={<section />}>
      <div className="flex items-start justify-between gap-4">
        {heading}
        <Collapsible.Trigger
          render={(props, state) => (
            <Button plain {...props} className="group -mt-1.5 shrink-0">
              {state.open ? "Hide" : "Show"}
              <span className="sr-only"> {title.toLowerCase()}</span>
              <ChevronDownIcon className="transition-transform duration-200 group-data-panel-open:rotate-180 motion-reduce:transition-none" />
            </Button>
          )}
        />
      </div>
      <Collapsible.Panel keepMounted className="…as above…">
        <div className="pt-6">{content}</div>
      </Collapsible.Panel>
    </Collapsible.Root>
  );
}
```

```tsx
// the four call sites each add `collapsible`, e.g. pages/app/[workspace]/settings/index.tsx
<SettingsSection
  title="Danger zone"
  description="These take effect at once and can’t be undone."
  variant="danger"
  collapsible
>
```

`DangerAction`, the dialogs and the actions' own code don't change.

### Where it applies

| Page | File | Title | Contents |
| --- | --- | --- | --- |
| Account settings | `pages/app/settings/index.tsx` | Danger zone | Delete account |
| Workspace settings | `pages/app/[workspace]/settings/index.tsx` | Danger zone | Leave workspace, Delete workspace |
| Admin user page | `pages/admin/[key]/users/[user].tsx` | Actions | Sign out everywhere, Reset two-factor authentication, Suspend / Unsuspend account |
| Admin workspace page | `pages/admin/[key]/workspaces/[workspace].tsx` | Actions | Revoke pending invitations |

Not collapsed: `ConfirmationDialog`s, `DropdownItem variant="danger"` in ⋯ menus (members, invitations, projects, email blocks, IP blocks, admins), and onboarding's account menu. They aren't danger zones.

### Why `keepMounted`

Base UI unmounts a closed panel by default. Here it stays mounted:

- **Data loads once.** Account settings' `DeleteAccountControls` reads the workspaces you're the only owner of (`stuck`) as the page loads, so the **Delete account** description and its last-owner warning are right the moment you open the zone. Unmounted, that fetch would start on open and the row would flash its generic text.
- **Dialog state survives.** Each action owns its `ConfirmationDialog` and `useOpenCount` key inside the panel; they're never unmounted mid-exit.
- **Nothing changes for the server or the API:** the same requests happen as today.

## Docs

The docs already promise `collapsible`; they get the new details and the Base UI wording.

- **`docs/ui/design.md`:** the `settings-section.tsx` row: `collapsible` starts the content hidden behind a ghost **Show** / **Hide** button with a chevron at the title's far right, on Base UI's `Collapsible` (height animation from `--collapsible-panel-height`, closed panel `hidden` but mounted); the description stays visible; all four danger zones. Add `Collapsible` to the Base UI part list if that doc lists parts.
- **`docs/ui/forms-and-actions.md`:** the **Danger zone** row ("behind a plain chevron at the title's right") becomes the ghost **Show** / **Hide** button, and adds the admin **Actions** zones; the admin action row's "danger zone titled **Actions**" gains "collapsed".
- **`docs/ui/base-ui.md`:** `Collapsible` joins "What we use" (our `SettingsSection collapsible`).
- **`docs/api/admin.md`** (the user and workspace page descriptions) and **`docs/ui/pages.md`** (the settings and admin rows): "ends with an **Actions** danger zone" → "…a collapsed **Actions** danger zone".
- **`docs/ui/testing.md`:** the e2e coverage line gets "opening the danger zone".
- This plan moves to `plans/features/implemented/` with its `Status:` and any differences noted.

## Security risks and how they're avoided

None new. No API, auth, session, rate-limit, route or data change. **Collapsing isn't a security control:** the buttons exist in the DOM while closed (`hidden`), and every action keeps its own confirmation, type-to-confirm name, password, step-up code and reason; the docs say "a convenience, not a safeguard" so nobody leans on it. Open or closed isn't stored, logged or put in a URL.

## Tests

- **Vitest, new `components/ui/settings-section.test.tsx`:** a collapsible section starts closed (`aria-expanded="false"`, its buttons not found by role); **Show** opens it (`aria-expanded="true"`, buttons found, the label is **Hide**); Enter and Space on the trigger toggle it; the accessible name is "Show danger zone" / "Hide danger zone", and "Show actions" for **Actions**; the description is visible when closed; a section without `collapsible` is unchanged (no trigger, content always there); `DangerAction` renders its text and button.
- **Vitest, updated:** `workspace-settings.test.tsx`, `pages/app/settings/index.test.tsx` and `pages/admin/actions.test.tsx` click **Show** before reaching a danger button. **Absence assertions** (`queryByRole("button", { name: "Leave workspace" })` for the only owner, **Suspend account** for another operator's view) must open the zone first: a closed zone hides every button from `queryByRole`, so those assertions would pass whether or not the button rendered. A shared `openDangerZone()` helper keeps them short.
- **Playwright:** `workspace.spec.ts` (Delete workspace / Leave workspace / Delete account) and `admin-actions.spec.ts` (Suspend / Unsuspend, and the `user-page-actions` screenshot) open the zone first through a shared `openDangerZone(page)` helper; one assertion that the buttons aren't visible, and Tab skips them, before opening.
- **Manual:** phone and desktop widths, light and dark, reduced motion (no animation), keyboard only (Tab to **Show**, Enter, Tab into the first button, Shift+Tab back, Enter to hide), screen reader name and state, and the focus outline of the buttons at the panel's edges ([forms-and-actions.md](../../../docs/ui/forms-and-actions.md#checking-your-change)).
- `make test`, `make check`, `make test-e2e E2E_PORT=auto`, `make build`.

## Decisions

Each needs your confirmation before code.

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | The button | **Ghost `Button plain` reading Show / Hide with a chevron**, at the far right of the header row. The docs said a plain chevron alone; the word makes the state and the action clear without hovering |
| 2 | Closed panel | **`keepMounted`**: data loads once and dialog state is safe ([why](#why-keepmounted)). The alternative, unmounting, saves nothing here and makes the account's last-owner text flash on open |
| 3 | The description | **Stays visible when collapsed**, so the zone still says what's in it |
| 4 | Which zones | **All four** ([the table](#where-it-applies)), as you asked. The admin **Actions** zones are the main thing on those pages, so a page can pass no `collapsible` if you'd rather they stay open |
| 5 | Remembering open state | **No**: it starts closed every time, never stored. A `#danger-zone` link that opens it (the last-owner dialog links to workspace settings) is a possible later addition |
| 6 | Where the code lives | **Inside `SettingsSection`** (the documented `collapsible` prop), not a new component: only danger zones use it today. If another page needs a collapsible, `Collapsible` can move to `components/ui/collapsible.tsx` then |

## Not included

- Collapsing anything else: `SettingsSection`s that aren't danger zones, the Events filters, the sidebar.
- Opening the zone from a link or the URL, or remembering it ([decision 5](#decisions)).
- Changing any action, dialog, confirmation, permission or copy.
- A hand-written version before the Base UI migration: this waits for [base-ui.md](base-ui.md).
