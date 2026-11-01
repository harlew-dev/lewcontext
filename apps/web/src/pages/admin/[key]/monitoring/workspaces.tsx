import { AccountLink, WorkspaceLink } from "@/components/app/admin/events";
import { formatEventTime } from "@/components/app/admin/event-filters";
import {
  MonitoringBody,
  MonitoringHeader,
  MonitoringSection,
  MonitoringTable,
  NoneFound,
  SectionGate,
  SeriesChart,
  SignalGrid,
  useMonitoringPage,
} from "@/components/app/admin/monitoring";
import { LevelBadge, type Level } from "@/components/ui/badge";
import { Meter } from "@/components/ui/chart";
import { TableCell, TableRow } from "@/components/ui/table";
import { Text } from "@/components/ui/text";
import { adminRoutes } from "@/lib/admin-routes";
import type { MonitoringView, WorkspacesSections } from "@/types/admin";
// Which workspaces are filling up, growing fast, or used to send spam?
// Only the workspaces with an event in the window are counted (a workspace
// grows only by writing, and counting every workspace's rows would read
// whole tables): their quotas, their rows across every workspace table,
// what they made; invitations over 7 days; workspaces made and thrown
// away within a day. One request, one recorded read.
export default function AdminMonitoringWorkspaces() {
  const page = useMonitoringPage<WorkspacesSections>(
    "workspaces",
    adminRoutes.monitoring.workspaces,
  );
  return (
    <>
      <MonitoringHeader
        title="Workspaces"
        window={page.window}
        onWindow={page.setWindow}
        view={page.data}
      />
      <MonitoringBody resource={page} label="Loading workspaces">
        {(view) => <Workspaces view={view} />}
      </MonitoringBody>
    </>
  );
}
type View = MonitoringView<WorkspacesSections>;
function Workspaces({ view }: { view: View }) {
  return (
    <>
      <SignalGrid className="mt-8" signals={view.signals} />
      <SectionGate names={["quotas"]} title="Quota headroom">
        <Quotas view={view} />
      </SectionGate>
      <SectionGate names={["rows"]} title="Rows per workspace">
        <Rows view={view} />
      </SectionGate>
      <div className="grid gap-x-14 lg:grid-cols-2">
        <SectionGate names={["growth"]} title="Growth">
          <Growth view={view} />
        </SectionGate>
        <SectionGate names={["throwaways"]} title="Thrown away">
          <Throwaways view={view} />
        </SectionGate>
      </div>
      <SectionGate names={["invitations"]} title="Invitations">
        <Invitations view={view} />
      </SectionGate>
      <SectionGate names={["totals"]} title="Made and deleted">
        <Totals view={view} />
      </SectionGate>
    </>
  );
}
// A quota's level: 80% used a warning, full critical (it's refusing).
function quotaLevel(value: number, max: number): Level {
  if (value >= max) return "critical";
  return value * 100 >= max * 80 ? "warn" : "ok";
}
function QuotaMeter({
  label,
  value,
  max,
}: {
  label: string;
  value: number;
  max: number;
}) {
  const level = quotaLevel(value, max);
  const used = `${value.toLocaleString("en-US")} of ${max.toLocaleString("en-US")}`;
  return (
    <div>
      <div className="flex items-center justify-between gap-2 text-sm/6">
        <span className="text-zinc-500 dark:text-zinc-400">{label}</span>
        <span className="flex items-center gap-2">
          <span className="tabular-nums">{used}</span>
          {level !== "ok" && <LevelBadge level={level} />}
        </span>
      </div>
      <Meter
        className="mt-1"
        label={label}
        value={value}
        max={max}
        level={level}
        valueText={`${used} used`}
      />
    </div>
  );
}
function Quotas({ view }: { view: View }) {
  const { caps, workspaces, owners } = view.sections.quotas;
  return (
    <MonitoringSection
      title="Quota headroom"
      description="Workspaces and owners at 80% or more of a quota: a warning at 80%, critical when full (creates are refused)."
    >
      {workspaces.length === 0 && owners.length === 0 ? (
        <NoneFound>No workspace or owner is near a quota.</NoneFound>
      ) : (
        <div className="grid gap-x-10 gap-y-8 sm:grid-cols-2 xl:grid-cols-3">
          {workspaces.map((w) => (
            <div key={w.workspace.id} className="space-y-2">
              <div className="font-medium">
                <WorkspaceLink workspace={w.workspace} />
              </div>
              <QuotaMeter
                label="Projects"
                value={w.projects}
                max={caps.projects}
              />
              <QuotaMeter
                label="Members and invitations"
                value={w.members + w.invitations}
                max={caps.members}
              />
              <QuotaMeter
                label="Live invitations"
                value={w.invitations}
                max={caps.invitations}
              />
            </div>
          ))}
          {owners.map((o) => (
            <div key={o.account.id} className="space-y-2">
              <div className="font-medium">
                <AccountLink account={o.account} />
              </div>
              <QuotaMeter
                label="Workspaces owned"
                value={o.owned}
                max={caps.owned}
              />
            </div>
          ))}
        </div>
      )}
    </MonitoringSection>
  );
}
function Rows({ view }: { view: View }) {
  const { tables, workspaces } = view.sections.rows;
  return (
    <MonitoringSection
      title="Rows per workspace"
      description="The workspaces holding the most rows across every table with a workspace, of those with an event in this window. Over 50,000 is a warning."
    >
      {workspaces.length === 0 ? (
        <NoneFound>No workspace had an event in this window.</NoneFound>
      ) : (
        <MonitoringTable
          head={["Workspace", ...tables.map((t) => t.label), "Total"]}
        >
          {workspaces.map((w) => (
            <TableRow key={w.workspace.id}>
              <TableCell>
                <WorkspaceLink workspace={w.workspace} />
              </TableCell>
              {w.counts.map((n, i) => (
                <TableCell key={tables[i]?.table ?? i} className="tabular-nums">
                  {n.toLocaleString("en-US")}
                </TableCell>
              ))}
              <TableCell className="font-medium tabular-nums">
                {w.total.toLocaleString("en-US")}
              </TableCell>
            </TableRow>
          ))}
        </MonitoringTable>
      )}
    </MonitoringSection>
  );
}
function Growth({ view }: { view: View }) {
  const rows = view.sections.growth;
  return (
    <MonitoringSection
      title="Growth"
      description="The workspaces making the most projects and invitations in this window, and those with the most events."
    >
      {rows.length === 0 ? (
        <NoneFound>No workspace had an event in this window.</NoneFound>
      ) : (
        <MonitoringTable head={["Workspace", "Made", "Events"]}>
          {rows.map((r) => (
            <TableRow key={r.workspace.id}>
              <TableCell>
                <WorkspaceLink workspace={r.workspace} />
              </TableCell>
              <TableCell className="tabular-nums">{r.created}</TableCell>
              <TableCell className="tabular-nums">{r.events}</TableCell>
            </TableRow>
          ))}
        </MonitoringTable>
      )}
    </MonitoringSection>
  );
}
function Throwaways({ view }: { view: View }) {
  const rows = view.sections.throwaways;
  return (
    <MonitoringSection
      title="Thrown away"
      description="Accounts whose workspaces were deleted within a day of being made, with the invitations those sent. 3 in a day is a warning."
    >
      {rows.length === 0 ? (
        <NoneFound>No workspace was made and deleted within a day.</NoneFound>
      ) : (
        <MonitoringTable
          head={["Account", "Workspaces", "Invitations", "Last deleted (UTC)"]}
        >
          {rows.map((r) => (
            <TableRow key={r.account.id}>
              <TableCell>
                <AccountLink account={r.account} />
              </TableCell>
              <TableCell className="tabular-nums">{r.workspaces}</TableCell>
              <TableCell className="tabular-nums">{r.invitations}</TableCell>
              <TableCell>{formatEventTime(r.lastDeleted)}</TableCell>
            </TableRow>
          ))}
        </MonitoringTable>
      )}
    </MonitoringSection>
  );
}
// Spam isn't accepted: under 10% of 20 or more.
function lowAcceptance(sent: number, accepted: number): boolean {
  return sent >= 20 && accepted * 10 < sent;
}
function Invitations({ view }: { view: View }) {
  const rows = view.sections.invitations;
  return (
    <MonitoringSection
      title="Invitations"
      description="The workspaces sending the most invitations over the last 7 days, and what became of them. Under 10% accepted of 20 or more looks like spam."
    >
      {rows.length === 0 ? (
        <NoneFound>No invitation was sent in the last 7 days.</NoneFound>
      ) : (
        <MonitoringTable
          head={["Workspace", "Sent", "Accepted", "Revoked", "Expired"]}
        >
          {rows.map((r) => (
            <TableRow key={r.workspace.id}>
              <TableCell>
                <span className="flex flex-wrap items-center gap-2">
                  <WorkspaceLink workspace={r.workspace} />
                  {lowAcceptance(r.sent, r.accepted) && (
                    <LevelBadge level="warn" />
                  )}
                </span>
              </TableCell>
              <TableCell className="tabular-nums">{r.sent}</TableCell>
              <TableCell className="tabular-nums">{r.accepted}</TableCell>
              <TableCell className="tabular-nums">{r.revoked}</TableCell>
              <TableCell className="tabular-nums">{r.expired}</TableCell>
            </TableRow>
          ))}
        </MonitoringTable>
      )}
    </MonitoringSection>
  );
}
const totalLabels = {
  "workspace.created": "Made",
  "workspace.deleted": "Deleted",
};
function Totals({ view }: { view: View }) {
  const { estimate, series } = view.sections.totals;
  return (
    <MonitoringSection
      title="Made and deleted"
      description="Workspaces made and deleted over the window."
    >
      <Text className="mb-4">
        About {estimate.toLocaleString("en-US")} workspaces in all
        (PostgreSQL&rsquo;s running count).
      </Text>
      <SeriesChart
        label="Workspaces made and deleted"
        series={series}
        labels={totalLabels}
        bucketSeconds={view.window.bucketSeconds}
        noun={["workspace", "workspaces"]}
      />
    </MonitoringSection>
  );
}
