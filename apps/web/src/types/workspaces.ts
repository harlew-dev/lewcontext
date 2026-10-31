// Mirrors the roles in apps/api/internal/auth/permissions.go, most access first.
export const roles = ["owner", "admin", "member", "viewer"] as const;
export type Role = (typeof roles)[number];
// Mirrors the Permission constants in apps/api/internal/auth/permissions.go.
// Which role has which comes from the API (Workspace.permissions).
export const permissions = [
  "workspace.update",
  "workspace.destroy",
  "workspace.invitation.index",
  "workspace.invitation.store",
  "workspace.invitation.destroy",
  "workspace.member.index",
  "workspace.member.update",
  "workspace.member.destroy",
  "project.store",
  "project.update",
  "project.destroy",
] as const;
export type Permission = (typeof permissions)[number];
// A workspace as the signed-in member sees it: their role, what it allows,
// and the roles they may give others.
export type Workspace = {
  id: string;
  name: string;
  role: Role;
  permissions: Permission[];
  assignableRoles: Role[];
  createdAt: string;
  updatedAt: string;
};
export type WorkspaceInput = Pick<Workspace, "name">;
// Matches the API's limit (validWorkspace in httpapi/workspaces.go).
export const maxWorkspaceName = 80;
// Whether the member may do this. The UI only hides what the API would
// refuse; the API enforces it.
export function can(workspace: Workspace, permission: Permission): boolean {
  return workspace.permissions.includes(permission);
}
// The name as the API will store it, or null when it would be rejected.
export function workspaceName(value: string): string | null {
  const name = value.trim();
  const length = Array.from(name).length;
  return length >= 1 && length <= maxWorkspaceName ? name : null;
}
