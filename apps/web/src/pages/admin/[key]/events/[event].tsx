import { useParams } from "react-router";
import { ChevronLeftIcon } from "@heroicons/react/16/solid";
import {
  DeliveryStatusBadge,
  DeliveryTime,
} from "@/components/app/admin/admin-list";
import {
  AccountLink,
  EventPayload,
  EventTable,
  IpLink,
  WorkspaceLink,
} from "@/components/app/admin/events";
import { Button } from "@/components/ui/button";
import {
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
} from "@/components/ui/description-list";
import { EmptyState, ListSkeleton, Notice } from "@/components/ui/feedback";
import { Heading, Subheading } from "@/components/ui/heading";
import { Link } from "@/components/ui/link";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Code, Text, TextLink } from "@/components/ui/text";
import { adminPaths } from "@/lib/api/admin";
import { useAdminKey } from "@/lib/admin";
import { formatDateTime } from "@/lib/format";
import { parsePublicId } from "@/lib/ids";
import { adminRoutes } from "@/lib/admin-routes";
import { useAdminResource } from "@/lib/use-admin-resource";
import type {
  AdminDelivery,
  AdminEventDetail,
  EventFilters,
} from "@/types/admin";
// One event: who, what and from where, its payload as stored, its
// deliveries to listeners, and the events it's linked with. Opening it is
// recorded (admin.event.viewed).
export default function AdminEventShow() {
  const id = parsePublicId("evt", useParams().event);
  if (id === null) return <EventNotFound />;
  return <EventContent key={id} id={id} />;
}
function EventContent({ id }: { id: string }) {
  const key = useAdminKey();
  const event = useAdminResource(adminPaths.events.show(id));
  if (event.missing) return <EventNotFound />;
  return (
    <>
      <Link
        href={adminRoutes.events.index(key)}
        className="inline-flex items-center gap-2 text-sm/6 text-zinc-500 dark:text-zinc-400"
      >
        <ChevronLeftIcon className="size-4 fill-zinc-400 dark:fill-zinc-500" />
        Events
      </Link>
      {event.error && (
        <Notice variant="error" className="mt-6" onRetry={event.refresh}>
          {event.error}
        </Notice>
      )}
      {event.data ? (
        <EventDetails event={event.data} />
      ) : (
        event.loading && <ListSkeleton label="Loading event" rows={1} />
      )}
    </>
  );
}
function EventDetails({ event }: { event: AdminEventDetail }) {
  return (
    <>
      <Heading className="mt-4 break-all lg:mt-8">{event.name}</Heading>
      <Text className="mt-1">{formatDateTime(event.createdAt)}</Text>
      <div className="mt-12">
        <Subheading>Event</Subheading>
        <DescriptionList className="mt-4">
          <DescriptionTerm>ID</DescriptionTerm>
          <DescriptionDetails className="font-mono">
            {event.id}
          </DescriptionDetails>
          <DescriptionTerm>Actor</DescriptionTerm>
          <DescriptionDetails>
            {event.actor ? <AccountLink account={event.actor} /> : "None"}
          </DescriptionDetails>
          <DescriptionTerm>Account</DescriptionTerm>
          <DescriptionDetails>
            {event.user ? <AccountLink account={event.user} /> : "None"}
          </DescriptionDetails>
          <DescriptionTerm>Workspace</DescriptionTerm>
          <DescriptionDetails>
            {event.workspace ? (
              <WorkspaceLink workspace={event.workspace} />
            ) : (
              "None"
            )}
          </DescriptionDetails>
          <DescriptionTerm>IP address</DescriptionTerm>
          <DescriptionDetails>
            {event.ip ? (
              <IpLink ip={event.ip} />
            ) : (
              "None (not recorded during a request)"
            )}
          </DescriptionDetails>
          <DescriptionTerm>User agent</DescriptionTerm>
          <DescriptionDetails className="break-all">
            {event.userAgent || "None"}
          </DescriptionDetails>
        </DescriptionList>
      </div>
      <div className="mt-12">
        <Subheading>Payload</Subheading>
        <EventPayload event={event} className="mt-4" />
      </div>
      <MoreLikeThis event={event} />
      {event.deliveries.length > 0 && (
        <Deliveries deliveries={event.deliveries} />
      )}
      {event.related.length > 0 && (
        <div className="mt-12">
          <Subheading>Related events</Subheading>
          <EventTable events={event.related} className="mt-4" />
        </div>
      )}
    </>
  );
}
// Links to the Events list filtered like this event, over the last 7 days:
// the same name, the same IP, the same account.
function MoreLikeThis({ event }: { event: AdminEventDetail }) {
  const key = useAdminKey();
  const account = event.actor ?? event.user;
  const links: { label: string; filters: EventFilters }[] = [
    { label: `Events named ${event.name}`, filters: { names: [event.name] } },
    ...(event.ip
      ? [{ label: `Events from ${event.ip}`, filters: { ip: event.ip } }]
      : []),
    ...(account
      ? [
          {
            label: `Events by ${account.name || account.id}`,
            filters: { account: account.id },
          },
        ]
      : []),
  ];
  return (
    <div className="mt-12">
      <Subheading>More like this</Subheading>
      <ul className="mt-4 space-y-2 text-sm/6">
        {links.map(({ label, filters }) => (
          <li key={label}>
            <TextLink
              href={adminRoutes.events.index(key, { ...filters, window: "7d" })}
            >
              {label}
            </TextLink>
            <span className="text-zinc-500 dark:text-zinc-400">
              {" "}
              in the last 7 days
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}
function Deliveries({ deliveries }: { deliveries: AdminDelivery[] }) {
  return (
    <div className="mt-12">
      <Subheading>Deliveries</Subheading>
      <Table dense className="mt-4">
        <TableHead>
          <TableRow>
            <TableHeader>Listener</TableHeader>
            <TableHeader>Status</TableHeader>
            <TableHeader>Attempts</TableHeader>
            <TableHeader className="max-sm:hidden">When</TableHeader>
            <TableHeader className="max-md:hidden">Last error</TableHeader>
          </TableRow>
        </TableHead>
        <TableBody>
          {deliveries.map((d) => (
            <TableRow key={d.listener}>
              <TableCell>
                <Code>{d.listener}</Code>
              </TableCell>
              <TableCell>
                <DeliveryStatusBadge status={d.status} />
              </TableCell>
              <TableCell className="tabular-nums">{d.attempts}</TableCell>
              <TableCell className="text-zinc-500 max-sm:hidden">
                <DeliveryTime delivery={d} />
              </TableCell>
              <TableCell className="min-w-48 max-w-xs whitespace-normal wrap-anywhere text-zinc-500 max-md:hidden">
                {d.lastError || "–"}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
function EventNotFound() {
  const key = useAdminKey();
  return (
    <EmptyState
      title="This event has wandered off."
      description="It was pruned after 90 days, or the link is mistyped."
      action={
        <Button href={adminRoutes.events.index(key)}>Back to events</Button>
      }
    />
  );
}
