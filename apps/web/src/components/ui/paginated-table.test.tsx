import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it } from "vitest";
import type { Page } from "@/lib/pagination";
import { PaginatedTable } from "./paginated-table";
import { TableCell, TableHeader, TableRow } from "./table";
afterEach(cleanup);
type Item = { id: number; name: string };
function result(current: number, total: number, data: Item[]): Page<Item> {
  return {
    data,
    pages: { current, total, perPage: 2, items: total * 2 - 1 },
  };
}
function renderTable(page: Page<Item> | undefined, loading = false) {
  return render(
    <PaginatedTable<Item>
      page={page}
      loading={loading}
      label="Items"
      head={
        <TableRow>
          <TableHeader href="/items?sort=-name" sort="ascending">
            Name
          </TableHeader>
        </TableRow>
      }
      row={(item) => (
        <TableRow href={`/items/${item.id}`} title={item.name}>
          <TableCell>{item.name}</TableCell>
        </TableRow>
      )}
      rowKey={(item) => item.id}
      href={(n) => `/items?page=${n}`}
      empty={<p>Nothing yet</p>}
    />,
    { wrapper: MemoryRouter },
  );
}
describe("PaginatedTable", () => {
  it("shows a skeleton before the first page loads", () => {
    renderTable(undefined, true);
    expect(
      screen.getByRole("status", { name: "Loading items" }),
    ).toBeInTheDocument();
  });
  it("renders nothing when the first page failed", () => {
    const { container } = renderTable(undefined, false);
    expect(container).toBeEmptyDOMElement();
  });
  it("shows the empty state when nothing matches", () => {
    renderTable({
      data: [],
      pages: { current: 1, total: 1, perPage: 2, items: 0 },
    });
    expect(screen.getByText("Nothing yet")).toBeInTheDocument();
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
  });
  it("links rows and pages, marking the current page", () => {
    renderTable(result(2, 5, [{ id: 7, name: "Seven" }]));
    expect(screen.getAllByRole("link", { name: "Seven" })[0]).toHaveAttribute(
      "href",
      "/items/7",
    );
    expect(screen.getByRole("columnheader")).toHaveAttribute(
      "aria-sort",
      "ascending",
    );
    const nav = screen.getByRole("navigation", { name: "Items pages" });
    expect(within(nav).getByRole("link", { name: "Page 2" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(
      within(nav).getByRole("link", { name: "Previous page" }),
    ).toHaveAttribute("href", "/items?page=1");
    expect(
      within(nav).getByRole("link", { name: "Next page" }),
    ).toHaveAttribute("href", "/items?page=3");
  });
  it("disables Previous on the first page and hides a single page selector", () => {
    renderTable(result(1, 3, [{ id: 1, name: "One" }]));
    expect(
      screen.getByRole("button", { name: "Previous page" }),
    ).toBeDisabled();
    cleanup();
    renderTable(result(1, 1, [{ id: 1, name: "One" }]));
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
  });
  it("offers the last page from past the end", () => {
    renderTable(result(9, 3, []));
    expect(screen.getByText("There’s nothing on page 9")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Go to last page" }),
    ).toHaveAttribute("href", "/items?page=3");
    expect(screen.getByRole("link", { name: "Previous page" })).toHaveAttribute(
      "href",
      "/items?page=3",
    );
  });
  it("keeps the previous page visible while the next loads", () => {
    const { container } = renderTable(
      result(1, 3, [{ id: 1, name: "One" }]),
      true,
    );
    expect(screen.getByText("One")).toBeInTheDocument();
    expect(container.firstElementChild).toHaveAttribute("aria-busy", "true");
  });
});
