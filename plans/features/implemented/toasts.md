# Toasts

Status: **agreed and implemented 2026-10-30**

## Decisions

| # | Question | Decision |
| --- | --- | --- |
| 1 | Library | BaseUI `toaster`, styled with `slot()`; no `sonner` dependency |
| 2 | Which messages | Successes become toasts. Errors from a form stay inline next to it; errors from a button-only action (no fields), e.g. "Sign out other sessions" or "Sign out", become toasts too |
| 3 | Placement | Bottom-right from `sm:`, across the bottom on phones |
| 4 | Duration | 5 s by default, configurable per call (`toast.success(message, { duration })`; `Infinity` stays until dismissed), paused while hovered or focused, with a close button |
| 5 | Mount point | Signed-in pages only: the app layout (`pages/app/layout.tsx`) and onboarding |
| 6 | Names | `toast` / `<Toaster />` |

## Why

Success messages are inline `<Notice variant="success">` blocks that stay on the page until something else clears them:

| Where | Message | Today |
| --- | --- | --- |
| `pages/app/settings.tsx` (`SessionControls`) | "All other sessions have been signed out." | `done` state + Notice under the button |
| `pages/app/[workspace]/settings.tsx` (`RenameForm`) | "Workspace renamed." | `done` state + Notice under the form |
| `components/app/projects/use-project-editor.ts` + `project-notices.tsx` | "Project created/updated…" | `notice` state + dismissable Notice on the dashboard, projects and project pages |

A short-lived toast fits these better: the action has finished, nothing needs doing, and the page doesn't need extra `done` state or a Notice that lingers and shifts the layout.

## Proposal

A `components/ui/toast.tsx` with a sonner-like API, built on BaseUI's `toaster` and styled like `Notice`:

```tsx
// Mounted by the signed-in layout (and onboarding).
<Toaster />

// Anywhere:
import { toast } from "@/components/ui/toast";
toast.success("All other sessions have been signed out.");
```

- `Toaster` renders BaseUI's `ToasterContainer` with its `Root`, `ToastBody` and `ToastInnerContainer` slots replaced via `slot()`. BaseUI's own close icon isn't keyboard-reachable (`aria-hidden`, `tabIndex=-1`), so toasts are `closeable={false}` and render our `Button plain` "Dismiss notification", as `Notice` does. The card uses the same zinc surface, ring, radius and shadow as our `Dropdown`/`Dialog` panels, with the `Notice` variant icon (lime `CheckCircleIcon`, red `XCircleIcon`), and a fade/slide on `data-closed`, off under `motion-reduce`.
- `toast.success(message)` / `toast.error(message)` call `toaster.show` with the variant's role; the variant table (icon, role, icon colour) is shared with `Notice` so the two can't drift.
- BaseUI keeps the behaviour: the `aria-live` region (`status` for success, `alert` for error), auto-hide, pausing the timer while hovered or focused, and stacking.
- The three call sites above drop their `done`/`notice` state and call `toast.success(...)`. `ProjectNotices` keeps only the load error.
- Errors from a form stay inline (`Notice variant="error"` next to its fields), as do load failures with Try again (AGENTS rule 8). Errors from button-only actions (sign out other sessions, sign out from the account menu or onboarding) become `toast.error(...)`.
- Tests: a component test that `toast.success` announces the message in a status region and the close button removes it; the e2e assertions for the three messages keep working (they look for the text / `role="status"`).
- Docs: `docs/ui/design.md` (components) gets a short Toasts section.

The `toaster` singleton is module-level, but it only holds the open toasts' message text for a few seconds, never session data. The container is mounted by the signed-in layout, so signing out unmounts it and its toasts with it.
