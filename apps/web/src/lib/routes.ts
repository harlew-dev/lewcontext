import {
  HomeIcon,
  ShieldCheckIcon,
  Square2StackIcon,
} from "@heroicons/react/20/solid";
// Page URLs follow the file layout under src/pages (auth/* and app/*).
// Redirects from "/" and the pre-nesting URLs are declared in next.config.mjs.
export const routes = {
  dashboard: "/app/dashboard",
  projects: "/app/projects",
  project: (id: number) => `/app/projects/${id}`,
  settings: "/app/settings",
  login: "/auth/login",
  register: "/auth/register",
} as const;
export const navigation = [
  { label: "Overview", href: routes.dashboard, icon: HomeIcon },
  { label: "Projects", href: routes.projects, icon: Square2StackIcon },
  { label: "Account & security", href: routes.settings, icon: ShieldCheckIcon },
];
export const apiRoutes = {
  me: "/api/auth/me",
  login: "/api/auth/login",
  register: "/api/auth/register",
  logout: "/api/auth/logout",
  sessions: "/api/auth/sessions",
  projects: "/api/projects",
  project: (id: number) => `/api/projects/${id}`,
} as const;
