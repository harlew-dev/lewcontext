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
// What happened, after the person's name, as text and the API key's ID where
// the renderer may link it: "renamed the project “Website”", or "created the
// API key " followed by the key's ID.
export type EventSentencePart =
  { text: string } | { key: string; personal: boolean };
function keySentence(
  details: Record<string, unknown>,
  before: string,
  after: string,
  personal: boolean,
): EventSentencePart[] {
  const key = text(details.key);
  if (!key) return [{ text: `${before}${after}`.trim() }];
  return [
    { text: before },
    { key, personal },
    ...(after ? [{ text: after }] : []),
  ];
}
export function eventSentenceParts(event: WorkspaceEvent): EventSentencePart[] {
  const d = event.details;
  const subject = event.subject?.name ?? "a member";
  const address = text(d.email) || "an address";
  const role = text(d.role);
  switch (event.name) {
    case "workspace.created":
      return [{ text: `created the workspace ${quoted(d.name)}`.trim() }];
    case "workspace.renamed":
      return [{ text: "renamed the workspace" }];
    case "workspace.deleted":
      return [{ text: "deleted the workspace" }];
    case "workspace.member.updated":
      return [{ text: `changed ${subject}’s role` }];
    case "workspace.member.removed":
      return [
        {
          text: `removed ${subject}${role ? ` (${role})` : ""} from the workspace`,
        },
      ];
    case "workspace.member.left":
      return [{ text: "left the workspace" }];
    case "workspace.invitation.created":
      return [{ text: `invited ${address}${role ? ` as ${role}` : ""}` }];
    case "workspace.invitation.revoked":
      return [{ text: `revoked the invitation to ${address}` }];
    case "workspace.invitation.accepted":
      return [
        {
          text: `accepted an invitation${role ? ` and joined as ${role}` : ""}`,
        },
      ];
    case "project.created":
      return [{ text: `created the project ${quoted(d.name)}`.trim() }];
    case "project.updated":
      return [{ text: "updated a project" }];
    case "project.deleted":
      return [{ text: `deleted the project ${quoted(d.name)}`.trim() }];
    case "workspace.api_key.created":
      return keySentence(d, "created the API key ", "", false);
    case "workspace.api_key.updated":
      return keySentence(d, "changed the API key ", "", false);
    case "workspace.api_key.revoked":
      return keySentence(d, "revoked the API key ", "", false);
    case "auth.api_key.created":
      return keySentence(
        d,
        "created a personal API key ",
        " for this workspace",
        true,
      );
    case "auth.api_key.updated":
      return keySentence(d, "changed a personal API key ", "", true);
    case "auth.api_key.revoked":
      return keySentence(d, "revoked a personal API key ", "", true);
    default:
      return [{ text: event.name }];
  }
}
// The same sentence as plain text, for a form's or list row's accessible
// name: "Ada Lovelace created the API key apk_…".
export function eventSentence(event: WorkspaceEvent): string {
  return eventSentenceParts(event)
    .map((part) => ("text" in part ? part.text : part.key))
    .join("");
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
// A detail row's field, for the ones whose value is a public ID the renderer
// may link (the key, project and member IDs), or none for a change and text.
export type DetailRow = { field?: string; label: string; value: string };
// The details as label/value rows, changes first as from → to. A value is
// text. An address the retention rules removed says so.
export function detailRows(details: Record<string, unknown>): DetailRow[] {
  const rows: DetailRow[] = changeList(details).map((c) => ({
    label: `${labels[c.field] ?? c.field} changed`,
    value: `${c.from} → ${c.to}`,
  }));
  for (const field of order) {
    const value = details[field];
    if (value === undefined || value === null || value === "") continue;
    rows.push({
      field,
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
