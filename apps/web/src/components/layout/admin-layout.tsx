import type { ReactNode } from "react";
import { useLocation, useNavigate, useNavigation } from "react-router";
import {
  ArrowRightStartOnRectangleIcon,
  ChevronUpIcon,
} from "@heroicons/react/16/solid";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import {
  Dropdown,
  DropdownButton,
  DropdownDivider,
  DropdownHeader,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
} from "@/components/ui/dropdown";
import { Logo } from "@/components/ui/logo";
import { Navbar, NavbarSection, NavbarSpacer } from "@/components/ui/navbar";
import {
  Sidebar,
  SidebarBody,
  SidebarFooter,
  SidebarHeader,
  SidebarItem,
  SidebarLabel,
  SidebarSection,
  SidebarToggle,
} from "@/components/ui/sidebar";
import { toast } from "@/components/ui/toast";
import { errorMessage, useAdminApi } from "@/lib/api";
import { useAdminKey, useAdminSession } from "@/lib/admin";
import { initials } from "@/lib/format";
import { adminNavigation, routes } from "@/lib/routes";
import { useClearCache } from "@/lib/use-clear-cache";
import { SidebarLayout } from "./sidebar-layout";
// The app name with an "Admin" badge, so the dashboard is never mistaken for
// the app.
function AdminBrand() {
  return (
    <span className="flex items-center gap-2">
      <Logo />
      <Badge>Admin</Badge>
    </span>
  );
}
// The route being navigated to, or the current one when idle, so the
// sidebar indicator slides on click.
function useCurrentPath() {
  const pending = useNavigation().location;
  const location = useLocation();
  return (pending ?? location).pathname;
}
// Signed-in chrome for the admin pages (pages/admin/layout.tsx): the sidebar
// layout with Users and Workspaces, and who is signed in with Sign out.
// Signing out of the dashboard leaves the app session alone.
export function AdminLayout({ children }: { children: ReactNode }) {
  const api = useAdminApi();
  const key = useAdminKey();
  const session = useAdminSession();
  const navigate = useNavigate();
  const clearCache = useClearCache();
  const currentPath = useCurrentPath();
  async function signOut() {
    try {
      await api.logout();
      await navigate(routes.admin.login(key));
      clearCache();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }
  return (
    <SidebarLayout
      navbar={
        <Navbar>
          <NavbarSpacer />
          <NavbarSection>
            <AdminBrand />
          </NavbarSection>
        </Navbar>
      }
      sidebar={
        <Sidebar>
          <SidebarHeader>
            <div className="flex items-center justify-between gap-2 lg:mb-2.5">
              <span className="px-2">
                <AdminBrand />
              </span>
              <SidebarToggle />
            </div>
          </SidebarHeader>
          <SidebarBody>
            <SidebarSection aria-label="Admin navigation">
              {adminNavigation(key).map(({ label, href, icon: Icon }) => (
                <SidebarItem
                  key={href}
                  href={href}
                  current={
                    currentPath === href || currentPath.startsWith(`${href}/`)
                  }
                >
                  <Icon />
                  <SidebarLabel>{label}</SidebarLabel>
                </SidebarItem>
              ))}
            </SidebarSection>
          </SidebarBody>
          <SidebarFooter>
            <Dropdown>
              <DropdownButton as={SidebarItem} aria-label="Admin account menu">
                <span className="flex min-w-0 items-center gap-3">
                  <Avatar
                    square
                    initials={initials(session.user.name)}
                    className="size-10 bg-zinc-900 text-white dark:bg-white dark:text-zinc-950"
                  />
                  <span className="min-w-0 truncate text-sm/5 font-medium text-zinc-950 dark:text-white">
                    {session.user.name}
                  </span>
                </span>
                <ChevronUpIcon />
              </DropdownButton>
              <DropdownMenu className="min-w-64" anchor="top start">
                <DropdownHeader>
                  <div className="text-sm/7 text-zinc-500 dark:text-zinc-400">
                    Signed in as
                  </div>
                  <div className="truncate text-sm/5 font-medium text-zinc-950 dark:text-white">
                    {session.user.email}
                  </div>
                </DropdownHeader>
                <DropdownDivider />
                <DropdownItem variant="danger" onClick={signOut}>
                  <ArrowRightStartOnRectangleIcon />
                  <DropdownLabel>Sign out</DropdownLabel>
                </DropdownItem>
              </DropdownMenu>
            </Dropdown>
          </SidebarFooter>
        </Sidebar>
      }
    >
      {children}
    </SidebarLayout>
  );
}
