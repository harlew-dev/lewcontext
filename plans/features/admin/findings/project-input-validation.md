# Findings: project names and descriptions accept control characters

Found while building A9; it isn't part of any admin plan. Nothing has been changed.

## What

`validProject` (`apps/api/internal/httpapi/endpoints/workspace/projects.go:19`) trims a project's name and description and bounds their length. Unlike every other free-text input, it doesn't refuse control characters:

| Input | Refuses control characters |
| --- | --- |
| Workspace name (`workspaces.go:28`) | yes (`unicode.IsControl`) |
| Account name (`me.go:36`) | yes |
| Search terms (`input/search.go:45`) | yes, because PostgreSQL can't hold NUL |
| Admin unblock reason (A9) | yes, apart from line breaks and tabs |
| **Project name and description** | **no** |

## Why it matters

- **A NUL byte (`\u0000`) in a project name or description probably answers 500.** PostgreSQL `text` and `jsonb` can't hold it, so the insert fails as an internal error. That's a generic failure where the user should get a 422 on the field. This was found by reading the code and is likely, but no test has confirmed it.
- **Other control characters** (escape sequences, zero-width or direction-override characters fall under `IsControl` only partly) can be stored and later shown in admin pages and the audit trail. React escapes them, so this isn't XSS, but they can make names confusing.

## Suggested fix (small, within the existing design)

- In `validProject`, refuse control characters in the name, as workspace names do.
- In the description, refuse them apart from `\n`, `\r` and `\t`, as A9's reason does.
- Answer 422 on the field.
- Add an integration test sending `\u0000` in each field.
