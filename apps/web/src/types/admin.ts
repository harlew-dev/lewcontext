import type { Sort } from "@/lib/pagination";
import type { User } from "@/lib/types";
import type { Role } from "./workspaces";
// What the admin dashboard shows (docs/api/admin.md): every account and every
// workspace, the audit trail and the mail queue.

// The signed-in admin's session, as /api/admin/me answers.
export type AdminSession = { user: User; csrfToken: string; expiresAt: string };
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
};
export type AdminMembership = {
  workspace: { id: string; name: string };
  role: Role;
  addedAt: string;
};
export type AdminUserDetail = AdminUser & {
  memberships: AdminMembership[];
  sessionCount: number;
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
export type AdminWorkspaceDetail = AdminWorkspace & { members: AdminMember[] };
// Sorts the lists accept; "-field" sorts descending. Mirror adminUserSorts
// and adminWorkspaceSorts in apps/api/internal/store/admin_views.go.
export const adminUserSortable = ["created", "name", "email"] as const;
export type AdminUserSort = Sort<(typeof adminUserSortable)[number]>;
export const defaultAdminUserSort: AdminUserSort = "-created";
export const adminWorkspaceSortable = ["created", "name"] as const;
export type AdminWorkspaceSort = Sort<(typeof adminWorkspaceSortable)[number]>;
export const defaultAdminWorkspaceSort: AdminWorkspaceSort = "-created";
// A list page's state, kept in its URL.
export type AdminListQuery<S> = { page: number; q: string; sort: S };

// An account or workspace named inside an event: name (and an account's
// email) are missing once it's deleted, leaving the ID from the payload.
export type AdminRef = { id: string; name?: string; email?: string };
// An event as the Events list shows it. The payload is as stored: each
// event's own fields (docs/api/events.md).
export type AdminEvent = {
  id: string;
  name: string;
  createdAt: string;
  actor: AdminRef | null;
  user: AdminRef | null;
  workspace: AdminRef | null;
  ip: string | null;
  payload: Record<string, unknown>;
};
// The Events list's filters (docs/api/admin.md), as in its page URL and its
// API query: a catalogue name or a prefix ending in ".", an account, a
// workspace, an email, an IP or range, RFC 3339 times, admin reads, and the
// cursor (rows older than that event).
export type EventFilters = {
  name?: string;
  account?: string;
  workspace?: string;
  email?: string;
  ip?: string;
  from?: string;
  to?: string;
  adminReads?: "include";
  before?: string;
};
// A cursor page: rows older than the cursor, and the next one's cursor.
export type CursorPage<T> = { data: T[]; next: string | null };
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
// The filters' names, from GET /api/admin/catalogue.
export type AdminCatalogue = {
  events: string[];
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
