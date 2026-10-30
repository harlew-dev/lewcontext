import { HomeIcon, Square2StackIcon } from "@heroicons/react/20/solid";
import { publicIdPattern } from "./ids";
// Page URLs, registered with their page files in src/routes.ts (auth/* and
// app/*). Workspace pages live under /app/<wsp_…>; /app itself opens the
// newest workspace (pages/app/index.tsx), and "/" is the public homepage. API
// paths live with their resource in lib/api/*.
export const routes = {
  home: "/",
  app: "/app",
  onboarding: "/app/onboarding",
  settings: "/app/settings",
  dashboard: (workspace: string) => `/app/${workspace}/dashboard`,
  projects: (workspace: string) => `/app/${workspace}/projects`,
  project: (workspace: string, project: string) =>
    `/app/${workspace}/projects/${project}`,
  workspaceSettings: (workspace: string) => `/app/${workspace}/settings`,
  login: "/auth/login",
  register: "/auth/register",
} as const;
// Sidebar items for a workspace. Account & security is in the account menu
// and workspace settings in the workspace menu instead.
export function navigation(workspace: string) {
  return [
    { label: "Overview", href: routes.dashboard(workspace), icon: HomeIcon },
    {
      label: "Projects",
      href: routes.projects(workspace),
      icon: Square2StackIcon,
    },
  ];
}
const wsp = publicIdPattern("wsp");
const prj = publicIdPattern("prj");
// Signed-in pages a sign-in may return to (?next=), matched exactly. Add a
// pattern for each new page under /app. Onboarding isn't one: /app sends
// accounts without a workspace there anyway.
const returnRoutes = [
  /^\/app$/,
  /^\/app\/settings$/,
  new RegExp(`^/app/${wsp}/dashboard$`),
  new RegExp(`^/app/${wsp}/projects$`),
  new RegExp(`^/app/${wsp}/projects/${prj}$`),
  new RegExp(`^/app/${wsp}/settings$`),
];
const origin = "http://lewcontext.invalid";
// A same-site path and query from ?next=, or null. It must start with "/";
// protocol-relative URLs (including "/\host") change the origin and are
// rejected; parsing also resolves "..", so "/app/../auth/login" can't pass.
// It is only ever used as a page navigation, never a request target.
export function returnPath(next: unknown): string | null {
  if (typeof next !== "string" || next.length > 512) return null;
  if (!next.startsWith("/")) return null;
  let url: URL;
  try {
    url = new URL(next, origin);
  } catch {
    return null;
  }
  if (url.origin !== origin) return null;
  if (!returnRoutes.some((route) => route.test(url.pathname))) return null;
  return url.pathname + url.search;
}
// An auth page (login or register) that returns to `next` after signing in,
// when it's an allowed return path.
export function authPath(page: string, next: unknown): string {
  const path = returnPath(next);
  return path ? `${page}?next=${encodeURIComponent(path)}` : page;
}
// The login page, returning to `path` afterwards.
export function loginPath(path: string): string {
  return authPath(routes.login, path);
}
