import type { Sort } from "@/lib/pagination";
import type { User } from "@/lib/types";
import type { Role } from "./workspaces";
// What the admin dashboard shows (docs/api/admin.md): every account and every
// workspace, the audit trail and the mail queue.

// What an admin may do (docs/api/admin.md), least access first: a viewer
// reads every admin page, an operator may also act. Set from the command
// line only; the API refuses an action to a role without it (403), and the
// pages hide what the role can't do (adminCan).
export const adminRoles = ["viewer", "operator"] as const;
export type AdminRole = (typeof adminRoles)[number];
// The signed-in admin's session, as /api/admin/me answers.
export type AdminSession = {
  user: User;
  role: AdminRole;
  csrfToken: string;
  expiresAt: string;
};
// An admin as the Admins page lists them: their live admin sessions,
// and lastSignedInAt, their newest admin sign-in still in the audit trail
// (events are kept 90 days by default).
export type AdminAccountSession = {
  createdAt: string;
  lastSeenAt: string;
  ip: string;
  userAgent: string;
  // The session asking.
  current: boolean;
  // Made under the key being retired (ADMIN_ROUTE_KEY_PREVIOUS).
  previousKey: boolean;
};
export type AdminAccount = {
  user: AdminPerson;
  role: AdminRole;
  mfaEnabled: boolean;
  // While set and ahead, they may still set MFA up at the door.
  mfaSetupUntil: string | null;
  grantedAt: string;
  lastSignedInAt: string | null;
  sessions: AdminAccountSession[];
};
export type AdminUser = {
  id: string;
  name: string;
  email: string;
  createdAt: string;
  // Null for an account that never verified its email.
  emailVerifiedAt: string | null;
  mfaEnabled: boolean;
  admin: boolean;
  workspaceCount: number;
  lastSignedInAt: string | null;
  // An admin suspended it: it can't sign in (AdminUserDetail.suspension
  // says why).
  suspended: boolean;
};
export type AdminMembership = {
  workspace: { id: string; name: string };
  role: Role;
  addedAt: string;
};
// Why an account is suspended, by whom (null from the command line, or once
// that admin's account is gone) and since when. Shown to admins only.
export type AdminSuspension = {
  reason: string;
  suspendedBy: AdminPerson | null;
  suspendedAt: string;
};
export type AdminUserDetail = AdminUser & {
  memberships: AdminMembership[];
  sessionCount: number;
  suspension: AdminSuspension | null;
};
export type AdminPerson = { id: string; name: string; email: string };
export type AdminWorkspace = {
  id: string;
  name: string;
  createdAt: string;
  memberCount: number;
  projectCount: number;
  // Up to three, oldest first.
  owners: AdminPerson[];
};
export type AdminMember = AdminPerson & { role: Role; addedAt: string };
export type AdminWorkspaceDetail = AdminWorkspace & {
  members: AdminMember[];
  // Live invitations: what Revoke pending invitations would revoke.
  pendingInvitations: number;
};
// Sorts the lists accept; "-field" sorts descending. Mirror adminUserSorts
// and adminWorkspaceSorts in apps/api/internal/store/admin_views.go.
export const adminUserSortable = ["created", "name", "email"] as const;
export type AdminUserSort = Sort<(typeof adminUserSortable)[number]>;
export const defaultAdminUserSort: AdminUserSort = "-created";
export const adminWorkspaceSortable = ["created", "name"] as const;
export type AdminWorkspaceSort = Sort<(typeof adminWorkspaceSortable)[number]>;
export const defaultAdminWorkspaceSort: AdminWorkspaceSort = "-created";
// A blocked address as the Email blocks page lists it: when it was blocked,
// and when the newest block link sent to it stops working (null when none
// works any more, so only an admin or the operator can unblock it).
export type AdminEmailBlock = {
  email: string;
  createdAt: string;
  linkExpiresAt: string | null;
};
// Mirrors adminEmailBlockSorts in apps/api/internal/store/admin_email_blocks.go.
export const adminEmailBlockSortable = ["created", "email"] as const;
export type AdminEmailBlockSort = Sort<
  (typeof adminEmailBlockSortable)[number]
>;
export const defaultAdminEmailBlockSort: AdminEmailBlockSort = "-created";
// A list page's state, kept in its URL.
export type AdminListQuery<S> = { page: number; q: string; sort: S };
// A client address or range every server refuses (403) until expiresAt,
// as CIDR ("203.0.113.7/32"); createdBy is null once that admin's account
// is gone.
export type AdminIPBlock = {
  id: string;
  network: string;
  reason: string;
  createdBy: AdminPerson | null;
  createdAt: string;
  expiresAt: string;
};
// How long a new block lasts: at most 30 days. Mirrors ipBlockLifetimes in
// apps/api/internal/httpapi/endpoints/admin/ip_blocks.go.
export const ipBlockLifetimes = ["1h", "24h", "7d", "30d"] as const;
export type IPBlockLifetime = (typeof ipBlockLifetimes)[number];

// An account or workspace named inside an event: name (and an account's
// email) are missing once it's deleted, leaving the ID from the payload.
export type AdminRef = { id: string; name?: string; email?: string };
// An event as the Events list shows it. The payload is as stored: each
// event's own fields (docs/api/events.md). `failed`: it records a failed
// check (a wrong password, a refused code, …).
export type AdminEvent = {
  id: string;
  name: string;
  createdAt: string;
  failed: boolean;
  actor: AdminRef | null;
  user: AdminRef | null;
  workspace: AdminRef | null;
  ip: string | null;
  payload: Record<string, unknown>;
};
// The Events page's time ranges, resolved by the API in UTC; "24h" is the
// default, and "all" lists rows with no charts.
export const eventWindows = [
  "1h",
  "24h",
  "today",
  "yesterday",
  "7d",
  "all",
] as const;
export type EventWindow = (typeof eventWindows)[number];
export const defaultEventWindow: EventWindow = "24h";
// The Events list's filters (docs/api/admin.md), as in its page URL and its
// API query: name tokens (catalogue names and group IDs such as "auth.*" or
// "admin-reads", written as one comma-separated `name`), failed checks
// only, an account, a workspace, an email, an IP or range, a relative
// window or RFC 3339 times (never both), and the cursor (rows older than
// that event).
export type EventFilters = {
  names?: string[];
  outcome?: "failed";
  account?: string;
  workspace?: string;
  email?: string;
  ip?: string;
  window?: EventWindow;
  from?: string;
  to?: string;
  before?: string;
};
// A cursor page: rows older than the cursor, and the next one's cursor.
export type CursorPage<T> = { data: T[]; next: string | null };
// The kinds of failed check, in the failures chart's order, and how the
// page names them (mirrors FailureKinds in internal/store).
export const failureKinds = [
  "sign_in",
  "second_factor",
  "password",
  "sign_up_code",
  "admin_door",
] as const;
export type FailureKind = (typeof failureKinds)[number];
export const failureLabels: Record<FailureKind, string> = {
  sign_in: "Sign-in",
  second_factor: "Second factor",
  password: "Password checks",
  sign_up_code: "Sign-up codes",
  admin_door: "Admin door",
};
// One column of a summary chart: its start, a count per series.
export type SummaryBucket = { start: string; counts: number[] };
// The charts' numbers, served with the newest page (summary=include), or
// why they weren't computed: a window over 7 days ("too_wide"), past their
// deadline ("too_slow"), or two summaries already running ("busy").
export type EventSummary =
  | {
      status: "ok";
      bucketSeconds: number;
      total: number;
      // Series are group IDs ("auth.*") or, inside one group, names; then
      // "other".
      stackedBy: "group" | "name";
      series: string[];
      buckets: SummaryBucket[];
      topNames: { name: string; count: number }[];
      // Null when no picked event can record a failed check.
      failures: {
        total: number;
        series: FailureKind[];
        buckets: SummaryBucket[];
      } | null;
      limitsReached: number;
      signInAttempts: number;
    }
  | { status: "too_wide" | "too_slow" | "busy" };
// An Events page: the rows and cursor, the window the API resolved (null
// for all time; `from` null for a range with no start), the account and
// workspace filters by name, and the summary when asked for.
export type AdminEventPage = CursorPage<AdminEvent> & {
  window: { from: string | null; to: string } | null;
  refs: { account: AdminRef | null; workspace: AdminRef | null };
  summary?: EventSummary;
};
// A group of event names, from the catalogue: "auth.*" holds sub-groups
// ("auth.mfa.*") and single names; "admin-reads" the admin reads.
export type EventGroup = {
  id: string;
  label: string;
  names: string[];
  groups: EventGroup[];
};
export const deliveryStatuses = [
  "failed",
  "retrying",
  "pending",
  "delivered",
] as const;
export type DeliveryStatus = (typeof deliveryStatuses)[number];
export type AdminDelivery = {
  listener: string;
  status: DeliveryStatus;
  attempts: number;
  nextAttemptAt: string | null;
  deliveredAt: string | null;
  failedAt: string | null;
  lastError: string;
};
export type AdminEventDetail = AdminEvent & {
  userAgent: string;
  deliveries: AdminDelivery[];
  related: AdminEvent[];
};
// A delivery in the Mail page's list; `to` is the event's email, if any.
export type AdminDeliveryRow = AdminDelivery & {
  event: { id: string; name: string; createdAt: string };
  to: string | null;
};
// The filters' names, from GET /api/admin/catalogue: every event name in
// catalogue order, and their groups.
export type AdminCatalogue = {
  events: string[];
  eventGroups: EventGroup[];
  limits: string[];
  listeners: string[];
};
export const mailWindows = ["24h", "7d"] as const;
export type MailWindow = (typeof mailWindows)[number];
// The Mail page's state in its URL: the summary's window, the deliveries'
// status tab and cursor.
export type DeliveryPageQuery = {
  window?: MailWindow;
  status?: DeliveryStatus;
  before?: string;
};
// Kinds of email, as the volume counts them, and of request, as "asked
// for vs sent" compares them.
export type MailKind =
  | "password_reset"
  | "invitation"
  | "registration_code"
  | "account_exists"
  | "email_verification";
export type RequestKind =
  "password_reset" | "registration" | "email_verification";
export type AdminMail = {
  window: MailWindow;
  driver: {
    driver: string;
    host?: string;
    port?: number;
    tls?: string;
    fromAddress?: string;
    fromName?: string;
  };
  queue: {
    listener: string;
    due: number;
    retrying: number;
    oldestPendingAt: string | null;
    failed: number;
    delivered: number;
  }[];
  volume: { hour: string; kind: MailKind; count: number }[];
  requests: { kind: RequestKind; requested: number; sent: number }[];
  topRecipients: {
    email: string;
    total: number;
    resets: number;
    invitations: number;
    registrations: number;
  }[];
  lastDeliveredAt: string | null;
  lastFailure: { listener: string; failedAt: string; error: string } | null;
};
