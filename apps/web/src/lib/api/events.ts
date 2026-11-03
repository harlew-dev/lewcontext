import { url } from "@/lib/pagination";
import type { EventDetail, EventPage } from "@/types/events";
import type { Http } from "./http";
import { path } from "./paths";
// The parameters GET /api/workspaces/{workspace}/events accepts. `name` is
// an event name or a type's ID; the rest narrow by person (`actor`: what
// they did, `subject`: whom it was about), project, text and time.
export type EventIndexQuery = {
  // Comma-separated event names and type IDs.
  name?: string;
  actor?: string;
  subject?: string;
  // What they did or what was done to them.
  account?: string;
  project?: string;
  // A key's own lifecycle and what it wrote.
  key?: string;
  q?: string;
  window?: string;
  from?: string;
  to?: string;
  before?: string;
  perPage?: number;
};
// Typed paths for one workspace's log (paths.events(workspace)).
export function eventRoutes(workspace: string) {
  const base = `/api/workspaces/${workspace}/events`;
  return {
    index: (query: EventIndexQuery = {}) => path<EventPage>(url(base, query)),
    show: (id: string) => path<EventDetail>(`${base}/${id}`),
    // Whether an API path (e.g. an SWR key) is one of this workspace's logs.
    matches: (key: string) => key === base || key.startsWith(`${base}?`),
  };
}
// Owners and admins only (workspace.event.index). The log is read only.
export function events(http: Http, workspace: string) {
  const routes = eventRoutes(workspace);
  return {
    index: (query: EventIndexQuery = {}) => http.get(routes.index(query)),
  };
}
// A personal key's own log, for its owner: /api/auth/api-keys/{key}/events
// (paths.apiKeyEvents(key)). Fewer filters than the workspace's.
export type ApiKeyEventIndexQuery = Pick<
  EventIndexQuery,
  "name" | "q" | "window" | "from" | "to" | "before" | "perPage"
>;
export function apiKeyEventRoutes(key: string) {
  const base = `/api/auth/api-keys/${key}/events`;
  return {
    index: (query: ApiKeyEventIndexQuery = {}) =>
      path<EventPage>(url(base, query)),
  };
}
