import { createContext, useContext, type ReactNode } from "react";
// Lets sidebar links close the mobile sidebar after navigating.
export const SidebarCloseContext = createContext<() => void>(() => {});
export function useCloseSidebar() {
  return useContext(SidebarCloseContext);
}
// What <SidebarToggle> does where it's rendered: close the mobile sidebar,
// or hide the desktop one. Nothing outside a SidebarLayout.
export type SidebarToggleAction = {
  label: string;
  icon: ReactNode;
  onClick: () => void;
  // Placement from the layout (the desktop toggle glides to the navbar).
  className?: string;
};
export const SidebarToggleContext = createContext<SidebarToggleAction | null>(
  null,
);
