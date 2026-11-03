// A workspace's log of changes as owners and admins read it
// (GET /api/workspaces/{workspace}/events, docs/api/events.md#the-workspace-log).

// An account in an entry. `name` is missing for an account that is gone.
export type EventRef = { id: string; name?: string };
// One change. `details` is what the API chose to show of the event: public
// IDs, workspace content and the changes' from/to, never an IP or a browser.
export type WorkspaceEvent = {
  id: string;
  name: string;
  createdAt: string;
  // Who did it. Null for a workspace API key's writes (`apiKey` names it),
  // or once the account is deleted.
  actor: EventRef | null;
  apiKey?: string;
  // Whom it is about: the member whose role changed, a personal key's owner.
  subject: EventRef | null;
  // The request it was recorded in (req_…): the reference in a failed
  // request's message and the server's logs. Several changes can share one.
  requestId: string;
  details: Record<string, unknown>;
};
// One change, with the others recorded in the same request.
export type EventDetail = WorkspaceEvent & { related: WorkspaceEvent[] };
export type EventPage = {
  data: WorkspaceEvent[];
  // The cursor of the next, older page; null on the last.
  next: string | null;
  // The names behind the filters.
  refs: {
    actor: EventRef | null;
    subject: EventRef | null;
    project: EventRef | null;
  };
};
// The types the Logs filter offers: each group and the events in it. Mirrors
// events.WorkspaceLogGroups in apps/api/internal/events/workspace_log.go (a
// Vitest test reads the Go list and compares).
export const eventTypes = [
  {
    id: "workspace",
    label: "Workspace",
    names: ["workspace.created", "workspace.renamed", "workspace.deleted"],
  },
  {
    id: "members",
    label: "Members",
    names: [
      "workspace.member.updated",
      "workspace.member.removed",
      "workspace.member.left",
    ],
  },
  {
    id: "invitations",
    label: "Invitations",
    names: [
      "workspace.invitation.created",
      "workspace.invitation.revoked",
      "workspace.invitation.accepted",
    ],
  },
  {
    id: "projects",
    label: "Projects",
    names: ["project.created", "project.updated", "project.deleted"],
  },
  {
    id: "api-keys",
    label: "API keys",
    names: [
      "workspace.api_key.created",
      "workspace.api_key.updated",
      "workspace.api_key.revoked",
      "auth.api_key.created",
      "auth.api_key.updated",
      "auth.api_key.revoked",
    ],
  },
] as const;
// The periods the Logs page offers beside a custom range; workspace changes
// are kept 90 days.
export const eventWindows = [
  { id: "24h", label: "Last 24 hours" },
  { id: "7d", label: "Last 7 days" },
  { id: "30d", label: "Last 30 days" },
  { id: "all", label: "Everything kept" },
] as const;
export type EventWindow = (typeof eventWindows)[number]["id"];
export const defaultEventWindow: EventWindow = "7d";
// The Logs state in its URL (?type=&actor=&project=&q=&window=&from=&to=&before=).
// `types` are group IDs and event names; `from` and `to` (RFC 3339) replace
// the window as a custom range.
export type EventQuery = {
  types: string[];
  actor: string;
  project: string;
  q: string;
  window: EventWindow;
  from: string;
  to: string;
  before: string;
};
