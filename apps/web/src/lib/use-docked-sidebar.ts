import { useState } from "react";
// Where this device remembers that the desktop sidebar is hidden. Docked is
// the default, so only "hidden" is stored.
export const sidebarStorageKey = "forma:sidebar";
// Read while rendering, so the first paint already has the remembered state:
// the app layout renders only in the browser (never prerendered, so nothing
// to hydrate), and no effect flips it after mount, which would flash the
// sidebar and play its slide. Storage can be unavailable (blocked, or a
// private window), and then the sidebar is docked.
function readDocked() {
  try {
    return localStorage.getItem(sidebarStorageKey) !== "hidden";
  } catch {
    return true;
  }
}
function writeDocked(docked: boolean) {
  try {
    if (docked) localStorage.removeItem(sidebarStorageKey);
    else localStorage.setItem(sidebarStorageKey, "hidden");
  } catch {
    // Not remembered, but the toggle still works for this visit.
  }
}
// Whether the desktop sidebar is docked, remembered across visits on this
// device (a layout preference, not user data).
export function useDockedSidebar() {
  const [docked, setDocked] = useState(readDocked);
  function toggle() {
    writeDocked(!docked);
    setDocked(!docked);
  }
  return { docked, toggle };
}
