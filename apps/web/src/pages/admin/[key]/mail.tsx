import { useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import {
  ArrowPathIcon,
  EllipsisHorizontalIcon,
} from "@heroicons/react/16/solid";
import { AdminActionDialog } from "@/components/app/admin/admin-action-dialog";
import {
  DeliveryStatusBadge,
  DeliveryTime,
} from "@/components/app/admin/admin-list";
import { CursorNav } from "@/components/app/admin/cursor-nav";
import { Button } from "@/components/ui/button";
import {
  BarChart,
  type ChartRow,
  type ChartSeries,
} from "@/components/ui/chart";
import {
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
} from "@/components/ui/description-list";
import {
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
} from "@/components/ui/dropdown";
import { EmptyState, ListSkeleton, Notice } from "@/components/ui/feedback";
import { Field, Label } from "@/components/ui/fieldset";
import { Heading, Subheading } from "@/components/ui/heading";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import { Navbar, NavbarItem, NavbarSection } from "@/components/ui/navbar";
import { Stat } from "@/components/ui/stat";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Code, Text, TextLink } from "@/components/ui/text";
import { toast } from "@/components/ui/toast";
import { adminPaths, useAdminApi } from "@/lib/api/admin";
import { adminCan, useAdminKey, useAdminSession } from "@/lib/admin";
import { cx } from "@/lib/cx";
import { formatDateTime } from "@/lib/format";
import { adminRoutes } from "@/lib/admin-routes";
import { useAdminResource } from "@/lib/use-admin-resource";
import { useOpenCount } from "@/lib/use-open-count";
import {
  deliveryStatuses,
  mailWindows,
  type AdminDeliveryRow,
  type AdminMail,
  type CursorPage,
  type DeliveryPageQuery,
  type DeliveryStatus,
  type MailKind,
  type MailWindow,
  type RequestKind,
} from "@/types/admin";
// The mail queue: what's waiting and how late the worker is, how much mail
// went out per hour, requests against emails sent, addresses asked for
// often, how mail leaves, and every delivery by status. Opening it records
// two reads: the summary (admin.mail.viewed) and the deliveries
// (admin.delivery.listed). Nothing reloads on its own: Refresh does, and
// so does a retry. Operators can retry a failed delivery from its row's ⋯
// menu, or a listener's failures since a time from the header, each behind
// a fresh code and a reason.
export default function AdminDeliveryIndex() {
  const key = useAdminKey();
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const query = pageQuery(search);
  const period = query.window ?? "24h";
  const summary = useAdminResource(adminPaths.mail(period));
  const deliveries = useAdminResource(
    adminPaths.deliveries.index({
      status: query.status,
      before: query.before,
    }),
  );
  const operator = adminCan(useAdminSession().role, "operator");
  // After a retry, both reload (two more recorded reads).
  const retried = () => {
    summary.refresh();
    deliveries.refresh();
  };
  const pathFor = (q: DeliveryPageQuery) =>
    adminRoutes.deliveries.index(key, q);
  return (
    <>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <Heading>Mail</Heading>
        <div className="flex items-center gap-2">
          <Listbox
            aria-label="Window"
            value={period}
            onChange={(w) =>
              void navigate(
                pathFor({ ...query, window: w, before: undefined }),
                { preventScrollReset: true },
              )
            }
          >
            {mailWindows.map((w) => (
              <ListboxOption key={w} value={w}>
                <ListboxLabel>{windowLabels[w]}</ListboxLabel>
              </ListboxOption>
            ))}
          </Listbox>
          <Button
            outline
            onClick={summary.refresh}
            loading={summary.refreshing && !summary.loading}
          >
            <ArrowPathIcon />
            Refresh
          </Button>
          {operator && (
            <RetryFailedButton mail={summary.data} onRetried={retried} />
          )}
        </div>
      </div>
      {summary.error && (
        <Notice variant="error" className="mt-6" onRetry={summary.refresh}>
          {summary.error}
        </Notice>
      )}
      {summary.data ? (
        <div
          aria-busy={summary.refreshing}
          className={cx(summary.refreshing && "opacity-60")}
        >
          <Summary mail={summary.data} />
        </div>
      ) : (
        summary.loading && <ListSkeleton label="Loading mail" rows={3} />
      )}
      <Deliveries
        query={query}
        mail={summary.data}
        list={deliveries}
        pathFor={pathFor}
        onRetried={retried}
      />
    </>
  );
}
// How far back a bulk retry reaches: at most 7 days (the API's bound).
const retrySince = [
  { value: "1h", label: "the last hour", hours: 1 },
  { value: "6h", label: "the last 6 hours", hours: 6 },
  { value: "24h", label: "the last 24 hours", hours: 24 },
  // A minute short of 7 days, so the API's bound holds by the time it's sent.
  { value: "7d", label: "the last 7 days", hours: 7 * 24 - 1 / 60 },
] as const;
// Retry failed deliveries: one listener's failures since a time (the most
// recent 1,000), for after an SMTP outage. Each runs its listener again, so
// a reset or an invitation gets a fresh link; a blocked address still gets
// nothing.
function RetryFailedButton({
  mail,
  onRetried,
}: {
  mail: AdminMail | undefined;
  onRetried: () => void;
}) {
  const api = useAdminApi();
  const [open, setOpen] = useState(false);
  const opened = useOpenCount(open);
  const listeners = mail?.queue.map((q) => q.listener) ?? [];
  const [listener, setListener] = useState("");
  const [since, setSince] = useState("24h");
  const chosen = listener || listeners[0] || "";
  async function retry(reason: string) {
    const hours = retrySince.find((s) => s.value === since)?.hours ?? 24;
    const failedSince = new Date(Date.now() - hours * 60 * 60 * 1000);
    const { count } = await api.failedDeliveries().retry({
      listener: chosen,
      failedSince: failedSince.toISOString(),
      reason,
    });
    toast.success(
      `${count} ${count === 1 ? "delivery" : "deliveries"} queued again.`,
    );
  }
  return (
    <>
      <Button
        outline
        disabled={listeners.length === 0}
        disabledReason="Loading the listeners"
        onClick={() => setOpen(true)}
      >
        Retry failed deliveries
      </Button>
      <AdminActionDialog
        key={opened}
        open={open}
        onClose={() => setOpen(false)}
        title="Retry failed deliveries?"
        description="Every delivery to the listener that failed since then is queued again (the most recent 1,000), and runs as for a new event: reset and invitation emails get fresh links. Blocked addresses still get nothing."
        confirm="Retry deliveries"
        ready={chosen !== ""}
        action={retry}
        onDone={onRetried}
      >
        <Field>
          <Label>Listener</Label>
          <Listbox value={chosen} onChange={setListener}>
            {listeners.map((name) => (
              <ListboxOption key={name} value={name}>
                <ListboxLabel>{name}</ListboxLabel>
              </ListboxOption>
            ))}
          </Listbox>
        </Field>
        <Field>
          <Label>Failed in</Label>
          <Listbox value={since} onChange={setSince}>
            {retrySince.map(({ label, value }) => (
              <ListboxOption key={value} value={value}>
                <ListboxLabel>{label}</ListboxLabel>
              </ListboxOption>
            ))}
          </Listbox>
        </Field>
      </AdminActionDialog>
    </>
  );
}
const windowLabels: Record<MailWindow, string> = {
  "24h": "Last 24 hours",
  "7d": "Last 7 days",
};
// The URL's state; anything the API wouldn't take is left out.
function pageQuery(search: URLSearchParams): DeliveryPageQuery {
  const period = search.get("window");
  const status = search.get("status");
  const before = search.get("before");
  return {
    window: mailWindows.find((w) => w === period),
    status: deliveryStatuses.find((s) => s === status),
    before:
      before && /^evt_[0-9A-Za-z]{14}:[a-z0-9_.-]{1,100}$/.test(before)
        ? before
        : undefined,
  };
}
function Summary({ mail }: { mail: AdminMail }) {
  return (
    <>
      <QueueStats mail={mail} />
      <Volume mail={mail} />
      <div className="mt-14 grid gap-14 lg:grid-cols-2">
        <Requests mail={mail} />
        <TopRecipients mail={mail} />
      </div>
      <Queue mail={mail} />
      <Driver mail={mail} />
    </>
  );
}
// How long ago a time was, roughly: "4 min", "3 h", "2 days".
function age(since: string): string {
  const minutes = Math.max(
    0,
    Math.round((Date.now() - new Date(since).getTime()) / 60_000),
  );
  if (minutes < 1) return "under a minute";
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.round(minutes / 60);
  if (hours < 48) return `${hours} h`;
  return `${Math.round(hours / 24)} days`;
}
function sum(
  mail: AdminMail,
  field: "due" | "retrying" | "failed" | "delivered",
): number {
  return mail.queue.reduce((total, q) => total + q[field], 0);
}
function QueueStats({ mail }: { mail: AdminMail }) {
  const oldest = mail.queue
    .map((q) => q.oldestPendingAt)
    .filter((t): t is string => t !== null)
    .sort()[0];
  const inWindow = (
    <span className="text-zinc-500">
      {windowLabels[mail.window].toLowerCase()}
    </span>
  );
  return (
    <div className="mt-8 grid gap-8 sm:grid-cols-2 xl:grid-cols-4">
      <Stat title="Due now" value={sum(mail, "due")}>
        <span className="text-zinc-500">
          {sum(mail, "retrying")} retrying after a failure
        </span>
      </Stat>
      <Stat title="Oldest waiting" value={oldest ? age(oldest) : "None"}>
        <span className="text-zinc-500">How far the worker is behind</span>
      </Stat>
      <Stat title="Failed" value={sum(mail, "failed")}>
        {inWindow}
      </Stat>
      <Stat title="Delivered" value={sum(mail, "delivered")}>
        {inWindow}
      </Stat>
    </div>
  );
}
const kinds: ChartSeries<MailKind>[] = [
  { key: "password_reset", label: "Reset links" },
  { key: "invitation", label: "Invitations" },
  { key: "registration_code", label: "Sign-up codes" },
  { key: "account_exists", label: "“You already have an account”" },
  { key: "email_verification", label: "Verification codes" },
];
const hour = 60 * 60 * 1000;
// One row per hour of the window, oldest first, empty hours included.
function volumeRows(mail: AdminMail): ChartRow<MailKind>[] {
  const hours = mail.window === "7d" ? 7 * 24 : 24;
  const last = Math.floor(Date.now() / hour) * hour;
  const counts = new Map<number, Record<MailKind, number>>();
  for (let i = hours - 1; i >= 0; i--) {
    counts.set(last - i * hour, {
      password_reset: 0,
      invitation: 0,
      registration_code: 0,
      account_exists: 0,
      email_verification: 0,
    });
  }
  for (const v of mail.volume) {
    const bucket = counts.get(new Date(v.hour).getTime());
    if (bucket) bucket[v.kind] += v.count;
  }
  return [...counts].map(([time, values]) => ({
    label: hourLabel(time, mail.window),
    values,
  }));
}
// "14:00" over a day, "Oct 30, 14:00" over a week, in UTC like every date.
function hourLabel(time: number, window: MailWindow): string {
  const date = new Date(time);
  const clock = `${String(date.getUTCHours()).padStart(2, "0")}:00`;
  if (window === "24h") return clock;
  const day = date.toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  });
  return `${day}, ${clock}`;
}
function Volume({ mail }: { mail: AdminMail }) {
  const rows = volumeRows(mail);
  const totals = rows.map((row) =>
    kinds.reduce((n, { key }) => n + row.values[key], 0),
  );
  const total = totals.reduce((a, b) => a + b, 0);
  const busiest = rows[totals.indexOf(Math.max(...totals))];
  const period = windowLabels[mail.window].toLowerCase();
  const summary =
    total === 0
      ? `No emails sent in the ${period}.`
      : `${total} ${total === 1 ? "email" : "emails"} sent in the ${period}, the most at ${busiest.label} UTC.`;
  return (
    <div className="mt-14">
      <Subheading>Emails sent per hour</Subheading>
      <BarChart
        className="mt-2"
        label="Emails sent per hour"
        summary={summary}
        rows={rows}
        series={kinds}
      />
    </div>
  );
}
const requestKinds: Record<RequestKind, { label: string; gap: string }> = {
  password_reset: {
    label: "Password resets",
    gap: "Addresses without an account get nothing, so a wide gap is someone trying addresses.",
  },
  registration: {
    label: "Sign-ups",
    gap: "Each gets a code or “You already have an account”: a gap is mail trouble, sign-ups left over 15 minutes, or the 5-a-day notice cap.",
  },
  email_verification: {
    label: "Email verification",
    gap: "Each gets a code: a gap is mail trouble.",
  },
};
function Requests({ mail }: { mail: AdminMail }) {
  return (
    <div>
      <Subheading>Asked for and sent</Subheading>
      <Table dense className="mt-4">
        <TableHead>
          <TableRow>
            <TableHeader>Request</TableHeader>
            <TableHeader className="text-right">Asked for</TableHeader>
            <TableHeader className="text-right">Sent</TableHeader>
          </TableRow>
        </TableHead>
        <TableBody>
          {mail.requests.map(({ kind, requested, sent }) => (
            <TableRow key={kind}>
              <TableCell className="whitespace-normal">
                <div className="font-medium">{requestKinds[kind].label}</div>
                <div className="text-zinc-500 dark:text-zinc-400">
                  {requestKinds[kind].gap}
                </div>
              </TableCell>
              <TableCell className="text-right tabular-nums">
                {requested}
              </TableCell>
              <TableCell className="text-right tabular-nums">{sent}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
function TopRecipients({ mail }: { mail: AdminMail }) {
  const key = useAdminKey();
  return (
    <div>
      <Subheading>Addresses asked for often</Subheading>
      {mail.topRecipients.length === 0 ? (
        <Text className="mt-4">No address was asked for 3 or more times.</Text>
      ) : (
        <Table dense className="mt-4">
          <TableHead>
            <TableRow>
              <TableHeader>Email</TableHeader>
              <TableHeader className="text-right">Total</TableHeader>
              <TableHeader className="text-right max-sm:hidden">
                Resets
              </TableHeader>
              <TableHeader className="text-right max-sm:hidden">
                Invitations
              </TableHeader>
              <TableHeader className="text-right max-sm:hidden">
                Sign-ups
              </TableHeader>
            </TableRow>
          </TableHead>
          <TableBody>
            {mail.topRecipients.map((r) => (
              <TableRow key={r.email}>
                <TableCell className="max-w-56 truncate">
                  <TextLink
                    href={adminRoutes.events.index(key, {
                      email: r.email,
                      window: mail.window,
                    })}
                  >
                    {r.email}
                  </TextLink>
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {r.total}
                </TableCell>
                <TableCell className="text-right tabular-nums max-sm:hidden">
                  {r.resets}
                </TableCell>
                <TableCell className="text-right tabular-nums max-sm:hidden">
                  {r.invitations}
                </TableCell>
                <TableCell className="text-right tabular-nums max-sm:hidden">
                  {r.registrations}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
function Queue({ mail }: { mail: AdminMail }) {
  return (
    <div className="mt-14">
      <Subheading>Queue by listener</Subheading>
      {mail.queue.length === 0 ? (
        <Text className="mt-4">No listeners: mail is off.</Text>
      ) : (
        <Table dense className="mt-4">
          <TableHead>
            <TableRow>
              <TableHeader>Listener</TableHeader>
              <TableHeader className="text-right">Due</TableHeader>
              <TableHeader className="text-right">Retrying</TableHeader>
              <TableHeader className="max-sm:hidden">
                Oldest waiting
              </TableHeader>
              <TableHeader className="text-right max-sm:hidden">
                Failed
              </TableHeader>
              <TableHeader className="text-right max-sm:hidden">
                Delivered
              </TableHeader>
            </TableRow>
          </TableHead>
          <TableBody>
            {mail.queue.map((q) => (
              <TableRow key={q.listener}>
                <TableCell>
                  <Code>{q.listener}</Code>
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {q.due}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {q.retrying}
                </TableCell>
                <TableCell className="text-zinc-500 max-sm:hidden">
                  {q.oldestPendingAt ? age(q.oldestPendingAt) : "–"}
                </TableCell>
                <TableCell className="text-right tabular-nums max-sm:hidden">
                  {q.failed}
                </TableCell>
                <TableCell className="text-right tabular-nums max-sm:hidden">
                  {q.delivered}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
function Driver({ mail }: { mail: AdminMail }) {
  const { driver } = mail;
  return (
    <div className="mt-14">
      <Subheading>How mail is sent</Subheading>
      <DescriptionList className="mt-4">
        <DescriptionTerm>Driver</DescriptionTerm>
        <DescriptionDetails className="font-mono">
          {driver.driver || "off"}
        </DescriptionDetails>
        {driver.host && (
          <>
            <DescriptionTerm>Server</DescriptionTerm>
            <DescriptionDetails className="font-mono">
              {driver.host}:{driver.port} ({driver.tls})
            </DescriptionDetails>
          </>
        )}
        {driver.fromAddress && (
          <>
            <DescriptionTerm>Sender</DescriptionTerm>
            <DescriptionDetails>
              {driver.fromName} &lt;{driver.fromAddress}&gt;
            </DescriptionDetails>
          </>
        )}
        <DescriptionTerm>Last delivered</DescriptionTerm>
        <DescriptionDetails>
          {mail.lastDeliveredAt
            ? formatDateTime(mail.lastDeliveredAt)
            : `None in the ${windowLabels[mail.window].toLowerCase()}`}
        </DescriptionDetails>
        <DescriptionTerm>Last failure</DescriptionTerm>
        <DescriptionDetails>
          {mail.lastFailure ? (
            <>
              {formatDateTime(mail.lastFailure.failedAt)},{" "}
              <Code>{mail.lastFailure.listener}</Code>
              <div className="mt-1 break-words text-zinc-500">
                {mail.lastFailure.error}
              </div>
            </>
          ) : (
            `None in the ${windowLabels[mail.window].toLowerCase()}`
          )}
        </DescriptionDetails>
      </DescriptionList>
    </div>
  );
}
const statusLabels: Record<DeliveryStatus, string> = {
  failed: "Failed",
  retrying: "Retrying",
  pending: "Pending",
  delivered: "Delivered",
};
// Every delivery, by status, newest event first. Failed comes first while
// the window has any.
function Deliveries({
  query,
  mail,
  list,
  pathFor,
  onRetried,
}: {
  query: DeliveryPageQuery;
  mail: AdminMail | undefined;
  list: ReturnType<typeof useAdminResource<CursorPage<AdminDeliveryRow>>>;
  pathFor: (q: DeliveryPageQuery) => string;
  onRetried: () => void;
}) {
  const failures = mail ? sum(mail, "failed") : 0;
  const tabs: (DeliveryStatus | undefined)[] = failures
    ? ["failed", undefined, "retrying", "pending", "delivered"]
    : [undefined, "failed", "retrying", "pending", "delivered"];
  return (
    <div className="mt-14">
      <Subheading>Deliveries</Subheading>
      <Navbar aria-label="Delivery status" className="mt-2">
        <NavbarSection className="-ml-2">
          {tabs.map((status) => (
            <NavbarItem
              key={status ?? "all"}
              href={pathFor({ window: query.window, status })}
              current={query.status === status}
              aria-current={query.status === status ? "page" : undefined}
            >
              {status ? statusLabels[status] : "All"}
              {status === "failed" && failures > 0 && ` (${failures})`}
            </NavbarItem>
          ))}
        </NavbarSection>
      </Navbar>
      {list.error && (
        <Notice variant="error" className="mt-6" onRetry={list.refresh}>
          {list.error}
        </Notice>
      )}
      {list.data ? (
        list.data.data.length === 0 ? (
          <EmptyState
            title={
              query.status
                ? `No ${statusLabels[query.status].toLowerCase()} deliveries`
                : "No deliveries yet"
            }
            description="Each email is a delivery of its event to a mail listener."
          />
        ) : (
          <div
            aria-busy={list.refreshing}
            className={cx(list.refreshing && "opacity-60")}
          >
            <DeliveryTable rows={list.data.data} onRetried={onRetried} />
            <CursorNav
              label="Delivery pages"
              next={list.data.next}
              hrefFor={(before) => pathFor({ ...query, before })}
            />
          </div>
        )
      ) : (
        list.loading && <ListSkeleton label="Loading deliveries" />
      )}
    </div>
  );
}
function DeliveryTable({
  rows,
  onRetried,
}: {
  rows: AdminDeliveryRow[];
  onRetried: () => void;
}) {
  const key = useAdminKey();
  const api = useAdminApi();
  const operator = adminCan(useAdminSession().role, "operator");
  const [retrying, setRetrying] = useState<AdminDeliveryRow | null>(null);
  const opened = useOpenCount(retrying !== null);
  // The last one stays rendered while its dialog animates out.
  const [shown, setShown] = useState<AdminDeliveryRow | null>(null);
  if (retrying && retrying !== shown) setShown(retrying);
  return (
    <>
      <Table dense className="mt-4">
        <TableHead>
          <TableRow>
            <TableHeader>Event</TableHeader>
            <TableHeader className="max-md:hidden">Listener</TableHeader>
            <TableHeader className="max-sm:hidden">To</TableHeader>
            <TableHeader>Status</TableHeader>
            <TableHeader className="max-lg:hidden">When</TableHeader>
            {operator && (
              <TableHeader className="w-0">
                <span className="sr-only">Actions</span>
              </TableHeader>
            )}
          </TableRow>
        </TableHead>
        <TableBody>
          {rows.map((row) => (
            <TableRow key={`${row.event.id}:${row.listener}`}>
              <TableCell>
                <TextLink href={adminRoutes.events.show(key, row.event.id)}>
                  {formatDateTime(row.event.createdAt)}
                </TextLink>
                <div className="text-zinc-500 dark:text-zinc-400">
                  {row.event.name}
                </div>
              </TableCell>
              <TableCell className="max-md:hidden">
                <Code>{row.listener}</Code>
              </TableCell>
              <TableCell className="max-w-56 truncate max-sm:hidden">
                {row.to ?? "–"}
              </TableCell>
              <TableCell>
                <DeliveryStatusBadge status={row.status} />
                <div className="mt-1 text-zinc-500 tabular-nums">
                  {row.attempts} {row.attempts === 1 ? "attempt" : "attempts"}
                </div>
              </TableCell>
              {/* The last error under the time, wrapping: an SMTP reply is long. */}
              <TableCell className="max-w-sm whitespace-normal text-zinc-500 max-lg:hidden">
                <DeliveryTime delivery={row} />
                {row.lastError && (
                  <div className="mt-1 wrap-anywhere">{row.lastError}</div>
                )}
              </TableCell>
              {operator && (
                <TableCell className="w-0 text-right">
                  {row.status === "failed" && (
                    <Dropdown>
                      <DropdownButton
                        plain
                        aria-label={`Actions for the ${row.listener} delivery`}
                      >
                        <EllipsisHorizontalIcon />
                      </DropdownButton>
                      <DropdownMenu anchor="bottom end">
                        <DropdownItem onClick={() => setRetrying(row)}>
                          <DropdownLabel>Retry delivery</DropdownLabel>
                        </DropdownItem>
                      </DropdownMenu>
                    </Dropdown>
                  )}
                </TableCell>
              )}
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {shown && (
        <AdminActionDialog
          key={opened}
          open={retrying !== null}
          onClose={() => setRetrying(null)}
          title="Retry this delivery?"
          description={`The ${shown.event.name} event is delivered to ${shown.listener} again, as for a new event: a reset or invitation email gets a fresh link, never the old one. A blocked address still gets nothing.`}
          confirm="Retry delivery"
          action={(reason) =>
            api.deliveries().retry(shown.event.id, shown.listener, reason)
          }
          onDone={() => {
            toast.success("Delivery queued again.");
            onRetried();
          }}
        />
      )}
    </>
  );
}
