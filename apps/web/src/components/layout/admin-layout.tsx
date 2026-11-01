import type { ReactNode } from "react";
import { useLocation, useNavigate, useNavigation } from "react-router";
import {
  ArrowRightStartOnRectangleIcon,
  ArrowUturnLeftIcon,
  ChevronUpIcon,
  ShieldCheckIcon,
} from "@heroicons/react/16/solid";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Notice } from "@/components/ui/feedback";
import {
  Dropdown,
  DropdownButton,
  DropdownDivider,
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
  SidebarHeading,
  SidebarItem,
  SidebarLabel,
  SidebarSection,
  SidebarToggle,
} from "@/components/ui/sidebar";
import { toast } from "@/components/ui/toast";
import { errorMessage } from "@/lib/api";
import { useAdminApi } from "@/lib/api/admin";
import { useAdminKey, useAdminSession, usePreviousAdminKey } from "@/lib/admin";
import {
  adminMonitoringNavigation,
  adminNavigation,
  adminRoutes,
} from "@/lib/admin-routes";
import { initials } from "@/lib/format";
import { routes } from "@/lib/routes";
import { useClearCache } from "@/lib/use-clear-cache";
import { SidebarLayout } from "./sidebar-layout";
// The app name with a red "Admin" badge, so the dashboard is never mistaken
// for the app. `stacked` puts the badge under the name, leaving the sidebar's
// header room for its toggle.
function AdminBrand({ stacked = false }: { stacked?: boolean }) {
  return (
    <span
      className={
        stacked
          ? "flex flex-col items-start gap-1.5"
          : "flex items-center gap-2"
      }
    >
      <Logo />
      <Badge color="red">Admin</Badge>
    </span>
  );
}
// Above every admin page while its key is being retired
// (ADMIN_ROUTE_KEY_PREVIOUS): the session keeps working until it ends, but
// this address won't open the sign-in again.
function RetiringKeyNotice() {
  if (!usePreviousAdminKey()) return null;
  return (
    <Notice variant="warning" className="mb-8">
      This admin address is being retired. Ask your operator for the new one
      before this session ends.
    </Notice>
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
// layout with the admin pages, a notice while the key is being retired, and
// an account menu leading back to the app (or its Account & security) and
// Sign out. Signing out of the dashboard
// leaves the app session alone. The app's pages are another build: following
// those links loads them as a new page (pages/admin/not-found.tsx).
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
      await navigate(adminRoutes.login(key));
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
            <div className="flex items-start justify-between gap-2 lg:mb-2.5">
              <span className="px-2 pt-1.5">
                <AdminBrand stacked />
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
                  {href === adminRoutes.alerts.index(key) &&
                    session.openAlerts > 0 && (
                      <Badge color="red" className="ml-auto">
                        {session.openAlerts}
                        <span className="sr-only"> open</span>
                      </Badge>
                    )}
                </SidebarItem>
              ))}
            </SidebarSection>
            <SidebarSection
              aria-labelledby="admin-monitoring-heading"
              className="mt-8"
            >
              <SidebarHeading id="admin-monitoring-heading">
                Monitoring
              </SidebarHeading>
              {adminMonitoringNavigation(key).map(
                ({ label, href, icon: Icon }) => (
                  // Each page is current on its own path and its detail
                  // pages (IP addresses on one address's), never as the
                  // prefix of another page: Overview's path is theirs.
                  <SidebarItem
                    key={href}
                    href={href}
                    current={
                      currentPath === href ||
                      (href !== adminRoutes.monitoring.index(key) &&
                        currentPath.startsWith(`${href}/`))
                    }
                  >
                    <Icon />
                    <SidebarLabel>{label}</SidebarLabel>
                  </SidebarItem>
                ),
              )}
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
                <DropdownItem href={routes.app}>
                  <ArrowUturnLeftIcon />
                  <DropdownLabel>Back to app</DropdownLabel>
                </DropdownItem>
                <DropdownItem href={routes.settings}>
                  <ShieldCheckIcon />
                  <DropdownLabel>Account & security</DropdownLabel>
                </DropdownItem>
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
      <RetiringKeyNotice />
      {children}
    </SidebarLayout>
  );
}
