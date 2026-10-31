import {
  index,
  layout,
  route,
  type RouteConfig,
} from "@react-router/dev/routes";
// The page registry: every URL and the file under src/pages that renders it.
// Pages inside the app layout share the signed-in chrome, which stays mounted
// between them (sidebar state, the sliding current-page indicator).
export default [
  index("pages/home.tsx"),
  route("auth/login", "pages/auth/login.tsx"),
  route("auth/register", "pages/auth/register.tsx"),
  route("auth/forgot-password", "pages/auth/forgot-password.tsx"),
  route(
    "auth/forgot-password/:token",
    "pages/auth/forgot-password/[token].tsx",
  ),
  route("auth/invitations/:token", "pages/auth/invitations/[token].tsx"),
  route("app", "pages/app/index.tsx"),
  route("app/onboarding", "pages/app/onboarding.tsx"),
  layout("pages/app/layout.tsx", [
    route("app/settings", "pages/app/settings.tsx"),
    route("app/:workspace/dashboard", "pages/app/[workspace]/dashboard.tsx"),
    route("app/:workspace/projects", "pages/app/[workspace]/projects.tsx"),
    route(
      "app/:workspace/projects/:project",
      "pages/app/[workspace]/projects/[project].tsx",
    ),
    route("app/:workspace/settings", "pages/app/[workspace]/settings.tsx"),
    route(
      "app/:workspace/settings/members",
      "pages/app/[workspace]/settings/members.tsx",
    ),
  ]),
  // The admin dashboard, at its secret key (docs/api/admin.md). The sign-in
  // page stands alone; the rest share the admin chrome.
  route("admin/:key", "pages/admin/[key]/index.tsx"),
  layout("pages/admin/layout.tsx", [
    route("admin/:key/users", "pages/admin/[key]/users.tsx"),
    route("admin/:key/users/:user", "pages/admin/[key]/users/[user].tsx"),
    route("admin/:key/workspaces", "pages/admin/[key]/workspaces.tsx"),
    route(
      "admin/:key/workspaces/:workspace",
      "pages/admin/[key]/workspaces/[workspace].tsx",
    ),
  ]),
  route("*", "pages/not-found.tsx"),
] satisfies RouteConfig;
