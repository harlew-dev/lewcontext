# Endpoints

All routes are registered in `apps/api/internal/httpapi/routes.go`. The browser reaches them through the Next proxy at `/api/*`.

| Method | Route | Requires | Success |
| --- | --- | --- | --- |
| GET | `/api/health` | Public | `{"status":"ok"}`, or 503 if the database is down |
| POST | `/api/auth/register` | Origin, JSON, rate limit | 201 `{user, csrfToken}` and a new session cookie |
| POST | `/api/auth/login` | Origin, JSON, rate limit | 200 `{user, csrfToken}` and a rotated session cookie |
| GET | `/api/auth/me` | Session | `{user, csrfToken, expiresAt, signedInAt}` |
| POST | `/api/auth/logout` | Session, CSRF, Origin | 204; session revoked, cookie cleared |
| DELETE | `/api/auth/sessions` | Session, CSRF, Origin | 204; every other session revoked |
| GET | `/api/projects` | Session | One page of your projects ([pagination.md](pagination.md)) |
| GET | `/api/projects/{id}` | Session | Your project, or 404 |
| POST | `/api/projects` | Session, CSRF, Origin | 201 with the created project |
| PATCH | `/api/projects/{id}` | Session, CSRF, Origin | 200 with the updated project, or 404 |

"Origin" means the exact `APP_ORIGIN`, no cross-site fetch metadata, and `Content-Type: application/json` on POST and PATCH. See [auth.md](auth.md).

## Projects

```json
{ "id": 41, "name": "Website redesign", "description": "…", "status": "In progress",
  "color": "violet", "createdAt": "2026-10-30T12:00:00Z", "updatedAt": "2026-10-30T12:00:00Z" }
```

- **Body for POST/PATCH:** `name` (1–100 characters), `description` (≤ 500), `status` (`Planned`, `In progress`, `Completed`), `color` (`violet`, `blue`, `amber`, `emerald`, `rose`). Unknown fields are rejected.
- **PATCH replaces the whole editable representation.** There is no delete endpoint.
- **Ownership:** `user_id` is never accepted from the request or returned. Another user's project ID behaves exactly like a missing one (404).

## Status codes

| Code | Meaning |
| --- | --- |
| 400 | Malformed JSON, ID or query parameters |
| 401 | No session, expired session, or wrong credentials |
| 403 | Bad Origin or CSRF token |
| 404 | Not found or not yours |
| 409 | Email already registered (generic message) |
| 415 | Mutation without JSON content type |
| 422 | Body failed validation |
| 429 | Rate limited; `Retry-After: 900` |
| 503 | Database unavailable, or sign-in concurrency full |

## Adding an endpoint

1. Handler and validation in `httpapi` (one file per resource), query in `store` taking the user ID.
2. Register it in `routes.go`, wrapped in `requireSession` unless it is public.
3. Integration tests for success, validation, 401, and another user's data (404 or empty).
4. Add it to this table.
