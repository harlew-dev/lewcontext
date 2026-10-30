import { LayoutDashboard, FolderKanban, ShieldCheck } from "lucide-react";
// Page URLs follow the file layout under src/pages (auth/* and app/*).
// Redirects from "/" and the pre-nesting URLs are declared in next.config.mjs.
export const routes = {
  dashboard: "/app/dashboard",
  projects: "/app/projects",
  settings: "/app/settings",
  login: "/auth/login",
} as const;
export const navigation = [
  { label: "Overview", href: routes.dashboard, icon: LayoutDashboard },
  { label: "Projects", href: routes.projects, icon: FolderKanban },
  { label: "Account & security", href: routes.settings, icon: ShieldCheck },
];
export const apiRoutes = {
  me: "/api/auth/me",
  login: "/api/auth/login",
  register: "/api/auth/register",
  logout: "/api/auth/logout",
  sessions: "/api/auth/sessions",
  projects: "/api/projects",
} as const;
