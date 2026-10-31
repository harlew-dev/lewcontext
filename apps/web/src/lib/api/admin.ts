import { url, type Page } from "@/lib/pagination";
import type { Project, ProjectSort } from "@/types/projects";
import { defaultSort as defaultProjectSort } from "@/types/projects";
import type { SignedIn } from "./auth";
import type { MfaSetup, RecoveryCodes } from "./auth";
import type { Http } from "./http";
import { path } from "./paths";
import {
  defaultAdminUserSort,
  defaultAdminWorkspaceSort,
  type AdminCatalogue,
  type AdminDeliveryRow,
  type AdminEvent,
  type AdminEventDetail,
  type AdminMail,
  type AdminSession,
  type AdminUser,
  type AdminUserDetail,
  type AdminUserSort,
  type AdminWorkspace,
  type AdminWorkspaceDetail,
  type AdminWorkspaceSort,
  type CursorPage,
  type DeliveryStatus,
  type EventFilters,
  type MailWindow,
} from "@/types/admin";
// The admin dashboard's API, under /api/admin. Every call carries the key
// (adminHttp), never in a path. The lists take a page, a search and a sort;
// the default sort is left out, so each query has one path.
export type AdminUserIndexQuery = {
  page?: number;
  perPage?: number;
  q?: string;
  sort?: AdminUserSort;
};
export type AdminWorkspaceIndexQuery = {
  page?: number;
  perPage?: number;
  q?: string;
  sort?: AdminWorkspaceSort;
};
// A workspace's projects take the app's list query.
export type AdminProjectIndexQuery = {
  page?: number;
  perPage?: number;
  q?: string;
  status?: Project["status"];
  sort?: ProjectSort;
};
// The Events list's query: its filters and a page size.
export type AdminEventIndexQuery = EventFilters & { perPage?: number };
export type AdminDeliveryIndexQuery = {
  status?: DeliveryStatus;
  listener?: string;
  from?: string;
  to?: string;
  before?: string;
  perPage?: number;
};
// The door's password step: a code next, or (an admin without MFA, inside
// the setup window) setting MFA up.
export type AdminLoginStep = { mfaRequired: true } | { mfaSetupRequired: true };
// Signing in with a code starts the admin session; confirming the setup does
// too, and returns the new recovery codes.
export type AdminSignedIn = Omit<SignedIn, "user"> & AdminSession;
export type AdminSetupDone = AdminSignedIn & RecoveryCodes;
const base = "/api/admin";
// Typed paths (paths.admin).
export const adminRoutes = {
  me: () => path<AdminSession>(`${base}/me`),
  login: () => `${base}/login`,
  loginMfaSetup: () => `${base}/login/mfa/setup`,
  loginMfa: () => `${base}/login/mfa`,
  logout: () => `${base}/logout`,
  users: {
    index: ({ page, perPage, q, sort }: AdminUserIndexQuery = {}) =>
      path<Page<AdminUser>>(
        url(`${base}/users`, {
          page,
          perPage,
          q,
          sort: sort === defaultAdminUserSort ? undefined : sort,
        }),
      ),
    show: (id: string) => path<AdminUserDetail>(`${base}/users/${id}`),
  },
  workspaces: {
    index: ({ page, perPage, q, sort }: AdminWorkspaceIndexQuery = {}) =>
      path<Page<AdminWorkspace>>(
        url(`${base}/workspaces`, {
          page,
          perPage,
          q,
          sort: sort === defaultAdminWorkspaceSort ? undefined : sort,
        }),
      ),
    show: (id: string) =>
      path<AdminWorkspaceDetail>(`${base}/workspaces/${id}`),
  },
  projects: (workspace: string) => ({
    index: ({ page, perPage, q, status, sort }: AdminProjectIndexQuery = {}) =>
      path<Page<Project>>(
        url(`${base}/workspaces/${workspace}/projects`, {
          page,
          perPage,
          q,
          status,
          sort: sort === defaultProjectSort ? undefined : sort,
        }),
      ),
  }),
  events: {
    index: (query: AdminEventIndexQuery = {}) =>
      path<CursorPage<AdminEvent>>(url(`${base}/events`, query)),
    show: (id: string) => path<AdminEventDetail>(`${base}/events/${id}`),
  },
  catalogue: () => path<AdminCatalogue>(`${base}/catalogue`),
  // The Mail summary; 24h is the default, so it's left out.
  mail: (window: MailWindow = "24h") =>
    path<AdminMail>(
      url(`${base}/mail`, { window: window === "24h" ? undefined : window }),
    ),
  deliveries: {
    index: (query: AdminDeliveryIndexQuery = {}) =>
      path<CursorPage<AdminDeliveryRow>>(url(`${base}/deliveries`, query)),
  },
};
export function adminClient(http: Http) {
  return {
    // Loads any typed admin path, for SWR.
    get: http.get,
    me: () => http.get(adminRoutes.me()),
    login: ({ email, password }: { email: string; password: string }) =>
      http.send<AdminLoginStep>("POST", adminRoutes.login(), {
        email,
        password,
      }),
    // A new authenticator secret, for an admin setting MFA up at the door.
    loginMfaSetup: () =>
      http.send<MfaSetup>("POST", adminRoutes.loginMfaSetup(), {}),
    // A code (MFA on), or the setup's first code (answering with the
    // recovery codes). A wrong code is an ApiError with field "code"; a 401
    // means the sign-in ended and the password is needed again.
    loginMfa: <T extends AdminSignedIn = AdminSignedIn>(code: string) =>
      http.send<T>("POST", adminRoutes.loginMfa(), { code }),
    logout: () => http.send<void>("POST", adminRoutes.logout(), {}),
    users: () => ({
      index: (query: AdminUserIndexQuery = {}) =>
        http.get(adminRoutes.users.index(query)),
      show: (id: string) => http.get(adminRoutes.users.show(id)),
    }),
    workspaces: () => ({
      index: (query: AdminWorkspaceIndexQuery = {}) =>
        http.get(adminRoutes.workspaces.index(query)),
      show: (id: string) => http.get(adminRoutes.workspaces.show(id)),
    }),
    projects: (workspace: string) => ({
      index: (query: AdminProjectIndexQuery = {}) =>
        http.get(adminRoutes.projects(workspace).index(query)),
    }),
    events: () => ({
      index: (query: AdminEventIndexQuery = {}) =>
        http.get(adminRoutes.events.index(query)),
      show: (id: string) => http.get(adminRoutes.events.show(id)),
    }),
    catalogue: () => http.get(adminRoutes.catalogue()),
    mail: (window?: MailWindow) => http.get(adminRoutes.mail(window)),
    deliveries: () => ({
      index: (query: AdminDeliveryIndexQuery = {}) =>
        http.get(adminRoutes.deliveries.index(query)),
    }),
  };
}
export type AdminApiClient = ReturnType<typeof adminClient>;
// Whether the door's password step asks for setting MFA up.
export function needsMfaSetup(step: AdminLoginStep): boolean {
  return "mfaSetupRequired" in step;
}
