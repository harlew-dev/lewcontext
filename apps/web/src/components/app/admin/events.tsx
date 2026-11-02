import { Fragment, useId, useState, type ReactNode } from "react";
import { ChevronRightIcon } from "@heroicons/react/16/solid";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { cx } from "@/lib/cx";
import { adminRoutes } from "@/lib/admin-routes";
import type { AdminEvent, AdminRef } from "@/types/admin";
import { formatEventTime } from "./event-filters";
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
// An IP address, linking to the events from it (`href`, or the last 7
// days of them).
export function IpLink({ ip, href }: { ip: string; href?: string }) {
  const key = useAdminKey();
  return (
    <TextLink
      href={href ?? adminRoutes.events.index(key, { ip, window: "7d" })}
      className="font-mono"
    >
      {ip}
    </TextLink>
  );
}
// Events, newest first. Each row opens in place to show its payload (the
// list already holds it: nothing more is loaded or recorded); the time
// opens the event's page; every account, workspace and IP links to its own
// page or filter. `ipHref` builds an IP's link (the Events list keeps its
// window and filters).
export function EventTable({
  events,
  ipHref,
  className,
}: {
  events: AdminEvent[];
  ipHref?: (ip: string) => string;
  className?: string;
}) {
  return (
    <Table dense className={className}>
      <TableHead>
        <TableRow>
          <TableHeader className="w-0">
            <span className="sr-only">Details</span>
          </TableHeader>
          <TableHeader>Time (UTC)</TableHeader>
          <TableHeader>Event</TableHeader>
          <TableHeader className="max-sm:hidden">Actor</TableHeader>
          <TableHeader className="max-md:hidden">About</TableHeader>
          <TableHeader className="max-lg:hidden">IP</TableHeader>
        </TableRow>
      </TableHead>
      <TableBody>
        {events.map((event) => (
          <EventRow key={event.id} event={event} ipHref={ipHref} />
        ))}
      </TableBody>
    </Table>
  );
}
function EventRow({
  event,
  ipHref,
}: {
  event: AdminEvent;
  ipHref?: (ip: string) => string;
}) {
  const key = useAdminKey();
  const detailsId = useId();
  const [open, setOpen] = useState(false);
  const summary = eventSummary(event);
  return (
    <Fragment>
      <TableRow className={cx(open && "*:border-b-0")}>
        <TableCell className="w-0 pr-0">
          <Button
            plain
            aria-expanded={open}
            aria-controls={detailsId}
            aria-label={`Details of ${event.name} at ${formatEventTime(event.createdAt)}`}
            onClick={() => setOpen(!open)}
            className="-my-1.5"
          >
            <ChevronRightIcon
              className={cx(
                "transition-transform motion-reduce:transition-none",
                open && "rotate-90",
              )}
            />
          </Button>
        </TableCell>
        <TableCell className="whitespace-nowrap tabular-nums">
          <TextLink href={adminRoutes.events.show(key, event.id)}>
            {formatEventTime(event.createdAt)}
          </TextLink>
        </TableCell>
        <TableCell className="max-w-xs">
          <div className="flex items-center gap-x-2 font-medium">
            {event.name}
            {event.failed && <Badge color="rose">Failed</Badge>}
          </div>
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
          {event.ip ? (
            <IpLink ip={event.ip} href={ipHref?.(event.ip)} />
          ) : (
            <None />
          )}
        </TableCell>
      </TableRow>
      {open && (
        <TableRow id={detailsId}>
          <TableCell colSpan={6} className="whitespace-normal">
            <EventPayload event={event} />
            <div className="mt-3 flex justify-end">
              <Button outline href={adminRoutes.events.show(key, event.id)}>
                Open event
              </Button>
            </div>
          </TableCell>
        </TableRow>
      )}
    </Fragment>
  );
}
// An event's payload as indented JSON, as text through React's escaping:
// names and emails may hold markup.
export function EventPayload({
  event,
  className,
}: {
  event: AdminEvent;
  className?: string;
}) {
  return (
    <pre
      className={cx(
        className,
        "overflow-x-auto rounded-lg bg-zinc-950/2.5 p-4 font-mono text-sm/6 text-zinc-950 ring-1 ring-zinc-950/5 dark:bg-white/5 dark:text-white dark:ring-white/10",
      )}
    >
      {JSON.stringify(event.payload, null, 2)}
    </pre>
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
