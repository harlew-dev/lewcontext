import { useMemo } from "react";
import { useOptionalAdmin } from "@/lib/admin";
import { url, type Page } from "@/lib/pagination";
import { eventQuery, eventsUrl } from "@/lib/admin-routes";
import type { Project, ProjectSort } from "@/types/projects";
import { defaultSort as defaultProjectSort } from "@/types/projects";
import type { SignedIn } from "./auth";
import type { MfaSetup, RecoveryCodes } from "./auth";
import { ApiError, httpWith, type Http } from "./http";
import { path } from "./paths";
import {
  defaultAdminEmailBlockSort,
  defaultAdminUserSort,
  defaultAdminWorkspaceSort,
  type AdminAccount,
  type AdminCatalogue,
  type AdminDeliveryRow,
  type AdminEmailBlock,
  type AdminEmailBlockSort,
  type AdminEventDetail,
  type AdminEventPage,
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
// The admin dashboard's API, under /api/admin. It lives only here, never in
// lib/api/index.ts, which every app page loads: the admin build alone may
// hold admin code (plans/features/admin/README.md, rule 7). Every call
// carries the key (adminHttp), never in a path. The lists take a page, a
// search and a sort; the default sort is left out, so each query has one path.
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
// The Events list's query: its filters, a page size, and the summary for
// the charts (the newest page only).
export type AdminEventIndexQuery = EventFilters & {
  perPage?: number;
  summary?: boolean;
};
export type AdminDeliveryIndexQuery = {
  status?: DeliveryStatus;
  listener?: string;
  from?: string;
  to?: string;
  before?: string;
  perPage?: number;
};
export type AdminEmailBlockIndexQuery = {
  page?: number;
  perPage?: number;
  q?: string;
  sort?: AdminEmailBlockSort;
};
// Unblocking an address: the address in the body (never a path), and why.
export type AdminUnblock = { email: string; reason: string };
// The door's password step: a code next, or (an admin without MFA, inside
// the setup window) setting MFA up.
export type AdminLoginStep = { mfaRequired: true } | { mfaSetupRequired: true };
// Signing in with a code starts the admin session; confirming the setup does
// too, and returns the new recovery codes.
export type AdminSignedIn = Omit<SignedIn, "user"> & AdminSession;
export type AdminSetupDone = AdminSignedIn & RecoveryCodes;
const base = "/api/admin";
// Typed paths, for SWR keys: adminPaths.users.index({ page: 2 }).
export const adminPaths = {
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
      path<AdminEventPage>(
        eventsUrl(`${base}/events`, {
          ...eventQuery(query),
          summary: query.summary ? "include" : undefined,
          perPage: query.perPage,
        }),
      ),
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
  // The Admins page: every admin, one list (no paging).
  admins: {
    index: () => path<{ data: AdminAccount[] }>(`${base}/admins`),
  },
  // The signed-in admin's own other sessions.
  sessions: { destroyOthers: () => `${base}/sessions` },
  // The block list, and unblocking (DELETE, the address in the body).
  emailBlocks: {
    index: ({ page, perPage, q, sort }: AdminEmailBlockIndexQuery = {}) =>
      path<Page<AdminEmailBlock>>(
        url(`${base}/email-blocks`, {
          page,
          perPage,
          q,
          sort: sort === defaultAdminEmailBlockSort ? undefined : sort,
        }),
      ),
    destroy: () => `${base}/email-blocks`,
  },
  // A fresh code opens this session's step-up window for the actions.
  stepUp: () => `${base}/step-up`,
};
export function adminClient(http: Http) {
  return {
    // Loads any typed admin path, for SWR.
    get: http.get,
    me: () => http.get(adminPaths.me()),
    login: ({ email, password }: { email: string; password: string }) =>
      http.send<AdminLoginStep>("POST", adminPaths.login(), {
        email,
        password,
      }),
    // A new authenticator secret, for an admin setting MFA up at the door.
    loginMfaSetup: () =>
      http.send<MfaSetup>("POST", adminPaths.loginMfaSetup(), {}),
    // A code (MFA on), or the setup's first code (answering with the
    // recovery codes). A wrong code is an ApiError with field "code"; a 401
    // means the sign-in ended and the password is needed again.
    loginMfa: <T extends AdminSignedIn = AdminSignedIn>(code: string) =>
      http.send<T>("POST", adminPaths.loginMfa(), { code }),
    logout: () => http.send<void>("POST", adminPaths.logout(), {}),
    users: () => ({
      index: (query: AdminUserIndexQuery = {}) =>
        http.get(adminPaths.users.index(query)),
      show: (id: string) => http.get(adminPaths.users.show(id)),
    }),
    workspaces: () => ({
      index: (query: AdminWorkspaceIndexQuery = {}) =>
        http.get(adminPaths.workspaces.index(query)),
      show: (id: string) => http.get(adminPaths.workspaces.show(id)),
    }),
    projects: (workspace: string) => ({
      index: (query: AdminProjectIndexQuery = {}) =>
        http.get(adminPaths.projects(workspace).index(query)),
    }),
    events: () => ({
      index: (query: AdminEventIndexQuery = {}) =>
        http.get(adminPaths.events.index(query)),
      show: (id: string) => http.get(adminPaths.events.show(id)),
    }),
    catalogue: () => http.get(adminPaths.catalogue()),
    mail: (window?: MailWindow) => http.get(adminPaths.mail(window)),
    deliveries: () => ({
      index: (query: AdminDeliveryIndexQuery = {}) =>
        http.get(adminPaths.deliveries.index(query)),
    }),
    admins: () => ({ index: () => http.get(adminPaths.admins.index()) }),
    // Signs out every other admin session of yours, keeping this one.
    sessions: () => ({
      destroyOthers: () =>
        http.send<void>("DELETE", adminPaths.sessions.destroyOthers()),
    }),
    emailBlocks: () => ({
      index: (query: AdminEmailBlockIndexQuery = {}) =>
        http.get(adminPaths.emailBlocks.index(query)),
      // Operators only, inside the step-up window (needsStepUp otherwise).
      // A 404 means the address isn't blocked (any more).
      destroy: ({ email, reason }: AdminUnblock) =>
        http.send<void>("DELETE", adminPaths.emailBlocks.destroy(), {
          email,
          reason,
        }),
    }),
    // An authenticator or recovery code: opens this admin session's 5-minute
    // window for actions. A wrong code is an ApiError with field "code".
    stepUp: (code: string) =>
      http.send<void>("POST", adminPaths.stepUp(), { code }),
  };
}
export type AdminApiClient = ReturnType<typeof adminClient>;
// The header the API sends with every answer to a session made under the
// key being retired (ADMIN_ROUTE_KEY_PREVIOUS): "previous".
export const adminKeyStatusHeader = "X-Admin-Key-Status";
// Whether a response came under the key being retired.
export function previousKeyIn(response: Response): boolean {
  return response.headers.get(adminKeyStatusHeader) === "previous";
}
// The admin dashboard's requests: the same-origin /api/admin with the admin
// cookie, the ADMIN_ROUTE_KEY from the page's URL in X-Admin-Key on every
// call (without it the API answers as an unknown route), and the admin
// session's CSRF token on mutations when given. onPreviousKey is called
// whenever an answer says the key is being retired.
export function adminHttp(
  adminKey: string,
  csrfToken?: string,
  onPreviousKey?: () => void,
): Http {
  return httpWith({
    base: "",
    headers: {
      "X-Admin-Key": adminKey,
      ...(csrfToken ? { "X-CSRF-Token": csrfToken } : {}),
    },
    credentials: "same-origin",
    timeout: 12000,
    onResponse: (response) => {
      if (onPreviousKey && previousKeyIn(response)) onPreviousKey();
    },
  });
}
// The admin dashboard's API, for components under the admin pages
// (AdminProvider): every call sends the key from the page's URL, and
// mutations the admin session's CSRF token. It never uses the app session.
//   const api = useAdminApi();
//   await api.logout();
export function useAdminApi(): AdminApiClient {
  const admin = useOptionalAdmin();
  if (!admin) throw new Error("useAdminApi() needs an AdminProvider");
  const { adminKey, retiring } = admin;
  const csrfToken = admin.session?.csrfToken;
  return useMemo(
    () => adminClient(adminHttp(adminKey, csrfToken, retiring)),
    [adminKey, csrfToken, retiring],
  );
}
// The admin API for the admin pages' route loaders: reads only, with the key
// from the route's params.
export function loaderAdminApi(
  adminKey: string,
  onPreviousKey?: () => void,
): AdminApiClient {
  return adminClient(adminHttp(adminKey, undefined, onPreviousKey));
}
// Whether an action was refused for want of a fresh code (403 with
// stepUp): ask for one, step up, and try again (AdminActionDialog).
export function needsStepUp(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    error.status === 403 &&
    error.details.stepUp === true
  );
}
// Whether the door's password step asks for setting MFA up.
export function needsMfaSetup(step: AdminLoginStep): boolean {
  return "mfaSetupRequired" in step;
}
