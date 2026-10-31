import { Navbar, NavbarItem, NavbarSection } from "@/components/ui/navbar";
import { routes } from "@/lib/routes";
import { useWorkspace } from "@/lib/workspace";
import { can } from "@/types/workspaces";
// General / Members tabs at the top of workspace settings. Only owners and
// admins can see members, so everyone else gets no tabs at all.
export function SettingsTabs({ current }: { current: "general" | "members" }) {
  const workspace = useWorkspace();
  if (!can(workspace, "workspace.member.index")) return null;
  return (
    <Navbar aria-label="Workspace settings" className="mt-4">
      {/* Pulled left by the items' padding, so the labels line up with the
          content's edge (their hover background reaches into the margin). */}
      <NavbarSection className="-ml-2">
        <NavbarItem
          href={routes.workspaces.edit(workspace.id)}
          current={current === "general"}
          aria-current={current === "general" ? "page" : undefined}
        >
          General
        </NavbarItem>
        <NavbarItem
          href={routes.members.index(workspace.id)}
          current={current === "members"}
          aria-current={current === "members" ? "page" : undefined}
        >
          Members
        </NavbarItem>
      </NavbarSection>
    </Navbar>
  );
}
