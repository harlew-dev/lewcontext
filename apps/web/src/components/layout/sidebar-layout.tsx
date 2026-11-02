import { useState, type ReactNode } from "react";
import { useLocation } from "react-router";
import { Drawer } from "baseui/drawer";
import { slot } from "@/lib/baseui";
import { cx } from "@/lib/cx";
import { useScrollLock } from "@/lib/scroll-lock";
import { useDockedSidebar } from "@/lib/use-docked-sidebar";
import { NavbarItem } from "@/components/ui/navbar";
import {
  SidebarCloseContext,
  SidebarToggleContext,
} from "@/components/ui/sidebar-context";
// A window with a sidebar pane on the left: shows or hides the desktop sidebar.
const sidebarIconPath =
  "M4.75 3A2.75 2.75 0 0 0 2 5.75v8.5A2.75 2.75 0 0 0 4.75 17h10.5A2.75 2.75 0 0 0 18 14.25v-8.5A2.75 2.75 0 0 0 15.25 3H4.75ZM3.5 5.75c0-.69.56-1.25 1.25-1.25H7v11H4.75c-.69 0-1.25-.56-1.25-1.25v-8.5Zm5 9.75h6.75c.69 0 1.25-.56 1.25-1.25v-8.5c0-.69-.56-1.25-1.25-1.25H8.5v11Z";
function SidebarIcon() {
  return (
    <svg data-slot="icon" viewBox="0 0 20 20" aria-hidden="true">
      <path fillRule="evenodd" d={sidebarIconPath} />
    </svg>
  );
}
function OpenMenuIcon() {
  return (
    <svg data-slot="icon" viewBox="0 0 20 20" aria-hidden="true">
      <path d="M2 6.75C2 6.33579 2.33579 6 2.75 6H17.25C17.6642 6 18 6.33579 18 6.75C18 7.16421 17.6642 7.5 17.25 7.5H2.75C2.33579 7.5 2 7.16421 2 6.75ZM2 13.25C2 12.8358 2.33579 12.5 2.75 12.5H17.25C17.6642 12.5 18 12.8358 18 13.25C18 13.6642 17.6642 14 17.25 14H2.75C2.33579 14 2 13.6642 2 13.25Z" />
    </svg>
  );
}
function CloseMenuIcon() {
  return (
    <svg data-slot="icon" viewBox="0 0 20 20" aria-hidden="true">
      <path d="M6.28 5.22a.75.75 0 0 0-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 1 0 1.06 1.06L10 11.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L11.06 10l3.72-3.72a.75.75 0 0 0-1.06-1.06L10 8.94 6.28 5.22Z" />
    </svg>
  );
}
// The mobile sidebar is a BaseUI Drawer (focus lock, Escape, backdrop click)
// styled as a slide-over panel.
const DrawerRoot = slot(
  "div",
  "fixed inset-0 data-closed:pointer-events-none lg:hidden",
);
const DrawerBackdrop = slot(
  "div",
  "fixed inset-0 bg-black/30 transition duration-300 ease-out data-closed:opacity-0 motion-reduce:transition-none",
);
const DrawerContainer = slot(
  "div",
  "fixed inset-y-0 w-full max-w-80 p-2 transition duration-300 ease-in-out data-closed:-translate-x-full motion-reduce:transition-none",
  { props: () => ({ role: "dialog", "aria-label": "Navigation" }) },
);
const DrawerPanel = slot(
  "div",
  "flex h-full flex-col rounded-lg bg-white shadow-xs ring-1 ring-zinc-950/5 dark:bg-zinc-900 dark:ring-white/10",
);
const NoCloseButton = () => null;
function MobileSidebar({
  open,
  close,
  children,
}: {
  open: boolean;
  close: () => void;
  children: ReactNode;
}) {
  useScrollLock(open);
  return (
    <Drawer
      isOpen={open}
      onClose={close}
      anchor="left"
      animate
      autoFocus
      closeable
      overrides={{
        Root: { component: DrawerRoot },
        Backdrop: { component: DrawerBackdrop },
        DrawerContainer: { component: DrawerContainer },
        DrawerBody: { component: DrawerPanel },
        Close: { component: NoCloseButton },
      }}
    >
      <SidebarToggleContext.Provider
        value={{
          label: "Close navigation",
          icon: <CloseMenuIcon />,
          onClick: close,
        }}
      >
        <SidebarCloseContext.Provider value={close}>
          {children}
        </SidebarCloseContext.Provider>
      </SidebarToggleContext.Provider>
    </Drawer>
  );
}
// The desktop sidebar's toggle is one button that never leaves: hiding the
// sidebar glides it from beside the workspace menu into the navbar's corner
// (where the mobile menu button is), in step with the sidebar sliding out;
// showing glides it back. It stays visible while the sidebar is invisible.
// From its place in the sidebar header (p-4, at the row's end) the corner is
// its own width plus both paddings to the right, and 1.5 up (py-2.5 vs p-4).
function desktopToggleClasses(docked: boolean) {
  return cx(
    "transition-[translate] duration-300 ease-in-out motion-reduce:transition-none",
    !docked && "visible translate-x-[calc(100%+--spacing(8))] -translate-y-1.5",
  );
}
// The desktop sidebar: slides out to the left when hidden. It becomes
// invisible (out of the tab order) only once it has slid out, and visible at
// once when it comes back.
function desktopSidebarClasses(docked: boolean) {
  return cx(
    "fixed inset-y-0 left-0 w-64 duration-300 ease-in-out max-lg:hidden motion-reduce:transition-none",
    docked
      ? "transition-[translate]"
      : "invisible -translate-x-full transition-[translate,visibility]",
  );
}
// The navbar: always on mobile; on desktop only while the sidebar is hidden,
// opening and closing its height (grid rows 0fr ↔ 1fr) as it fades. There its
// menu button only keeps the corner free for the sidebar's toggle.
function navbarClasses(docked: boolean) {
  return cx(
    "grid grid-rows-[1fr] duration-300 ease-in-out motion-reduce:transition-none",
    docked
      ? "lg:invisible lg:grid-rows-[0fr] lg:opacity-0 lg:transition-[grid-template-rows,opacity,visibility]"
      : "lg:transition-[grid-template-rows,opacity]",
  );
}
// SidebarLayout: a sidebar docked on large screens (which <SidebarToggle>
// hides and shows, with a navbar while it's hidden; remembered on this
// device), a navbar with a slide-over sidebar on small screens, and the page
// content in an inset card.
export function SidebarLayout({
  navbar,
  sidebar,
  children,
}: {
  navbar: ReactNode;
  sidebar: ReactNode;
  children: ReactNode;
}) {
  const [showSidebar, setShowSidebar] = useState(false);
  // Opening another page closes the mobile sidebar, including from a menu in
  // it (Workspace settings, Account & security, another workspace); its own
  // links close it as they're clicked.
  const { pathname } = useLocation();
  const [shownFor, setShownFor] = useState(pathname);
  if (pathname !== shownFor) {
    setShownFor(pathname);
    setShowSidebar(false);
  }
  const { docked, toggle } = useDockedSidebar();
  return (
    <div className="relative isolate flex min-h-svh w-full flex-col bg-white lg:bg-zinc-100 dark:bg-zinc-900 dark:lg:bg-zinc-950">
      <a
        href="#main"
        className="fixed top-2 left-2 z-50 -translate-y-20 rounded-lg bg-white px-3 py-2 text-sm font-medium shadow-sm ring-1 ring-zinc-950/10 focus:translate-y-0 dark:bg-zinc-900 dark:ring-white/10"
      >
        Skip to content
      </a>
      {/* Sidebar on desktop */}
      <div className={desktopSidebarClasses(docked)}>
        <SidebarToggleContext.Provider
          value={{
            label: docked ? "Hide sidebar" : "Show sidebar",
            icon: <SidebarIcon />,
            onClick: toggle,
            className: desktopToggleClasses(docked),
          }}
        >
          {sidebar}
        </SidebarToggleContext.Provider>
      </div>
      {/* Sidebar on mobile */}
      <MobileSidebar open={showSidebar} close={() => setShowSidebar(false)}>
        {sidebar}
      </MobileSidebar>
      {/* Navbar on mobile, and on desktop while the sidebar is hidden */}
      <header className={navbarClasses(docked)}>
        <div className="flex min-h-0 items-center overflow-hidden px-4">
          <div className="py-2.5 lg:invisible">
            <NavbarItem
              onClick={() => setShowSidebar(true)}
              aria-label="Open navigation"
            >
              <OpenMenuIcon />
            </NavbarItem>
          </div>
          <div className="min-w-0 flex-1">{navbar}</div>
        </div>
      </header>
      {/* Content */}
      <main
        id="main"
        className={cx(
          "flex flex-1 flex-col pb-2 transition-[padding] duration-300 ease-in-out motion-reduce:transition-none lg:min-w-0 lg:pt-2 lg:pr-2",
          docked ? "lg:pl-64" : "lg:pl-2",
        )}
      >
        <div className="grow p-6 lg:rounded-lg lg:bg-white lg:p-10 lg:shadow-xs lg:ring-1 lg:ring-zinc-950/5 dark:lg:bg-zinc-900 dark:lg:ring-white/10">
          <div className="mx-auto max-w-6xl">{children}</div>
        </div>
      </main>
    </div>
  );
}
