import { HomeIcon, Square2StackIcon } from "@heroicons/react/20/solid";
// Page URLs follow the file layout under src/pages (auth/* and app/*).
// Redirects from "/" and the pre-nesting URLs are declared in next.config.mjs.
// API paths live with their resource in lib/api/*.
export const routes = {
  dashboard: "/app/dashboard",
  projects: "/app/projects",
  project: (id: number) => `/app/projects/${id}`,
  settings: "/app/settings",
  login: "/auth/login",
  register: "/auth/register",
} as const;
// Sidebar items. Account & security is in the account menu instead.
export const navigation = [
  { label: "Overview", href: routes.dashboard, icon: HomeIcon },
  { label: "Projects", href: routes.projects, icon: Square2StackIcon },
];
// Signed-in pages a sign-in may return to (?next=), matched exactly. Add a
// pattern for each new page under /app.
const returnRoutes = [
  /^\/app\/dashboard$/,
  /^\/app\/projects$/,
  /^\/app\/projects\/[1-9]\d*$/,
  /^\/app\/settings$/,
];
const origin = "http://forma.invalid";
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
