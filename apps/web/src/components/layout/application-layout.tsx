import { useEffect, useRef, useState, type ReactNode } from "react";
import { useLocation, useNavigate, useNavigation } from "react-router";
import {
  ArrowRightStartOnRectangleIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronUpIcon,
  Cog8ToothIcon,
  LightBulbIcon,
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
import { toast } from "@/components/ui/toast";
import { errorMessage, useApi } from "@/lib/api";
import { navigation, routes } from "@/lib/routes";
import { useSession } from "@/lib/session";
import { useWorkspace, useWorkspaceList } from "@/lib/workspace";
import type { User } from "@/lib/types";
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
// Workspace switcher: the current workspace, its settings, every workspace
// the member belongs to and "New workspace…".
function WorkspaceDropdown({ onCreate }: { onCreate: () => void }) {
  const workspace = useWorkspace();
  return (
    <Dropdown>
      <DropdownButton
        as={SidebarItem}
        className="lg:mb-2.5"
        aria-label="Workspace menu"
      >
        <WorkspaceAvatar workspace={workspace} />
        <SidebarLabel>{workspace.name}</SidebarLabel>
        <ChevronDownIcon />
      </DropdownButton>
      <DropdownMenu className="min-w-80 lg:min-w-64" anchor="bottom start">
        <DropdownItem href={routes.workspaceSettings(workspace.id)}>
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
        Forma keeps your projects together in workspaces you can share.
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
// Signed-in chrome for /app pages (pages/app/layout.tsx): the sidebar layout with Forma's
// navigation, account menu, sign-out and the getting-started dialog.
export function ApplicationLayout({ children }: { children: ReactNode }) {
  const api = useApi();
  const session = useSession();
  const workspace = useWorkspace();
  const navigate = useNavigate();
  const [help, setHelp] = useState(false);
  const [creating, setCreating] = useState(false);
  const currentPath = useCurrentPath();
  const initials = initialsOf(session.user);
  async function signOut() {
    try {
      await api.auth().logout();
      await navigate(routes.login);
    } catch (e) {
      toast.error(errorMessage(e));
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
            <WorkspaceDropdown onCreate={() => setCreating(true)} />
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
      {children}
      <HelpDialog open={help} onClose={() => setHelp(false)} />
      <CreateWorkspaceDialog
        open={creating}
        onClose={() => setCreating(false)}
      />
    </SidebarLayout>
  );
}
