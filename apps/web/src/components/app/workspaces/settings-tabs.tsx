import { useLocation } from "react-router";
import { Navbar, NavbarItem, NavbarSection } from "@/components/ui/navbar";
import { routes } from "@/lib/routes";
import { useWorkspace } from "@/lib/workspace";
import { can, type Permission } from "@/types/workspaces";
// Workspace settings' tabs, in order (their content slides by this order).
export function settingsTabs(workspaceId: string) {
  return [
    { label: "General", href: routes.workspaces.edit(workspaceId) },
    { label: "Members", href: routes.members.index(workspaceId) },
    { label: "API Keys", href: routes.workspaceApiKeys.index(workspaceId) },
    { label: "Logs", href: routes.events.index(workspaceId) },
  ];
}
// The tabs at the top of workspace settings, the current one from the URL.
// Each but General needs a permission (owners and admins have them all), so
// members and viewers get no tabs at all.
export function SettingsTabs() {
  const workspace = useWorkspace();
  const { pathname } = useLocation();
  const permissionFor: Record<string, Permission | undefined> = {
    General: undefined,
    Members: "workspace.member.index",
    "API Keys": "workspace.api_key.index",
    Logs: "workspace.event.index",
  };
  const tabs = settingsTabs(workspace.id).filter((tab) => {
    const permission = permissionFor[tab.label];
    return permission === undefined || can(workspace, permission);
  });
  if (tabs.length < 2) return null;
  return (
    <Navbar aria-label="Workspace settings" className="mt-4">
      {/* Pulled left by the items' padding, so the labels line up with the
          content's edge (their hover background reaches into the margin). */}
      <NavbarSection className="-ml-2">
        {tabs.map(({ label, href }) => (
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
