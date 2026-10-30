import { useContext } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SidebarToggle } from "@/components/ui/sidebar";
import { SidebarToggleContext } from "@/components/ui/sidebar-context";
import { sidebarStorageKey } from "@/lib/use-docked-sidebar";
import { SidebarLayout } from "./sidebar-layout";
// Renders the layout with a toggle in its sidebar. `labels` collects the
// desktop toggle's label on every render, so a test can see the first one.
function renderLayout(labels: string[] = []) {
  function Probe() {
    const toggle = useContext(SidebarToggleContext);
    if (toggle) labels.push(toggle.label);
    return null;
  }
  return render(
    <SidebarLayout
      navbar={<span>Navbar</span>}
      sidebar={
        <div>
          <SidebarToggle />
          <Probe />
        </div>
      }
    >
      Content
    </SidebarLayout>,
  );
}
afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});
describe("<SidebarLayout> desktop sidebar", () => {
  it("is docked until hidden, and remembers hiding on this device", () => {
    renderLayout();
    fireEvent.click(screen.getByRole("button", { name: "Hide sidebar" }));
    expect(screen.getByRole("button", { name: "Show sidebar" })).toBeVisible();
    expect(localStorage.getItem(sidebarStorageKey)).toBe("hidden");
    fireEvent.click(screen.getByRole("button", { name: "Show sidebar" }));
    expect(screen.getByRole("button", { name: "Hide sidebar" })).toBeVisible();
    expect(localStorage.getItem(sidebarStorageKey)).toBeNull();
  });
  it("opens hidden from its first render when it was hidden last time", () => {
    localStorage.setItem(sidebarStorageKey, "hidden");
    const labels: string[] = [];
    renderLayout(labels);
    // Never docked for a moment and then hidden: that would flash the sidebar.
    expect(labels[0]).toBe("Show sidebar");
    expect(labels).not.toContain("Hide sidebar");
  });
  it("stays docked, and still toggles, when storage is unavailable", () => {
    const denied = () => {
      throw new DOMException("Storage is disabled", "SecurityError");
    };
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(denied);
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(denied);
    renderLayout();
    fireEvent.click(screen.getByRole("button", { name: "Hide sidebar" }));
    expect(screen.getByRole("button", { name: "Show sidebar" })).toBeVisible();
  });
});
