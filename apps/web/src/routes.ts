import {
  index,
  layout,
  route,
  type RouteConfig,
  type RouteConfigEntry,
} from "@react-router/dev/routes";
import { webBuild } from "../web-build.ts";
// The page registry: every URL and the file under src/pages that renders it.
// Pages inside the app layout share the signed-in chrome, which stays mounted
// between them (sidebar state, the sliding current-page indicator).
//
// The web app is built twice (web-build.ts): `react-router build` takes the
// app's pages or, with WEB_BUILD=admin, the admin dashboard's, each ending in
// a not-found page. Type generation and the development servers register
// every page: typegen writes one set of types for the whole program, and
// each dev server runs it (plans/features/implemented/admin/separate-build.md).
const appRoutes = [
  index("pages/home.tsx"),
  route("auth/login", "pages/auth/login.tsx"),
  route("auth/register", "pages/auth/register.tsx"),
  route("auth/register/verify", "pages/auth/register/verify.tsx"),
  route("auth/forgot-password", "pages/auth/forgot-password.tsx"),
  route(
    "auth/forgot-password/:token",
    "pages/auth/forgot-password/[token].tsx",
  ),
  route("auth/invitations/:token", "pages/auth/invitations/[token].tsx"),
  route("auth/email-blocks/:token", "pages/auth/email-blocks/[token].tsx"),
  route("app", "pages/app/index.tsx"),
  route("app/onboarding", "pages/app/onboarding.tsx"),
  route("app/verify-email", "pages/app/verify-email.tsx"),
  layout("pages/app/layout.tsx", [
    route("app/settings", "pages/app/settings.tsx"),
    route("app/:workspace/dashboard", "pages/app/[workspace]/dashboard.tsx"),
    route("app/:workspace/projects", "pages/app/[workspace]/projects.tsx"),
    route(
      "app/:workspace/projects/:project",
      "pages/app/[workspace]/projects/[project].tsx",
    ),
    // General and Members are tabs of one layout (the heading and tabs).
    route(
      "app/:workspace/settings",
      "pages/app/[workspace]/settings/layout.tsx",
      [
        index("pages/app/[workspace]/settings.tsx"),
        route("members", "pages/app/[workspace]/settings/members.tsx"),
      ],
    ),
  ]),
] satisfies RouteConfig;

// The admin dashboard, at its secret key (docs/api/admin.md), in the admin
// build only. The sign-in page stands alone; the rest share the admin chrome.
const adminRoutes = [
  route("admin/:key", "pages/admin/[key]/index.tsx"),
  layout("pages/admin/layout.tsx", [
    route("admin/:key/users", "pages/admin/[key]/users.tsx"),
    route("admin/:key/users/:user", "pages/admin/[key]/users/[user].tsx"),
    route("admin/:key/workspaces", "pages/admin/[key]/workspaces.tsx"),
    route(
      "admin/:key/workspaces/:workspace",
      "pages/admin/[key]/workspaces/[workspace].tsx",
    ),
    route("admin/:key/events", "pages/admin/[key]/events.tsx"),
    route("admin/:key/events/:event", "pages/admin/[key]/events/[event].tsx"),
    route("admin/:key/mail", "pages/admin/[key]/mail.tsx"),
    route("admin/:key/email-blocks", "pages/admin/[key]/email-blocks.tsx"),
    route("admin/:key/admins", "pages/admin/[key]/admins.tsx"),
    route("admin/:key/monitoring", "pages/admin/[key]/monitoring/index.tsx"),
    route(
      "admin/:key/monitoring/security",
      "pages/admin/[key]/monitoring/security.tsx",
    ),
    route(
      "admin/:key/monitoring/limits",
      "pages/admin/[key]/monitoring/limits.tsx",
    ),
    route("admin/:key/monitoring/ips", "pages/admin/[key]/monitoring/ips.tsx"),
    route(
      "admin/:key/monitoring/ips/:ip",
      "pages/admin/[key]/monitoring/ips/[ip].tsx",
    ),
    route(
      "admin/:key/monitoring/accounts",
      "pages/admin/[key]/monitoring/accounts.tsx",
    ),
    route(
      "admin/:key/monitoring/workspaces",
      "pages/admin/[key]/monitoring/workspaces.tsx",
    ),
    route(
      "admin/:key/monitoring/usage",
      "pages/admin/[key]/monitoring/usage.tsx",
    ),
    route(
      "admin/:key/monitoring/integrity",
      "pages/admin/[key]/monitoring/integrity.tsx",
    ),
    route(
      "admin/:key/monitoring/performance",
      "pages/admin/[key]/monitoring/performance.tsx",
    ),
    route(
      "admin/:key/monitoring/errors",
      "pages/admin/[key]/monitoring/errors.tsx",
    ),
    route(
      "admin/:key/monitoring/errors/:error",
      "pages/admin/[key]/monitoring/errors/[error].tsx",
    ),
    route(
      "admin/:key/monitoring/configuration",
      "pages/admin/[key]/monitoring/configuration.tsx",
    ),
  ]),
] satisfies RouteConfig;

// Every URL no other route matches. The admin build's hands links to the
// app's pages over to a full page load (pages/admin/not-found.tsx).
const notFound = route("*", "pages/not-found.tsx");
const adminNotFound = route("*", "pages/admin/not-found.tsx");

// Every page, for typegen and the dev servers; one build's for anything else
// (`react-router build`), so an unexpected command never mixes the two.
function pages(): RouteConfigEntry[] {
  const command = process.argv[2];
  if (command === "typegen" || command === "dev")
    return [...appRoutes, ...adminRoutes, notFound];
  return webBuild() === "admin"
    ? [...adminRoutes, adminNotFound]
    : [...appRoutes, notFound];
}

export default pages() satisfies RouteConfig;
