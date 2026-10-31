import { permissions, type Role, type Workspace } from "@/types/workspaces";
// What the API sends for each role (apps/api/internal/auth/permissions.go).
// The app reads these from the response; only tests spell them out.
const granted: Record<
  Role,
  Pick<Workspace, "permissions" | "assignableRoles">
> = {
  owner: {
    permissions: [...permissions],
    assignableRoles: ["owner", "admin", "member", "viewer"],
  },
  admin: {
    permissions: permissions.filter((p) => p !== "workspace.destroy"),
    assignableRoles: ["member", "viewer"],
  },
  member: {
    permissions: ["project.store", "project.update"],
    assignableRoles: [],
  },
  viewer: { permissions: [], assignableRoles: [] },
};
// A workspace the signed-in member sees with `role`. Override what a test
// checks: testWorkspace("viewer", { name: "Acme" })
export function testWorkspace(
  role: Role = "owner",
  overrides: Partial<Workspace> = {},
): Workspace {
  return {
    id: "wsp_2yEHYLwiwPVhLW",
    name: "Acme",
    role,
    ...granted[role],
    createdAt: "",
    updatedAt: "",
    ...overrides,
  };
}
