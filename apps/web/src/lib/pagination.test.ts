import { describe, expect, it } from "vitest";
import {
  page,
  pageRange,
  sort,
  sortDirection,
  toggleSort,
  url,
  type Pages,
} from "./pagination";
const pages = (current: number, total: number): Pages => ({
  current,
  total,
  perPage: 20,
  items: total * 20,
});
describe("page", () => {
  it("reads positive page numbers and falls back to page 1", () => {
    expect(page({ page: "3" })).toBe(3);
    expect(page({ page: "10000" })).toBe(10000);
    for (const value of ["0", "-1", "+2", " 2", "1e3", "abc", "10001", ""])
      expect(page({ page: value })).toBe(1);
    expect(page({ page: ["2", "3"] })).toBe(1);
    expect(page({})).toBe(1);
  });
});
describe("sort", () => {
  const fields = ["name", "updated"] as const;
  it("accepts allowed fields in either direction", () => {
    expect(sort({ sort: "name" }, fields, "-updated")).toBe("name");
    expect(sort({ sort: "-name" }, fields, "-updated")).toBe("-name");
  });
  it("falls back for anything else", () => {
    for (const value of ["user_id", "--name", "name;", "", "Name"])
      expect(sort({ sort: value }, fields, "-updated")).toBe("-updated");
  });
  it("reports and toggles direction", () => {
    expect(sortDirection("name", "name")).toBe("ascending");
    expect(sortDirection("-name", "name")).toBe("descending");
    expect(sortDirection("-updated", "name")).toBeUndefined();
    expect(toggleSort("name", "name")).toBe("-name");
    expect(toggleSort("-name", "name")).toBe("name");
    expect(toggleSort("-updated", "name")).toBe("name");
    expect(toggleSort("name", "updated", "descending")).toBe("-updated");
  });
});
describe("url", () => {
  it("encodes values and leaves out empty ones and page 1", () => {
    expect(url("/api/projects", { page: 1, q: "", status: undefined })).toBe(
      "/api/projects",
    );
    expect(
      url("/api/projects", { q: "50% & more", status: "In progress", page: 2 }),
    ).toBe("/api/projects?q=50%25+%26+more&status=In+progress&page=2");
  });
  it("round-trips through page()", () => {
    const query = Object.fromEntries(
      new URLSearchParams(url("/x", { page: 7 }).split("?")[1]),
    );
    expect(page(query)).toBe(7);
  });
});
describe("pageRange", () => {
  it("lists every page when there are few", () => {
    expect(pageRange(pages(1, 1))).toEqual([1]);
    expect(pageRange(pages(3, 5))).toEqual([1, 2, 3, 4, 5]);
  });
  it("collapses long runs into gaps around the current page", () => {
    expect(pageRange(pages(1, 50))).toEqual([1, 2, "gap", 50]);
    expect(pageRange(pages(25, 50))).toEqual([1, "gap", 24, 25, 26, "gap", 50]);
    expect(pageRange(pages(50, 50))).toEqual([1, "gap", 49, 50]);
  });
  it("shows a single hidden page instead of a gap", () => {
    expect(pageRange(pages(4, 7))).toEqual([1, 2, 3, 4, 5, 6, 7]);
  });
  it("clamps a page past the end", () => {
    expect(pageRange(pages(9, 3))).toEqual([1, 2, 3]);
  });
});
