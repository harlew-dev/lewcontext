# Verification record

Verified on 2026-10-30 against the supplied local PostgreSQL database. The browser suite ran against the optimized Next production build and compiled Go API. Application environment remained development for local HTTP cookies.

| Check | Result |
| --- | --- |
| Go unit + real PostgreSQL integration tests, race detector | Passed |
| Go vet and gofmt | Passed |
| TypeScript strict checking | Passed |
| ESLint with zero warnings allowed | Passed |
| Prettier check | Passed |
| Vitest / React Testing Library | 6 tests passed |
| Chromium end-to-end suite | 2 tests passed |
| Go executable build | Passed |
| Next optimized production build | Passed; protected pages and login render on demand |
| npm audit, including development tools | 0 vulnerabilities |
| govulncheck | 0 reachable or imported-package vulnerabilities |
| Browser console on final dashboard | 0 errors, 0 warnings |
| API readiness and web sign-in page | HTTP 200 |
| Test cleanup | No leftover E2E accounts |

The integration suite verifies migration idempotency, hashed session storage, cookie flags, unauthenticated rejection, CSRF enforcement, user isolation, project updates, invalid IDs, wrong-password rejection, session rotation, logout, other-session revocation, idle/absolute expiry, and auth throttling. Tests use temporary schemas and leave application data alone.

The browser suite verifies signup, empty workspace, project creation, spinner/disabled submit under a deliberately delayed request, editing status, filters, search, server-rendered project HTML, CSP and cache headers, projects navigation, failed-request data retention and Retry, session revocation, 390px layout, sign-out, failed login, and successful login with persisted data.

Issues found during development were fixed before this final run: shared buttons now explicitly disable while loading; the status selector uses an accessible labeled combobox; mobile navigation icons have accessible names; sign-in receives a request-specific CSP nonce; Styletron overrides avoid conflicting shorthand styles.

Non-failing upstream notices:

- Next 16 supports React 18 but warns that support will be removed in Next 17. This project uses React 18 for BaseUI compatibility; coordinate React/BaseUI upgrades before moving to Next 17.
- govulncheck reports module-only GO-2026-5932 for the unmaintained OpenPGP package inside `golang.org/x/crypto`. This app does not import OpenPGP. Its imported Argon2id implementation has no reported vulnerability; no upstream fix is listed for that unrelated module-level advisory.

Desktop and mobile previews are saved in the user's Artifacts panel. Setup and repeatable commands are in `docs/testing.md` and the root README. Public deployment, TLS infrastructure, email verification/recovery, and MFA/OIDC are not part of this local demo.
