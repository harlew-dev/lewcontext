import { useEffect, useRef, useState, type ReactNode } from "react";
import { useLocation, useNavigate, useNavigation } from "react-router";
import {
  ArrowRightStartOnRectangleIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronUpIcon,
  Cog8ToothIcon,
  PlusIcon,
  ShieldCheckIcon,
} from "@heroicons/react/16/solid";
import { QuestionMarkCircleIcon } from "@heroicons/react/20/solid";
import { WorkspaceAvatar } from "@/components/app/workspaces/workspace-avatar";
import { CreateWorkspaceDialog } from "@/components/app/workspaces/workspace-form";
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
import {
  Navbar,
  NavbarItem,
  NavbarLabel,
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
  SidebarToggle,
} from "@/components/ui/sidebar";
import { toast } from "@/components/ui/toast";
import { errorMessage, useApi } from "@/lib/api";
import { appName } from "@/lib/app";
import { navigation, routes } from "@/lib/routes";
import { useSession } from "@/lib/session";
import { useClearCache } from "@/lib/use-clear-cache";
import { useWorkspace, useWorkspaceList } from "@/lib/workspace";
import { initials } from "@/lib/format";
import { SidebarLayout } from "./sidebar-layout";
function AccountDropdownMenu({
  anchor,
  onSignOut,
}: Pick<DropdownMenuProps, "anchor"> & { onSignOut: () => void }) {
  const current = useCurrentPath() === routes.settings;
  return (
    <DropdownMenu className="min-w-64" anchor={anchor}>
      <DropdownItem
        href={routes.settings}
        aria-current={current ? "page" : undefined}
        selected={current}
      >
        <ShieldCheckIcon />
        <DropdownLabel>Account & security</DropdownLabel>
      </DropdownItem>
      <DropdownDivider />
      <DropdownItem variant="danger" onClick={onSignOut}>
        <ArrowRightStartOnRectangleIcon />
        <DropdownLabel>Sign out</DropdownLabel>
      </DropdownItem>
    </DropdownMenu>
  );
}
// Workspace switcher: the current workspace, its settings, every workspace
// the member belongs to (a check on the current one) and "New workspace…". On
// workspace settings (General or Members) its button is selected, and the
// menu's highlight rests on Workspace settings. It's in the sidebar's header
// and, for phones and the hidden desktop sidebar, at the right of the top bar
// (`navbar`, its menu opening from that edge).
function WorkspaceDropdown({
  onCreate,
  navbar = false,
  className,
}: {
  onCreate: () => void;
  navbar?: boolean;
  className?: string;
}) {
  const Item = navbar ? NavbarItem : SidebarItem;
  const Label = navbar ? NavbarLabel : SidebarLabel;
  const workspace = useWorkspace();
  const settings = routes.workspaces.edit(workspace.id);
  const path = useCurrentPath();
  const onSettings = path === settings || path.startsWith(`${settings}/`);
  return (
    <Dropdown>
      <DropdownButton
        as={Item}
        className={className}
        selected={onSettings}
        aria-label="Workspace menu"
      >
        <WorkspaceAvatar workspace={workspace} />
        <Label>{workspace.name}</Label>
        <ChevronDownIcon />
      </DropdownButton>
      <DropdownMenu
        className="min-w-80 lg:min-w-64"
        anchor={navbar ? "bottom end" : "bottom start"}
      >
        <DropdownItem
          href={settings}
          aria-current={onSettings ? "page" : undefined}
          selected={onSettings}
        >
          <Cog8ToothIcon />
          <DropdownLabel>Workspace settings</DropdownLabel>
        </DropdownItem>
        <DropdownDivider />
        <WorkspaceMenuList />
        <DropdownDivider />
        <DropdownItem onClick={onCreate}>
          <PlusIcon />
          <DropdownLabel>New workspace…</DropdownLabel>
        </DropdownItem>
      </DropdownMenu>
    </Dropdown>
  );
}
// The member's workspaces, newest first, scrolling inside the menu. Reaching
// the end loads the next page; "Show more" does the same for keyboards and
// browsers without IntersectionObserver.
function WorkspaceMenuList() {
  const current = useWorkspace();
  const list = useWorkspaceList();
  const end = useRef<HTMLDivElement>(null);
  const { hasMore, loadMore } = list;
  useEffect(() => {
    const target = end.current;
    if (!target || !hasMore || typeof IntersectionObserver === "undefined")
      return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) loadMore();
      },
      { root: target.parentElement },
    );
    observer.observe(target);
    return () => observer.disconnect();
  }, [hasMore, loadMore]);
  const items = list.items.length ? list.items : [current];
  return (
    <div
      aria-busy={list.loadingMore}
      className="col-span-full grid max-h-72 grid-cols-[auto_1fr_1.5rem_0.5rem_auto] overflow-y-auto supports-[grid-template-columns:subgrid]:grid-cols-subgrid"
    >
      {items.map((workspace) => (
        <DropdownItem
          key={workspace.id}
          href={routes.dashboard(workspace.id)}
          aria-current={workspace.id === current.id ? "page" : undefined}
        >
          <WorkspaceAvatar workspace={workspace} />
          <DropdownLabel className="truncate">{workspace.name}</DropdownLabel>
          {workspace.id === current.id && (
            <span className="col-start-5 row-start-1 flex justify-end">
              <CheckIcon className="size-4" />
            </span>
          )}
        </DropdownItem>
      ))}
      {list.loadingMore && <WorkspaceRowSkeleton />}
      {list.error ? (
        <DropdownItem keepOpen onClick={list.retry}>
          <DropdownLabel>Couldn’t load more · Retry</DropdownLabel>
        </DropdownItem>
      ) : (
        list.hasMore &&
        !list.loadingMore && (
          <DropdownItem keepOpen onClick={list.loadMore}>
            <DropdownLabel className="text-zinc-500 dark:text-zinc-400">
              Show more
            </DropdownLabel>
          </DropdownItem>
        )
      )}
      <div ref={end} aria-hidden="true" className="col-span-full h-px" />
    </div>
  );
}
function WorkspaceRowSkeleton() {
  return (
    <div
      role="status"
      aria-label="Loading more workspaces"
      className="col-span-full flex animate-pulse items-center gap-2.5 px-3.5 py-2.5 sm:px-3 sm:py-1.5"
    >
      <span className="size-6 rounded-md bg-zinc-950/5 sm:size-5 dark:bg-white/5" />
      <span className="h-3.5 flex-1 rounded bg-zinc-950/5 dark:bg-white/5" />
    </div>
  );
}
// The route being navigated to, or the current one when idle. Marking the
// destination as current on click lets the sidebar indicator slide while a
// route loader runs.
function useCurrentPath() {
  const pending = useNavigation().location;
  const location = useLocation();
  return (pending ?? location).pathname;
}
function HelpDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog open={open} onClose={onClose}>
      <DialogTitle>Make yourself at home.</DialogTitle>
      <DialogDescription>
        {appName} keeps your projects together in workspaces you can share.
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
// Signed-in chrome for /app pages (pages/app/layout.tsx): the sidebar layout with the app's
// navigation, account menu, sign-out and the getting-started dialog.
export function ApplicationLayout({ children }: { children: ReactNode }) {
  const api = useApi();
  const session = useSession();
  const workspace = useWorkspace();
  const navigate = useNavigate();
  const clearCache = useClearCache();
  const [help, setHelp] = useState(false);
  const [creating, setCreating] = useState(false);
  const currentPath = useCurrentPath();
  // On account settings the account menu's buttons are selected.
  const onAccount = currentPath === routes.settings;
  const userInitials = initials(session.user.name);
  async function signOut() {
    try {
      await api.auth().logout();
      await navigate(routes.login);
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
          <NavbarSection className="min-w-0">
            <WorkspaceDropdown
              navbar
              className="min-w-0"
              onCreate={() => setCreating(true)}
            />
          </NavbarSection>
        </Navbar>
      }
      sidebar={
        <Sidebar>
          <SidebarHeader>
            <div className="flex items-center gap-2 lg:mb-2.5">
              <WorkspaceDropdown
                className="min-w-0 flex-1"
                onCreate={() => setCreating(true)}
              />
              <SidebarToggle />
            </div>
          </SidebarHeader>
          <SidebarBody>
            <SidebarSection aria-label="Main navigation">
              {navigation(workspace.id).map(({ label, href, icon: Icon }) => (
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
          <SidebarFooter>
            <Dropdown>
              <DropdownButton
                as={SidebarItem}
                selected={onAccount}
                aria-label="Account menu"
              >
                <span className="flex min-w-0 items-center gap-3">
                  <Avatar
                    square
                    initials={userInitials}
                    className="size-10 bg-zinc-900 text-white dark:bg-white dark:text-zinc-950"
                  />
                  <span className="min-w-0 truncate text-sm/5 font-medium text-zinc-950 dark:text-white">
                    {session.user.name}
                  </span>
                </span>
                <ChevronUpIcon />
              </DropdownButton>
              <AccountDropdownMenu anchor="top start" onSignOut={signOut} />
            </Dropdown>
          </SidebarFooter>
        </Sidebar>
      }
    >
      {children}
      <HelpDialog open={help} onClose={() => setHelp(false)} />
      <CreateWorkspaceDialog
        open={creating}
        onClose={() => setCreating(false)}
      />
    </SidebarLayout>
  );
}
