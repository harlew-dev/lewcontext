import type { Sort } from "@/lib/pagination";
import type { User } from "@/lib/types";
import type { Role } from "./workspaces";
// What the admin dashboard shows (docs/api/admin.md): every account and every
// workspace, with counts rather than project contents.

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
