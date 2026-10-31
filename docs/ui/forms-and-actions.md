# Forms and actions

Where buttons go, which style they take, and in what order, for forms and for one-off actions. **Follow this for every button you add.** If a place doesn't match one of these patterns, ask rather than inventing a new layout.

The short version: **actions sit at the bottom right of what they act on**, the primary action is the rightmost one, and only the sign-in pages use full-width buttons.

## Where the action goes

| Context | Layout | Primary is | Example |
| --- | --- | --- | --- |
| **Settings form** (fields inside a `SettingsSection`) | Below the fields, in `<div className="flex justify-end">`. The form's `ErrorNotice` goes below that row. | rightmost | Save (profile, workspace name) |
| **One-off settings action** (a button with no fields, inside a `SettingsSection`) | Its own `<div className="flex justify-end">` at the bottom of the section's content. | the only button | Change password, Sign out other sessions |
| **Dialog** (`Dialog`, `ConfirmationDialog`) | `DialogActions`, which right-aligns its children. On phones it stacks them full width with the **last child on top**. | **last child** | Cancel · **Save changes**; Back · **Next** |
| **Danger zone** (`SettingsSection variant="danger"`) | One `DangerAction` row per action: the explanation on the left, the red button on the right. On phones the button drops below the text. | the row's button | Leave workspace, Delete workspace, Delete account |
| **Page header** (a page's `Heading` row) | Title on the left; actions on the right of the same row (`flex flex-wrap items-end justify-between gap-4`), wrapping below on phones. | rightmost | **New project** (Projects), Edit project (project page) |
| **Table row** | A ⋯ `Dropdown` in the last column, right-aligned (`TableCell className="w-0 text-right"`). Row actions live in the menu, never as loose buttons. | n/a | Change role, Remove from workspace, Delete |
| **Sign-in pages** (`AuthLayout`: login, register, forgot password, the reset link, onboarding) | **One full-width primary button** (`className="w-full"`) as the form's last control. Links (Forgot password?, Sign up) are text links, not buttons. | the only button | Sign in, Create account, Send reset link, Set new password, Create workspace |

Everything signed in and outside a dialog follows the settings rows: right-aligned, never left-aligned or full-width. The sign-in pages are the only exception, because their single action is the whole page.

## Order and style

- **Primary action last (rightmost)**, solid (`<Button>`, `dark/zinc`). Secondary actions to its left: `plain` for Cancel and Back, `outline` for an alternative that does something (Try again).
- **At most one solid button per group.** A one-off action that isn't the main thing on the page is `outline` (Change password, Sign out other sessions).
- **Red (`color="red"`) only for destructive actions**, and only in a danger zone or as a `ConfirmationDialog`'s confirm button (`variant="danger"`). In a ⋯ menu use `DropdownItem variant="danger"` instead. See [design.md](design.md#confirmations).
- **Labels say what happens**, with a verb: "Save", "Create project", "Delete workspace", "Sign out other sessions". Never "OK", "Submit" or a bare "Delete".
- **Cancel** never has a destructive or primary style. It's disabled while the action runs, as dialogs already do.

## Behaviour (same everywhere)

- **Pending:** run the action through `useAction` and pass `loading={action.loading}` to its button, so a double click submits once ([api.md](api.md#changing-data)). Disable the form's fields while it runs (`disabled={action.loading}`).
- **Nothing to save:** a settings Save is `disabled` until the value differs from what's saved and would be accepted (`disabled={clean === null || clean === saved}`).
- **Submit buttons** are `type="submit"` inside a `<form onSubmit>`, so Enter submits. Other buttons in a form are plain buttons (`Button` defaults to `type="button"`).
- **Errors:** a form's failure is an `ErrorNotice`; a one-off action with no fields reports its failure as `toast.error`. Successes are toasts ([design.md](design.md#toasts)).

## Examples

```tsx
// Settings form: fields, then Save at the bottom right, then the error.
<form onSubmit={submit} className="space-y-4">
  <WorkspaceNameField label="Name" value={name} onChange={setName} disabled={rename.loading} />
  <div className="flex justify-end">
    <Button type="submit" loading={rename.loading} disabled={clean === null || clean === workspace.name}>
      Save
    </Button>
  </div>
  <ErrorNotice error={rename.error} />
</form>

// One-off action in a settings section.
<div className="flex justify-end">
  <Button outline loading={revoke.loading} onClick={revokeOthers}>
    Sign out other sessions
  </Button>
</div>

// Dialog: secondary first, primary last.
<DialogActions>
  <Button plain onClick={close} disabled={save.loading}>Cancel</Button>
  <Button type="submit" loading={save.loading}>Save changes</Button>
</DialogActions>

// Sign-in page: one full-width primary.
<Button type="submit" className="w-full" loading={loading}>Sign in</Button>
```

## Checking your change

Look at the page at phone width and at desktop width (`make dev`) before calling a UI change done. Every action should be at the bottom right of what it acts on, or full width on a sign-in page, with the primary rightmost (or on top when stacked).
