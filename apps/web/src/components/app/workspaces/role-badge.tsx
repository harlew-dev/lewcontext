import { Badge, type BadgeColor } from "@/components/ui/badge";
import type { Role } from "@/types/workspaces";
export const roleLabels: Record<Role, string> = {
  owner: "Owner",
  admin: "Admin",
  member: "Member",
  viewer: "Viewer",
};
const roleColors: Record<Role, BadgeColor> = {
  owner: "purple",
  admin: "pink",
  member: "zinc",
  viewer: "zinc",
};
export function RoleBadge({ role }: { role: Role }) {
  return <Badge color={roleColors[role]}>{roleLabels[role]}</Badge>;
}
