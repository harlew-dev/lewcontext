import { useState, type ReactNode } from "react";
import { Drawer } from "baseui/drawer";
import { slot } from "@/lib/baseui";
import { useScrollLock } from "@/lib/scroll-lock";
import { NavbarItem } from "@/components/ui/navbar";
import { SidebarCloseContext } from "@/components/ui/sidebar-context";
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
      <div className="-mb-3 px-4 pt-3">
        <NavbarItem onClick={close} aria-label="Close navigation">
          <CloseMenuIcon />
        </NavbarItem>
      </div>
      <SidebarCloseContext.Provider value={close}>
        {children}
      </SidebarCloseContext.Provider>
    </Drawer>
  );
}
// SidebarLayout: fixed sidebar on large screens, a navbar with a
// slide-over sidebar on small screens, and the page content in an inset card.
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
  return (
    <div className="relative isolate flex min-h-svh w-full bg-white max-lg:flex-col lg:bg-zinc-100 dark:bg-zinc-900 dark:lg:bg-zinc-950">
      <a
        href="#main"
        className="fixed top-2 left-2 z-50 -translate-y-20 rounded-lg bg-white px-3 py-2 text-sm font-medium shadow-sm ring-1 ring-zinc-950/10 focus:translate-y-0 dark:bg-zinc-900 dark:ring-white/10"
      >
        Skip to content
      </a>
      {/* Sidebar on desktop */}
      <div className="fixed inset-y-0 left-0 w-64 max-lg:hidden">{sidebar}</div>
      {/* Sidebar on mobile */}
      <MobileSidebar open={showSidebar} close={() => setShowSidebar(false)}>
        {sidebar}
      </MobileSidebar>
      {/* Navbar on mobile */}
      <header className="flex items-center px-4 lg:hidden">
        <div className="py-2.5">
          <NavbarItem
            onClick={() => setShowSidebar(true)}
            aria-label="Open navigation"
          >
            <OpenMenuIcon />
          </NavbarItem>
        </div>
        <div className="min-w-0 flex-1">{navbar}</div>
      </header>
      {/* Content */}
      <main
        id="main"
        className="flex flex-1 flex-col pb-2 lg:min-w-0 lg:pt-2 lg:pr-2 lg:pl-64"
      >
        <div className="grow p-6 lg:rounded-lg lg:bg-white lg:p-10 lg:shadow-xs lg:ring-1 lg:ring-zinc-950/5 dark:lg:bg-zinc-900 dark:lg:ring-white/10">
          <div className="mx-auto max-w-6xl">{children}</div>
        </div>
      </main>
    </div>
  );
}
