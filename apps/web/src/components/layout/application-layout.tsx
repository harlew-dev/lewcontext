import { useEffect, useState, type ReactElement, type ReactNode } from "react";
import { useRouter } from "next/router";
import {
  ArrowRightStartOnRectangleIcon,
  ChevronDownIcon,
  ChevronUpIcon,
  Cog8ToothIcon,
  LightBulbIcon,
  PlusIcon,
  ShieldCheckIcon,
} from "@heroicons/react/16/solid";
import { QuestionMarkCircleIcon } from "@heroicons/react/20/solid";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Dropdown,
  DropdownButton,
  DropdownDivider,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
  type DropdownMenuProps,
} from "@/components/ui/dropdown";
import { Notice } from "@/components/ui/feedback";
import {
  Navbar,
  NavbarItem,
  NavbarSection,
  NavbarSpacer,
} from "@/components/ui/navbar";
import {
  Sidebar,
  SidebarBody,
  SidebarFooter,
  SidebarHeader,
  SidebarItem,
  SidebarLabel,
  SidebarSection,
  SidebarSpacer,
} from "@/components/ui/sidebar";
import { request, errorMessage } from "@/lib/api";
import { apiRoutes, navigation, routes } from "@/lib/routes";
import type { AppPageProps, Session, User } from "@/lib/types";
import { SidebarLayout } from "./sidebar-layout";
function initialsOf(user: User) {
  return user.name
    .split(" ")
    .map((part) => part[0])
    .slice(0, 2)
    .join("");
}
function AccountDropdownMenu({
  anchor,
  onSignOut,
  onHelp,
}: Pick<DropdownMenuProps, "anchor"> & {
  onSignOut: () => void;
  onHelp: () => void;
}) {
  return (
    <DropdownMenu className="min-w-64" anchor={anchor}>
      <DropdownItem href={routes.settings}>
        <ShieldCheckIcon />
        <DropdownLabel>Account & security</DropdownLabel>
      </DropdownItem>
      <DropdownItem onClick={onHelp}>
        <LightBulbIcon />
        <DropdownLabel>Getting started</DropdownLabel>
      </DropdownItem>
      <DropdownDivider />
      <DropdownItem onClick={onSignOut}>
        <ArrowRightStartOnRectangleIcon />
        <DropdownLabel>Sign out</DropdownLabel>
      </DropdownItem>
    </DropdownMenu>
  );
}
const workspaceAvatar =
  "bg-zinc-900 text-white dark:bg-white dark:text-zinc-950";
// Workspace switcher. Forma has one personal workspace per account, so
// the menu lists it alone; switching and creating workspaces are placeholders
// until the API supports them.
function WorkspaceDropdown() {
  return (
    <Dropdown>
      <DropdownButton
        as={SidebarItem}
        className="lg:mb-2.5"
        aria-label="Workspace menu"
      >
        <Avatar square initials="F" className={workspaceAvatar} />
        <SidebarLabel>Forma</SidebarLabel>
        <ChevronDownIcon />
      </DropdownButton>
      <DropdownMenu className="min-w-80 lg:min-w-64" anchor="bottom start">
        <DropdownItem href={routes.settings}>
          <Cog8ToothIcon />
          <DropdownLabel>Settings</DropdownLabel>
        </DropdownItem>
        <DropdownDivider />
        <DropdownItem href={routes.dashboard}>
          <Avatar square initials="F" className={workspaceAvatar} />
          <DropdownLabel>Personal workspace</DropdownLabel>
        </DropdownItem>
        <DropdownDivider />
        <DropdownItem disabled>
          <PlusIcon />
          <DropdownLabel>New workspace…</DropdownLabel>
        </DropdownItem>
      </DropdownMenu>
    </Dropdown>
  );
}
// The route being navigated to, or the current one when idle. Marking the
// destination as current on click lets the sidebar indicator slide while the
// next page's server data loads.
function useCurrentPath() {
  const router = useRouter();
  const [pending, setPending] = useState<string | null>(null);
  useEffect(() => {
    const start = (url: string) => setPending(url.split(/[?#]/)[0]);
    const stop = () => setPending(null);
    router.events.on("routeChangeStart", start);
    router.events.on("routeChangeComplete", stop);
    router.events.on("routeChangeError", stop);
    return () => {
      router.events.off("routeChangeStart", start);
      router.events.off("routeChangeComplete", stop);
      router.events.off("routeChangeError", stop);
    };
  }, [router.events]);
  return pending ?? router.pathname;
}
function HelpDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog open={open} onClose={onClose}>
      <DialogTitle>Make yourself at home.</DialogTitle>
      <DialogDescription>
        Forma keeps your projects together in a private workspace.
      </DialogDescription>
      <DialogBody>
        <ol className="list-decimal space-y-2 pl-5 text-base/6 text-zinc-700 sm:text-sm/6 dark:text-zinc-300">
          <li>Create a project with a name and a short description.</li>
          <li>Choose a status: planned, in progress, or completed.</li>
          <li>Open any project to update it as your work takes shape.</li>
        </ol>
      </DialogBody>
      <DialogActions>
        <Button onClick={onClose}>Got it</Button>
      </DialogActions>
    </Dialog>
  );
}
// Signed-in chrome for every /app page: the sidebar layout with Forma's
// navigation, account menu, sign-out and the getting-started dialog.
export function ApplicationLayout({
  session,
  children,
}: {
  session: Session;
  children: ReactNode;
}) {
  const router = useRouter();
  const [error, setError] = useState("");
  const [help, setHelp] = useState(false);
  const currentPath = useCurrentPath();
  const initials = initialsOf(session.user);
  async function signOut() {
    setError("");
    try {
      await request(
        apiRoutes.logout,
        { method: "POST", body: "{}" },
        session.csrfToken,
      );
      await router.push(routes.login);
    } catch (e) {
      setError(errorMessage(e));
    }
  }
  const accountMenu = (anchor: DropdownMenuProps["anchor"]) => (
    <AccountDropdownMenu
      anchor={anchor}
      onSignOut={signOut}
      onHelp={() => setHelp(true)}
    />
  );
  return (
    <SidebarLayout
      navbar={
        <Navbar>
          <NavbarSpacer />
          <NavbarSection>
            <Dropdown>
              <DropdownButton as={NavbarItem} aria-label="Account menu">
                <Avatar
                  square
                  initials={initials}
                  className="bg-zinc-900 text-white dark:bg-white dark:text-zinc-950"
                />
              </DropdownButton>
              {accountMenu("bottom end")}
            </Dropdown>
          </NavbarSection>
        </Navbar>
      }
      sidebar={
        <Sidebar>
          <SidebarHeader>
            <WorkspaceDropdown />
          </SidebarHeader>
          <SidebarBody>
            <SidebarSection aria-label="Main navigation">
              {navigation.map(({ label, href, icon: Icon }) => (
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
            <SidebarSpacer />
            <SidebarSection>
              <SidebarItem onClick={() => setHelp(true)}>
                <QuestionMarkCircleIcon />
                <SidebarLabel>Getting started</SidebarLabel>
              </SidebarItem>
            </SidebarSection>
          </SidebarBody>
          <SidebarFooter className="max-lg:hidden">
            <Dropdown>
              <DropdownButton as={SidebarItem} aria-label="Account menu">
                <span className="flex min-w-0 items-center gap-3">
                  <Avatar
                    square
                    initials={initials}
                    className="size-10 bg-zinc-900 text-white dark:bg-white dark:text-zinc-950"
                  />
                  <span className="min-w-0">
                    <span className="block truncate text-sm/5 font-medium text-zinc-950 dark:text-white">
                      {session.user.name}
                    </span>
                    <span className="block truncate text-xs/5 font-normal text-zinc-500 dark:text-zinc-400">
                      {session.user.email}
                    </span>
                  </span>
                </span>
                <ChevronUpIcon />
              </DropdownButton>
              {accountMenu("top start")}
            </Dropdown>
          </SidebarFooter>
        </Sidebar>
      }
    >
      {error && (
        <Notice variant="error" className="mb-6">
          {error}
        </Notice>
      )}
      {children}
      <HelpDialog open={help} onClose={() => setHelp(false)} />
    </SidebarLayout>
  );
}
// getLayout for signed-in pages: _app keeps this layout mounted between pages,
// which preserves sidebar state and lets the current-page indicator animate.
export function appLayout(page: ReactElement, props: AppPageProps) {
  if ("unavailable" in props) return page;
  return <ApplicationLayout session={props.session}>{page}</ApplicationLayout>;
}
