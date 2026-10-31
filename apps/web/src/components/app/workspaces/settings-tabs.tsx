import { useLocation } from "react-router";
import { Navbar, NavbarItem, NavbarSection } from "@/components/ui/navbar";
import { routes } from "@/lib/routes";
import { useWorkspace } from "@/lib/workspace";
import { can } from "@/types/workspaces";
// Workspace settings' tabs, in order (their content slides by this order).
export function settingsTabs(workspaceId: string) {
  return [
    { label: "General", href: routes.workspaces.edit(workspaceId) },
    { label: "Members", href: routes.members.index(workspaceId) },
  ];
}
// General / Members tabs at the top of workspace settings, the current one
// from the URL. Only owners and admins can see members, so everyone else
// gets no tabs at all.
export function SettingsTabs() {
  const workspace = useWorkspace();
  const { pathname } = useLocation();
  if (!can(workspace, "workspace.member.index")) return null;
  return (
    <Navbar aria-label="Workspace settings" className="mt-4">
      {/* Pulled left by the items' padding, so the labels line up with the
          content's edge (their hover background reaches into the margin). */}
      <NavbarSection className="-ml-2">
        {settingsTabs(workspace.id).map(({ label, href }) => (
          <NavbarItem
            key={href}
            href={href}
            current={pathname === href}
            aria-current={pathname === href ? "page" : undefined}
          >
            {label}
          </NavbarItem>
        ))}
      </NavbarSection>
    </Navbar>
  );
}
