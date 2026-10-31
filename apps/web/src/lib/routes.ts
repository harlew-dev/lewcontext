import {
  BuildingOffice2Icon,
  EnvelopeIcon,
  HomeIcon,
  QueueListIcon,
  Square2StackIcon,
  UsersIcon,
} from "@heroicons/react/20/solid";
import type { DeliveryPageQuery, EventFilters } from "@/types/admin";
import { publicIdPattern } from "./ids";
import { url } from "./pagination";
// Page URLs, registered with their page files in src/routes.ts (auth/* and
// app/*). Workspace pages live under /app/<wsp_…>; /app itself opens the
// newest workspace (pages/app/index.tsx), and "/" is the public homepage. API
// paths live with their resource in lib/api/*.
export const routes = {
  home: "/",
  app: "/app",
  onboarding: "/app/onboarding",
  // An account that must verify its email first (mustVerify) is sent here
  // from every signed-in page, with ?next= to return to.
  verifyEmail: "/app/verify-email",
  settings: "/app/settings",
  dashboard: (workspace: string) => `/app/${workspace}/dashboard`,
  // Resource pages, grouped by resource with the page actions index, show,
  // create and edit (docs/naming.md). URLs read naturally rather than
  // following the action names.
  projects: {
    index: (workspace: string) => `/app/${workspace}/projects`,
    show: (workspace: string, project: string) =>
      `/app/${workspace}/projects/${project}`,
  },
  workspaces: {
    edit: (workspace: string) => `/app/${workspace}/settings`,
  },
  members: {
    index: (workspace: string) => `/app/${workspace}/settings/members`,
  },
  login: "/auth/login",
  register: "/auth/register",
  // A sign-up's code step, its own page so a reload (a phone switching to
  // the mail app) resumes it from the sign-up's cookie.
  registerVerify: "/auth/register/verify",
  // Asks for a reset link; the link itself is /auth/forgot-password/<token>.
  forgotPassword: "/auth/forgot-password",
  // An invitation's emailed link. Like the reset link, it holds a single-use
  // token, so it can also be a ?next= (AGENTS.md names the exception).
  invitations: {
    show: (token: string) => `/auth/invitations/${token}`,
  },
  // The admin dashboard, at its secret key (ADMIN_ROUTE_KEY, from the URL;
  // lib/admin.tsx). Never a ?next=: signing in always lands on Users.
  admin: {
    login: (key: string) => `/admin/${key}`,
    users: {
      index: (key: string) => `/admin/${key}/users`,
      show: (key: string, user: string) => `/admin/${key}/users/${user}`,
    },
    workspaces: {
      index: (key: string) => `/admin/${key}/workspaces`,
      show: (key: string, workspace: string) =>
        `/admin/${key}/workspaces/${workspace}`,
    },
    // Events, filtered: links from elsewhere open the rows behind them.
    events: {
      index: (key: string, filters: EventFilters = {}) =>
        url(`/admin/${key}/events`, filters),
      show: (key: string, event: string) => `/admin/${key}/events/${event}`,
    },
    // The Mail page lists deliveries (AdminDeliveryIndex), with the summary's
    // window (24h is the default), a status tab and a cursor.
    deliveries: {
      index: (
        key: string,
        { window, status, before }: DeliveryPageQuery = {},
      ) =>
        url(`/admin/${key}/mail`, {
          window: window === "24h" ? undefined : window,
          status,
          before,
        }),
    },
  },
} as const;
// Sidebar items for the admin dashboard (Monitoring comes with its plan).
export function adminNavigation(key: string) {
  return [
    { label: "Users", href: routes.admin.users.index(key), icon: UsersIcon },
    {
      label: "Workspaces",
      href: routes.admin.workspaces.index(key),
      icon: BuildingOffice2Icon,
    },
    {
      label: "Events",
      href: routes.admin.events.index(key),
      icon: QueueListIcon,
    },
    {
      label: "Mail",
      href: routes.admin.deliveries.index(key),
      icon: EnvelopeIcon,
    },
  ];
}
// Sidebar items for a workspace. Account & security is in the account menu
// and workspace settings in the workspace menu instead.
export function navigation(workspace: string) {
  return [
    { label: "Overview", href: routes.dashboard(workspace), icon: HomeIcon },
    {
      label: "Projects",
      href: routes.projects.index(workspace),
      icon: Square2StackIcon,
    },
  ];
}
const wsp = publicIdPattern("wsp");
const prj = publicIdPattern("prj");
// The shape of auth.Token in Go: 43 base64url characters.
export const linkToken = /^[A-Za-z0-9_-]{43}$/;
const invitationLink = /^\/auth\/invitations\/([A-Za-z0-9_-]{43})$/;
// Pages a sign-in may return to (?next=), matched exactly: the signed-in
// pages, and an invitation link. Add a pattern for each new page under /app.
// Onboarding and verify-email aren't ones: /app sends whoever needs them
// there anyway.
const returnRoutes = [
  invitationLink,
  /^\/app$/,
  /^\/app\/settings$/,
  new RegExp(`^/app/${wsp}/dashboard$`),
  new RegExp(`^/app/${wsp}/projects$`),
  new RegExp(`^/app/${wsp}/projects/${prj}$`),
  new RegExp(`^/app/${wsp}/settings$`),
  new RegExp(`^/app/${wsp}/settings/members$`),
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
// The token of an invitation link that `next` returns to, or null: login and
// register show that invitation, and register returns there instead of going
// to onboarding.
export function invitationToken(next: unknown): string | null {
  const path = returnPath(next);
  return path ? (invitationLink.exec(path)?.[1] ?? null) : null;
}
// An auth page (login or register) that returns to `next` after signing in,
// when it's an allowed return path.
export function authPath(page: string, next: unknown): string {
  const path = returnPath(next);
  return path ? `${page}?next=${encodeURIComponent(path)}` : page;
}
// Navigation state the sign-in pages pass each other, rather than the URL, so
// an email stays out of history and server logs: the email typed so far,
// whether the password was just reset, whether the account was just deleted
// (account settings sends you to login afterwards), and whether the visitor
// came from our own invitation page's buttons (the invitation is then
// accepted as soon as they're signed in; a crafted ?next= can't set it).
export type AuthPageState = {
  email?: string;
  passwordReset?: boolean;
  accountDeleted?: boolean;
  invitation?: boolean;
};
export function authPageState(state: unknown): AuthPageState {
  if (typeof state !== "object" || state === null) return {};
  return {
    email:
      "email" in state && typeof state.email === "string"
        ? state.email
        : undefined,
    passwordReset: "passwordReset" in state && state.passwordReset === true,
    accountDeleted: "accountDeleted" in state && state.accountDeleted === true,
    invitation: "invitation" in state && state.invitation === true,
  };
}
// Navigation state for onboarding: the name of the workspace just deleted or
// left, when it was the account's last one (onboarding then says so; a toast
// would be lost as the app layout unmounts).
export type OnboardingState = {
  deletedWorkspace?: string;
  leftWorkspace?: string;
};
export function onboardingState(state: unknown): OnboardingState {
  return {
    deletedWorkspace: stringIn(state, "deletedWorkspace"),
    leftWorkspace: stringIn(state, "leftWorkspace"),
  };
}
// Navigation state for the signed-in layout: the workspace just joined from
// an invitation, or just created (from onboarding or the switcher), which the
// layout announces (onboarding's and the invitation page's own toasts would
// be lost as they unmount).
export type ArrivalState = {
  joinedWorkspace?: string;
  createdWorkspace?: string;
};
export function arrivalState(state: unknown): ArrivalState {
  return {
    joinedWorkspace: stringIn(state, "joinedWorkspace"),
    createdWorkspace: stringIn(state, "createdWorkspace"),
  };
}
// A string field of navigation state, which may be anything.
function stringIn(state: unknown, key: string): string | undefined {
  if (typeof state !== "object" || state === null) return undefined;
  const value: unknown = Reflect.get(state, key);
  return typeof value === "string" ? value : undefined;
}
// The login page saying the email already has an account (a sign-up's code
// found one), returning to `next`. The notice is fixed text the login page
// picks by name; nothing from the URL is shown.
export function accountExistsLoginPath(next: unknown): string {
  const login = authPath(routes.login, next);
  return `${login}${login.includes("?") ? "&" : "?"}notice=account-exists`;
}
// The verify-email page, returning to `path` afterwards.
export function verifyEmailPath(path: string): string {
  return authPath(routes.verifyEmail, path);
}
// The login page, returning to `path` afterwards.
export function loginPath(path: string): string {
  return authPath(routes.login, path);
}
