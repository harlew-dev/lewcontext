import type { Sort } from "@/lib/pagination";
export const statuses = ["In progress", "Planned", "Completed"] as const;
export const colors = ["violet", "blue", "amber", "emerald", "rose"] as const;
export type Project = {
  id: number;
  name: string;
  description: string;
  status: (typeof statuses)[number];
  color: (typeof colors)[number];
  createdAt: string;
  updatedAt: string;
};
export type ProjectInput = Pick<
  Project,
  "name" | "description" | "status" | "color"
>;
// Fields the API can sort projects by; "-field" sorts descending. Mirrors the
// allowlist in apps/api/internal/store/projects.go.
export const sortable = ["name", "status", "updated", "created"] as const;
export type ProjectSortField = (typeof sortable)[number];
export type ProjectSort = Sort<ProjectSortField>;
export const defaultSort: ProjectSort = "-updated";
export type StatusFilter = "all" | Project["status"];
// The projects page state kept in its URL.
export type ProjectQuery = {
  page: number;
  q: string;
  status: StatusFilter;
  sort: ProjectSort;
};
