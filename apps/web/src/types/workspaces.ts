// Mirrors the roles in apps/api/internal/store/workspaces.go, most access first.
export const roles = ["owner", "admin", "member"] as const;
export type Role = (typeof roles)[number];
// A workspace as the signed-in member sees it, with their role.
export type Workspace = {
  id: string;
  name: string;
  role: Role;
  createdAt: string;
  updatedAt: string;
};
export type WorkspaceInput = Pick<Workspace, "name">;
// Matches the API's limit (validWorkspace in httpapi/workspaces.go).
export const maxWorkspaceName = 80;
// Owners and admins can rename a workspace; the API enforces the same rule.
export function canManage(role: Role): boolean {
  return role === "owner" || role === "admin";
}
// The name as the API will store it, or null when it would be rejected.
export function workspaceName(value: string): string | null {
  const name = value.trim();
  const length = Array.from(name).length;
  return length >= 1 && length <= maxWorkspaceName ? name : null;
}
