# Forms and actions

Where buttons go, which style they take, and in what order, for forms and for one-off actions. **Follow this for every button you add.** If a place doesn't match one of these patterns, ask rather than inventing a new layout.

The short version: **actions sit at the bottom right of what they act on**, the primary action is the rightmost one, and only the sign-in pages use full-width buttons.

## Where the action goes

| Context | Layout | Primary is | Example |
| --- | --- | --- | --- |
| **Settings form** (fields inside a `SettingsSection`) | Below the fields, in `<div className="flex justify-end">`. The form's `ErrorNotice` goes below that row. | rightmost | Save (profile, workspace name) |
| **One-off settings action** (a button with no fields, inside a `SettingsSection`) | Its own `<div className="flex justify-end">` at the bottom of the section's content. | the only button | Change password, Sign out other sessions |
| **Dialog** (`Dialog`, `ConfirmationDialog`) | `DialogActions`, which right-aligns its children. On phones it stacks them full width with the **last child on top**. | **last child** | Cancel · **Save changes**; Back · **Next**; Cancel · **Resend code** (Didn’t receive the email?) |
| **Admin action** (an admin dashboard account-and-access action, from a row's ⋯ menu) | `AdminActionDialog` (`components/app/admin/admin-action-dialog.tsx`): a `ConfirmationDialog` with a required **Reason** (10 to 500 characters, counted under it), and, only when the API answers `stepUp`, a code field below it (authenticator, or a recovery code), focused, its description saying why; confirming then steps up and retries the action. Confirm is disabled ("Fill in required fields") until the reason, and the code when asked, are in. Success is a toast; a failure stays in the dialog, a wrong code on its field (emptied); a 401 goes back to the admin sign-in | the confirm button (`danger` for a destructive action) | Cancel · **Unblock** (Email blocks) |
| **An action on one field** (the field's value has a status with something to do about it) | The status as a `Badge` at the end of the field's `Label`, after a space, so it's read with the field's name; the action as a `TextButton` in the field's `Description` (below the control), opening a `Dialog` whose actions follow the dialog row. Shown only while there's something to do | the dialog's last button | Email address · Not verified, then "We haven’t confirmed this address. Verify email" (account settings) |
| **Danger zone** (`SettingsSection variant="danger"`) | One `DangerAction` row per action: the explanation on the left, the red button on the right. On phones the button drops below the text. | the row's button | Leave workspace, Delete workspace, Delete account |
| **Page header** (a page's `Heading` row) | Title on the left; actions on the right of the same row (`flex flex-wrap items-end justify-between gap-4`), wrapping below on phones. | rightmost | **New project** (Projects), the project page's ⋯ menu |
| **Table row** | A ⋯ `Dropdown` in the last column, right-aligned (`TableCell className="w-0 text-right"`). Row actions live in the menu, never as loose buttons. | n/a | Change role, Remove from workspace, Edit project, Delete project |
| **Sign-in pages** (`AuthLayout`: login, register, forgot password, the reset link, onboarding) | **One full-width primary button** (`className="w-full"`) as the form's last control. Links (Forgot password?, Sign up) are text links, not buttons; Forgot password? sits right under the password input as its `Description`. | the only button | Sign in, Create account, Send reset link, Set new password, Create workspace |

### Back on the sign-in pages

The way back is a **Back** button right under the logo, never a "Back to …" or "Remembered it?" link at the foot of the form: `BackButton` (`components/layout/auth-layout.tsx`), a `plain` button with a left arrow, in line with the logo. With `href` it opens the page before (carrying `?next=`, the invitation state and the typed email as the old links did); with `onClick` it goes back a step on the same page. Links to somewhere else (Sign up, Sign in from the code page, Forgot password?) stay text links.

| Page | Back goes to |
| --- | --- |
| Login | the homepage |
| Login's and the admin door's code step (`CodeStep`) | the password step, the email kept (it was "Sign in as someone else") |
| Register | login, with `?next=` and the invitation (its "Already have an account? Sign in" stays too) |
| A sign-up's code page | register, with the email filled in (it was "Use a different email") |
| Forgot password | login, with the typed email |
| Forgot password's "Check your email" | the form, the email kept, to try again (it was Try again and Back to sign in) |
| The reset link page | login |
| The admin door's MFA setup | the password step (until MFA is on; then the codes lead on to Users) |

The pages that start a flow from an email or gate a session have no Back: the admin door's password step, the invitation and block links, onboarding, verify email (its way out is Sign out), and a sign-up that has ended (its one button is Back to sign up).

Everything signed in and outside a dialog follows the settings rows: right-aligned, never left-aligned or full-width. The sign-in pages are the only exception, because their single action is the whole page.

## Order and style

- **Primary action last (rightmost)**, solid (`<Button>`, `dark/zinc`). Secondary actions to its left: `plain` for Cancel and Back, `outline` for an alternative that does something (Try again).
- **At most one solid button per group.** A one-off action that isn't the main thing on the page is `outline` (Change password, Sign out other sessions).
- **Red (`color="red"`) only for destructive actions**, and only in a danger zone or as a `ConfirmationDialog`'s confirm button (`variant="danger"`). In a ⋯ menu use `DropdownItem variant="danger"` instead. See [design.md](design.md#confirmations). **Sign out is not destructive,** so in the account menus (the app layout's, onboarding's and the admin dashboard's) it's a default `DropdownItem`, after a divider; never red.
- **Labels say what happens**, with a verb: "Save", "Create project", "Delete workspace", "Sign out other sessions". Never "OK", "Submit" or a bare "Delete".
- **Cancel** never has a destructive or primary style. It's disabled while the action runs, as dialogs already do.

## Behaviour (same everywhere)

- **Pending:** run the action through `useAction` and pass `loading={action.loading}` to its button, so a double click submits once ([api.md](api.md#changing-data)). Disable the form's fields while it runs (`disabled={action.loading}`).
- **Nothing to save:** a settings Save is `disabled` until the value differs from what's saved and would be accepted (`disabled={clean === null || clean === saved}`).
- **Say why it's disabled:** a button disabled by the form's state passes `disabledReason`, in one of two wordings: "No changes made" (nothing differs from what's saved) or "Fill in required fields" (something needed is empty or invalid), e.g. `disabledReason={clean === null ? "Fill in required fields" : "No changes made"}`. Not for `loading`, which explains itself.
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
