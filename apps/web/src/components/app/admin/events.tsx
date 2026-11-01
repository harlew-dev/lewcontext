import type { ReactNode } from "react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Code, TextLink } from "@/components/ui/text";
import { useAdminKey } from "@/lib/admin";
import { formatDateTime } from "@/lib/format";
import { adminRoutes } from "@/lib/admin-routes";
import type { AdminEvent, AdminRef } from "@/types/admin";
// Shared by the admin dashboard's Events list and an event's page (its
// related events): the events table, a row's one-line summary, and links to
// what an event names.

// Payload fields worth a glance, in the order a summary shows them.
const summaryFields = [
  "result",
  "limit",
  "step",
  "method",
  "role",
  "status",
  "reason",
  "via",
  "email",
  "name",
];
// How many of them a summary shows at most.
const summaryLength = 3;

// A line saying what happened, from the payload: "wrong_password ·
// ada@example.com", "auth:per-ip:block", "name: Acme → Acme Inc". Only
// text: React escapes it like any other value.
export function eventSummary(event: AdminEvent): string {
  const p = event.payload;
  const parts: string[] = [];
  if (typeof p.ok === "boolean") parts.push(p.ok ? "ok" : "failed");
  for (const field of summaryFields) {
    const value = p[field];
    if (typeof value === "string" && value !== "") parts.push(value);
  }
  const changes = p.changes;
  if (changes && typeof changes === "object") {
    for (const [field, change] of Object.entries(
      changes as Record<string, { from?: unknown; to?: unknown }>,
    )) {
      parts.push(`${field}: ${String(change.from)} → ${String(change.to)}`);
    }
  }
  return parts.slice(0, summaryLength).join(" · ");
}
// An account in an event: its page, or its ID alone once deleted.
export function AccountLink({ account }: { account: AdminRef }) {
  const key = useAdminKey();
  if (!account.name) return <DeletedRef id={account.id} />;
  return (
    <TextLink href={adminRoutes.users.show(key, account.id)}>
      {account.name}
    </TextLink>
  );
}
// A workspace in an event: its page, or its ID alone once deleted.
export function WorkspaceLink({ workspace }: { workspace: AdminRef }) {
  const key = useAdminKey();
  if (!workspace.name) return <DeletedRef id={workspace.id} />;
  return (
    <TextLink href={adminRoutes.workspaces.show(key, workspace.id)}>
      {workspace.name}
    </TextLink>
  );
}
function DeletedRef({ id }: { id: string }) {
  return (
    <span className="text-zinc-500">
      <Code>{id}</Code> (deleted)
    </span>
  );
}
// An IP address, linking to the events from it.
export function IpLink({ ip }: { ip: string }) {
  const key = useAdminKey();
  return (
    <TextLink
      href={adminRoutes.events.index(key, { ip })}
      className="font-mono"
    >
      {ip}
    </TextLink>
  );
}
// Events, newest first. The time opens the event; every account, workspace
// and IP links to its own page or filter.
export function EventTable({
  events,
  className,
}: {
  events: AdminEvent[];
  className?: string;
}) {
  return (
    <Table dense className={className}>
      <TableHead>
        <TableRow>
          <TableHeader>Time</TableHeader>
          <TableHeader>Event</TableHeader>
          <TableHeader className="max-sm:hidden">Actor</TableHeader>
          <TableHeader className="max-md:hidden">About</TableHeader>
          <TableHeader className="max-lg:hidden">IP</TableHeader>
        </TableRow>
      </TableHead>
      <TableBody>
        {events.map((event) => (
          <EventRow key={event.id} event={event} />
        ))}
      </TableBody>
    </Table>
  );
}
function EventRow({ event }: { event: AdminEvent }) {
  const key = useAdminKey();
  const summary = eventSummary(event);
  return (
    <TableRow>
      <TableCell className="whitespace-nowrap">
        <TextLink href={adminRoutes.events.show(key, event.id)}>
          {formatDateTime(event.createdAt)}
        </TextLink>
      </TableCell>
      <TableCell className="max-w-xs">
        <div className="font-medium">{event.name}</div>
        {summary && (
          <div className="truncate text-zinc-500 dark:text-zinc-400">
            {summary}
          </div>
        )}
      </TableCell>
      <TableCell className="max-sm:hidden">
        {event.actor ? <AccountLink account={event.actor} /> : <None />}
      </TableCell>
      <TableCell className="max-md:hidden">
        <Stacked>
          {event.user && <AccountLink account={event.user} />}
          {event.workspace && <WorkspaceLink workspace={event.workspace} />}
        </Stacked>
      </TableCell>
      <TableCell className="max-lg:hidden">
        {event.ip ? <IpLink ip={event.ip} /> : <None />}
      </TableCell>
    </TableRow>
  );
}
// Several references, one per line; a dash when there are none.
function Stacked({ children }: { children: ReactNode[] }) {
  const shown = children.filter(Boolean);
  if (shown.length === 0) return <None />;
  return <div className="flex flex-col">{shown}</div>;
}
function None() {
  return <span className="text-zinc-400 dark:text-zinc-500">–</span>;
}
