import { createContext, useContext } from "react";
// Lets sidebar links close the mobile sidebar after navigating.
export const SidebarCloseContext = createContext<() => void>(() => {});
export function useCloseSidebar() {
  return useContext(SidebarCloseContext);
}
