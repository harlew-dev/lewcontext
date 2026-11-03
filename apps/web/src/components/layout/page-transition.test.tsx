import { fireEvent, render, screen } from "@testing-library/react";
import { Link, Outlet, createRoutesStub } from "react-router";
import { describe, expect, it } from "vitest";
import { usePageKey, type TransitionHandle } from "./page-transition";
// Shows the key the app layout would give PageTransition.
function KeyProbe() {
  return (
    <>
      <p>Key {usePageKey()}</p>
      <Link to="/settings/members">Members</Link>
      <Link to="/projects">Projects</Link>
      <Outlet />
    </>
  );
}
const tabGroup: TransitionHandle = { tabGroup: true };
function renderAt(path: string) {
  const Stub = createRoutesStub([
    {
      Component: KeyProbe,
      children: [
        {
          path: "/settings",
          handle: tabGroup,
          Component: Outlet,
          children: [
            { index: true, Component: () => <p>General</p> },
            { path: "members", Component: () => <p>Members tab</p> },
          ],
        },
        { path: "/projects", Component: () => <p>Projects page</p> },
      ],
    },
  ]);
  render(<Stub initialEntries={[path]} />);
}
describe("usePageKey", () => {
  it("gives a tab group's tabs one key, so switching tabs isn't a new page", async () => {
    renderAt("/settings");
    expect(screen.getByText("Key /settings")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("link", { name: "Members" }));
    expect(await screen.findByText("Members tab")).toBeInTheDocument();
    expect(screen.getByText("Key /settings")).toBeInTheDocument();
  });
  it("keys any other page by its path", async () => {
    renderAt("/settings");
    fireEvent.click(screen.getByRole("link", { name: "Projects" }));
    expect(await screen.findByText("Projects page")).toBeInTheDocument();
    expect(screen.getByText("Key /projects")).toBeInTheDocument();
  });
});
