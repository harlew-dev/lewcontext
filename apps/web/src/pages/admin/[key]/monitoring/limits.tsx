import type { ReactNode } from "react";
import { AccountLink } from "@/components/app/admin/events";
import { formatEventTime } from "@/components/app/admin/event-filters";
import {
  MonitoringBody,
  MonitoringHeader,
  MonitoringSection,
  NoneFound,
  SeriesChart,
  SharedLimitMeter,
  SignalGrid,
  useMonitoringPage,
} from "@/components/app/admin/monitoring";
import { BarList } from "@/components/ui/chart";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Code } from "@/components/ui/text";
import { useAdminKey } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import type { LimitsSections, MonitoringView } from "@/types/admin";
// Which limits are being hit, by whom, and is any shared limit shutting
// everyone out? Windows reached come from rate_limit.reached (one per limit
// per window, however many requests it refused); every refusal from the
// metrics; who's at a limit right now from the counters, which hold
// digests, so how many and never who.
export default function AdminMonitoringLimits() {
  const page = useMonitoringPage<LimitsSections>(
    "limits",
    adminRoutes.monitoring.limits,
  );
  return (
    <>
      <MonitoringHeader
        title="Rate limits"
        window={page.window}
        onWindow={page.setWindow}
        view={page.data}
      />
      <MonitoringBody resource={page} label="Loading rate limits">
        {(view) => <Limits view={view} />}
      </MonitoringBody>
    </>
  );
}
type View = MonitoringView<LimitsSections>;
function Limits({ view }: { view: View }) {
  return (
    <>
      <SignalGrid className="mt-8" signals={view.signals} />
      <Shared view={view} />
      <Reached view={view} />
      <div className="grid gap-x-14 lg:grid-cols-2">
        <Refused view={view} />
        <Blocked view={view} />
      </div>
      <div className="grid gap-x-14 lg:grid-cols-2">
        <TopAccounts view={view} />
        <TopAddresses view={view} />
      </div>
      <div className="grid gap-x-14 lg:grid-cols-2">
        <Budgets view={view} />
        <Quotas view={view} />
      </div>
      <Routes view={view} />
    </>
  );
}
function SimpleTable({
  head,
  children,
}: {
  head: string[];
  children: ReactNode;
}) {
  return (
    <Table dense>
      <TableHead>
        <TableRow>
          {head.map((h) => (
            <TableHeader key={h}>{h}</TableHeader>
          ))}
        </TableRow>
      </TableHead>
      <TableBody>{children}</TableBody>
    </Table>
  );
}
function Shared({ view }: { view: View }) {
  return (
    <MonitoringSection
      title="Shared limits"
      description="Limits counted for everyone together. One reached switches its feature off for everyone until its window ends."
    >
      <div className="grid gap-8 sm:grid-cols-2 xl:grid-cols-3">
        {view.sections.shared.map((l) => (
          <SharedLimitMeter key={l.name} limit={l} />
        ))}
      </div>
    </MonitoringSection>
  );
}
function Reached({ view }: { view: View }) {
  const key = useAdminKey();
  const { reached, byLimit } = view.sections;
  const labels = Object.fromEntries(
    reached.series.map((s) => [s, s === "other" ? "Other" : s]),
  );
  return (
    <MonitoringSection
      title="Windows reached"
      description="A window reached is counted once, however many requests it then refused."
    >
      <SeriesChart
        label="Windows reached"
        series={reached}
        labels={labels}
        bucketSeconds={view.window.bucketSeconds}
        noun={["window", "windows"]}
      />
      {byLimit.length > 0 && (
        <div className="mt-6">
          <SimpleTable head={["Limit", "Windows", "Last reached (UTC)"]}>
            {byLimit.map((l) => (
              <TableRow
                key={l.limit}
                href={adminRoutes.events.index(key, {
                  names: ["rate_limit.reached"],
                  window: view.window.name,
                })}
              >
                <TableCell>
                  <Code>{l.limit}</Code>
                </TableCell>
                <TableCell className="tabular-nums">{l.windows}</TableCell>
                <TableCell>{formatEventTime(l.lastReached)}</TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        </div>
      )}
    </MonitoringSection>
  );
}
function Refused({ view }: { view: View }) {
  const rows = view.sections.refused;
  return (
    <MonitoringSection
      title="Requests refused"
      description="Every refusal (429 or 503), by limit, from every server's counts."
    >
      {rows.length === 0 ? (
        <NoneFound>No requests refused in this window.</NoneFound>
      ) : (
        <SimpleTable head={["Limit", "Refused"]}>
          {rows.map((r) => (
            <TableRow key={r.label}>
              <TableCell>
                <Code>{r.label}</Code>
              </TableCell>
              <TableCell className="tabular-nums">
                {r.count.toLocaleString("en-US")}
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      )}
    </MonitoringSection>
  );
}
function Blocked({ view }: { view: View }) {
  const rows = view.sections.blocked.filter((b) => b.subjects !== 0);
  return (
    <MonitoringSection
      title="Blocked right now"
      description="Per limit, how many accounts, addresses or emails are at it in their current window. The counters hold digests: how many, never who; the events show who."
    >
      {rows.length === 0 ? (
        <NoneFound>Nobody is at a limit right now.</NoneFound>
      ) : (
        <SimpleTable head={["Limit", "At it now"]}>
          {rows.map((r) => (
            <TableRow key={r.name}>
              <TableCell>
                <Code>{r.name}</Code>
              </TableCell>
              <TableCell className="tabular-nums">
                {r.subjects ?? (
                  <span className="text-zinc-500">
                    Counted in each server&rsquo;s memory
                  </span>
                )}
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      )}
    </MonitoringSection>
  );
}
function TopAccounts({ view }: { view: View }) {
  const rows = view.sections.topAccounts;
  return (
    <MonitoringSection title="Accounts reaching the most">
      {rows.length === 0 ? (
        <NoneFound>No account reached a limit in this window.</NoneFound>
      ) : (
        <SimpleTable head={["Account", "Windows"]}>
          {rows.map((r) => (
            <TableRow key={r.account.id}>
              <TableCell>
                <AccountLink account={r.account} />
              </TableCell>
              <TableCell className="tabular-nums">{r.windows}</TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      )}
    </MonitoringSection>
  );
}
function TopAddresses({ view }: { view: View }) {
  const key = useAdminKey();
  const rows = view.sections.topAddresses;
  return (
    <MonitoringSection title="Addresses reaching the most">
      {rows.length === 0 ? (
        <NoneFound>No address reached a limit in this window.</NoneFound>
      ) : (
        <BarList
          label="Addresses reaching the most limits"
          total={rows.reduce((n, r) => n + r.count, 0)}
          items={rows.map((r) => ({
            key: r.ip,
            label: <span className="font-mono">{r.ip}</span>,
            value: r.count,
            href: adminRoutes.events.index(key, {
              ip: r.ip,
              names: ["rate_limit.reached"],
              window: view.window.name,
            }),
          }))}
        />
      )}
    </MonitoringSection>
  );
}
function Budgets({ view }: { view: View }) {
  const rows = view.sections.budgets;
  return (
    <MonitoringSection
      title="Budgets"
      description="Accounts reaching the write or read budget every signed-in request counts against: a script, a scraper or a runaway tab."
    >
      {rows.length === 0 ? (
        <NoneFound>No account reached its budget in this window.</NoneFound>
      ) : (
        <SimpleTable head={["Account", "Budget", "Windows"]}>
          {rows.map((r) => (
            <TableRow key={`${r.account.id}${r.limit}`}>
              <TableCell>
                <AccountLink account={r.account} />
              </TableCell>
              <TableCell>
                <Code>{r.limit}</Code>
              </TableCell>
              <TableCell className="tabular-nums">{r.windows}</TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      )}
    </MonitoringSection>
  );
}
const quotaLabels: Record<string, string> = {
  owned_workspaces: "Workspaces an account owns (20)",
  workspace_projects: "Projects in a workspace (1,000)",
  workspace_members: "Members and invitations in a workspace",
};
function Quotas({ view }: { view: View }) {
  const rows = view.sections.quotas;
  return (
    <MonitoringSection
      title="Quotas"
      description="Creates refused because a quota was full."
    >
      {rows.length === 0 ? (
        <NoneFound>No create refused by a quota in this window.</NoneFound>
      ) : (
        <SimpleTable head={["Quota", "Refused"]}>
          {rows.map((r) => (
            <TableRow key={r.label}>
              <TableCell>{quotaLabels[r.label] ?? r.label}</TableCell>
              <TableCell className="tabular-nums">{r.count}</TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      )}
    </MonitoringSection>
  );
}
function Routes({ view }: { view: View }) {
  const rows = view.sections.routes;
  return (
    <MonitoringSection
      title="By route"
      description="Routes answering 429, with their requests. A shared limit's 503s count among the route's server errors."
    >
      {rows.length === 0 ? (
        <NoneFound>No route answered 429 in this window.</NoneFound>
      ) : (
        <SimpleTable head={["Route", "429s", "Requests", "5xx"]}>
          {rows.map((r) => (
            <TableRow key={r.route}>
              <TableCell>
                <Code>{r.route}</Code>
              </TableCell>
              <TableCell className="tabular-nums">{r.refused}</TableCell>
              <TableCell className="tabular-nums">{r.requests}</TableCell>
              <TableCell className="tabular-nums">{r.serverErrors}</TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      )}
    </MonitoringSection>
  );
}
