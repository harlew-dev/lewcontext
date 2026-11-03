import type { EventRef, WorkspaceEvent } from "@/types/events";
// Reading an entry of a workspace's log (docs/api/events.md#the-workspace-log):
// one sentence for what happened and the details' fields, from the
// projection the API sends. Everything is returned as plain text, which
// React escapes: names and descriptions are whatever members typed.

type Change = { from: string; to: string };
function text(value: unknown): string {
  return typeof value === "string" ? value : "";
}
function quoted(value: unknown): string {
  const name = text(value);
  return name ? `“${name}”` : "";
}
// The account's name, or what stands in for it.
export function whoIs(ref: EventRef | null, apiKey?: string): string {
  if (ref?.name) return ref.name;
  if (apiKey) return `API key ${apiKey}`;
  return "A deleted account";
}
// The changes an update holds, in field order.
function changeList(
  details: Record<string, unknown>,
): { field: string; from: string; to: string }[] {
  const changes = details.changes;
  if (!changes || typeof changes !== "object") return [];
  return Object.entries(changes as Record<string, Partial<Change>>).map(
    ([field, change]) => ({
      field,
      from: text(change?.from) || "none",
      to: text(change?.to) || "none",
    }),
  );
}
// The changes as lines, e.g. "status: Planned → Completed".
export function changesOf(details: Record<string, unknown>): string[] {
  return changeList(details).map((c) => `${c.field}: ${c.from} → ${c.to}`);
}
// What happened, after the person's name: "renamed the project “Website”".
export function eventSentence(event: WorkspaceEvent): string {
  const d = event.details;
  const subject = event.subject?.name ?? "a member";
  const address = text(d.email) || "an address";
  const role = text(d.role);
  switch (event.name) {
    case "workspace.created":
      return `created the workspace ${quoted(d.name)}`.trim();
    case "workspace.renamed":
      return "renamed the workspace";
    case "workspace.deleted":
      return "deleted the workspace";
    case "workspace.member.updated":
      return `changed ${subject}’s role`;
    case "workspace.member.removed":
      return `removed ${subject}${role ? ` (${role})` : ""} from the workspace`;
    case "workspace.member.left":
      return "left the workspace";
    case "workspace.invitation.created":
      return `invited ${address}${role ? ` as ${role}` : ""}`;
    case "workspace.invitation.revoked":
      return `revoked the invitation to ${address}`;
    case "workspace.invitation.accepted":
      return `accepted an invitation${role ? ` and joined as ${role}` : ""}`;
    case "project.created":
      return `created the project ${quoted(d.name)}`.trim();
    case "project.updated":
      return "updated a project";
    case "project.deleted":
      return `deleted the project ${quoted(d.name)}`.trim();
    case "workspace.api_key.created":
      return `created the API key ${text(d.key)}`.trim();
    case "workspace.api_key.updated":
      return `changed the API key ${text(d.key)}`.trim();
    case "workspace.api_key.revoked":
      return `revoked the API key ${text(d.key)}`.trim();
    case "auth.api_key.created":
      return `created a personal API key ${text(d.key)} for this workspace`;
    case "auth.api_key.updated":
      return `changed a personal API key ${text(d.key)}`;
    case "auth.api_key.revoked":
      return `revoked a personal API key ${text(d.key)}`;
    default:
      return event.name;
  }
}
// A short line of what changed, for under the sentence.
export function eventSummary(event: WorkspaceEvent): string {
  const changes = changesOf(event.details);
  if (changes.length > 0) return changes.join(" · ");
  const d = event.details;
  if (event.name === "project.created")
    return [text(d.status), text(d.description)].filter(Boolean).join(" · ");
  return "";
}
const labels: Record<string, string> = {
  name: "Name",
  description: "Description",
  status: "Status",
  color: "Colour",
  project: "Project",
  user: "Member",
  role: "Role",
  email: "Address",
  invitation: "Invitation",
  key: "API key",
  scopes: "Scopes",
  fields: "Changed",
  reason: "Reason",
  projects: "Projects",
  members: "Members",
};
// Field order in a list of details.
const order = Object.keys(labels);
// The details as label/value rows, changes first as from → to. A value is
// text. An address the retention rules removed says so.
export function detailRows(
  details: Record<string, unknown>,
): { label: string; value: string }[] {
  const rows = changeList(details).map((c) => ({
    label: `${labels[c.field] ?? c.field} changed`,
    value: `${c.from} → ${c.to}`,
  }));
  for (const field of order) {
    const value = details[field];
    if (value === undefined || value === null || value === "") continue;
    rows.push({
      label: labels[field],
      value: Array.isArray(value) ? value.join(", ") : String(value),
    });
  }
  if (details.emailRemoved === true)
    rows.push({ label: labels.email, value: "No longer kept" });
  return rows;
}
// An event's name in words, for pickers and titles ("Created project").
export const eventLabels: Record<string, string> = {
  "workspace.created": "Created workspace",
  "workspace.renamed": "Renamed workspace",
  "workspace.deleted": "Deleted workspace",
  "workspace.member.updated": "Changed member role",
  "workspace.member.removed": "Removed member",
  "workspace.member.left": "Member left",
  "workspace.invitation.created": "Invited someone",
  "workspace.invitation.revoked": "Revoked invitation",
  "workspace.invitation.accepted": "Accepted invitation",
  "project.created": "Created project",
  "project.updated": "Updated project",
  "project.deleted": "Deleted project",
  "workspace.api_key.created": "Created API key",
  "workspace.api_key.updated": "Changed API key",
  "workspace.api_key.revoked": "Revoked API key",
  "auth.api_key.created": "Created personal API key",
  "auth.api_key.updated": "Changed personal API key",
  "auth.api_key.revoked": "Revoked personal API key",
};
