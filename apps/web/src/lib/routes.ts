import { LayoutDashboard, FolderKanban, ShieldCheck } from "lucide-react";
export const routes = {
  overview: "/",
  projects: "/projects",
  settings: "/settings",
  login: "/login",
} as const;
export const navigation = [
  { label: "Overview", href: routes.overview, icon: LayoutDashboard },
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
