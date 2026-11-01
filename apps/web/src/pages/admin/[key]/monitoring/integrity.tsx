import type { ReactNode } from "react";
import { LevelBadge } from "@/components/ui/badge";
import {
  MonitoringBody,
  MonitoringHeader,
  MonitoringSection,
  MonitoringTable,
  NoneFound,
  SectionGate,
  SignalGrid,
  formatSignal,
  signalWindow,
  useMonitoringPage,
} from "@/components/app/admin/monitoring";
import { TableCell, TableRow } from "@/components/ui/table";
import { Code, Text, TextLink } from "@/components/ui/text";
import { useAdminKey } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import type {
  CheckResult,
  IntegritySections,
  MonitoringView,
  Signal,
} from "@/types/admin";
// Is anything true that should never be? Any duplicates from retries?
// Each check is a signal, its value how many rows break it, with up to 10
// of them named by ID; then the duplicates and retries over the window
// (an Idempotency-Key for creates is the fix if those aren't zero). One
// request, one recorded read.
export default function AdminMonitoringIntegrity() {
  const page = useMonitoringPage<IntegritySections>(
    "integrity",
    adminRoutes.monitoring.integrity,
  );
  return (
    <>
      <MonitoringHeader
        title="Integrity"
        window={page.window}
        onWindow={page.setWindow}
        view={page.data}
      />
      <MonitoringBody resource={page} label="Loading the integrity checks">
        {(view) => <Integrity view={view} />}
      </MonitoringBody>
    </>
  );
}
type View = MonitoringView<IntegritySections>;
// The summaries' names; every other signal is one check.
const summaries = ["integrity.critical_checks", "integrity.warning_checks"];
function Integrity({ view }: { view: View }) {
  return (
    <>
      <SignalGrid
        className="mt-8"
        signals={view.signals.filter((s) => summaries.includes(s.name))}
      />
      <Checks view={view} />
      <MonitoringSection
        title="Duplicates and retries"
        description="Creates and sends that happened twice in this window. Creates aren't idempotent yet: if these aren't zero, an Idempotency-Key for create routes is the fix."
      >
        <div className="grid gap-x-14 gap-y-10 lg:grid-cols-2">
          <SectionGate names={["duplicates"]} title="Created twice">
            <Duplicates view={view} />
          </SectionGate>
          <SectionGate names={["duplicateSends"]} title="Sent twice">
            <Found
              title="Sent twice"
              description="A reset link or invitation emailed more than once for one request: at-least-once delivery sending again. Named by the request."
              result={view.sections.duplicateSends}
              none="No email was sent twice for one request."
            />
          </SectionGate>
          <SectionGate
            names={["retriedDeliveries"]}
            title="Delivered on a later attempt"
          >
            <Found
              title="Delivered on a later attempt"
              description="Deliveries that succeeded after a failed attempt: if that attempt timed out after sending, the email went twice. The first error isn't kept, so a timeout can't be told apart."
              result={view.sections.retriedDeliveries}
              none="Every delivery in this window went on its first attempt."
            />
          </SectionGate>
          <SectionGate names={["timedOutWrites"]} title="Writes that timed out">
            <TimedOut view={view} />
          </SectionGate>
        </div>
      </MonitoringSection>
    </>
  );
}
// What a failing check means, by its signal's name.
const meanings: Record<string, string> = {
  "integrity.ownerless_workspaces":
    "A membership change or deletion left a workspace with no owner. Of the workspaces with an event in the last day.",
  "integrity.workspaces_over_quota":
    "The quota's lock was bypassed, or a quota was lowered (existing rows are kept). Of the workspaces with an event in the last day.",
  "integrity.member_invitations":
    "Accepting an invitation didn't use it up. Named by workspace.",
  "integrity.admins_without_mfa":
    "Something turned two-factor off for an admin outside the setup window.",
  "integrity.expired_rows":
    "Sessions, admin sessions, challenges, reset links, sign-up and email codes or block links expired over 30 minutes ago: the cleanup loop isn't running. Named by table.",
  "integrity.ended_limit_windows":
    "Rate-limit windows that ended over 30 minutes ago: the cleanup loop isn't running.",
  "integrity.overdue_deliveries":
    "Deliveries waiting 10 minutes past their time: the event worker is stalled, or every server is down.",
  "integrity.unmatched_deliveries":
    "Deliveries that failed because no listener has their name: a listener renamed with deliveries queued. Last 7 days.",
  "integrity.unknown_events":
    "Events with a name outside the catalogue: old code, or a write that bypassed the store. Last day.",
  "integrity.servers_out_of_step":
    "Live servers on another version than the newest, or built ahead of their database, 30 minutes into a release: a release stopped halfway.",
};
function Checks({ view }: { view: View }) {
  const checks = view.signals.filter((s) => !summaries.includes(s.name));
  return (
    <MonitoringSection
      title="Checks"
      description="Things that should never be true. Each counts the rows that break it now (at most 1,000) and names up to 10."
    >
      <MonitoringTable head={["Check", "Level", "Rows", "Rows named"]}>
        {checks.map((s) => (
          <CheckRow key={s.name} signal={s} view={view} />
        ))}
      </MonitoringTable>
    </MonitoringSection>
  );
}
function CheckRow({ signal, view }: { signal: Signal; view: View }) {
  const rows = view.sections.checks.find((c) => c.name === signal.name);
  const unread =
    signal.unavailable || view.unavailable.includes(signal.name) || !rows;
  return (
    <TableRow>
      <TableCell className="max-w-md whitespace-normal">
        <div className="font-medium text-zinc-950 dark:text-white">
          {signal.label}
        </div>
        <div className="text-zinc-500 dark:text-zinc-400">
          {meanings[signal.name]}
        </div>
      </TableCell>
      <TableCell>
        {signal.unavailable ? (
          "Not loaded"
        ) : (
          <LevelBadge level={signal.level} />
        )}
      </TableCell>
      <TableCell className="tabular-nums">
        {signal.unavailable
          ? "–"
          : `${formatSignal(signal)}${rows?.capped ? "+" : ""}`}
        <div className="text-zinc-500 dark:text-zinc-400">
          {signalWindow(signal)}
        </div>
      </TableCell>
      <TableCell className="whitespace-normal">
        {unread ? (
          <span className="text-zinc-500 dark:text-zinc-400">Not loaded</span>
        ) : rows.samples.length === 0 ? (
          <span className="text-zinc-500 dark:text-zinc-400">–</span>
        ) : (
          <Samples ids={rows.samples} />
        )}
      </TableCell>
    </TableRow>
  );
}
// Rows by public ID: accounts, workspaces and events link to their pages;
// anything else (a server, a table) is shown as it is.
function Samples({ ids }: { ids: string[] }) {
  return (
    <ul className="flex flex-wrap gap-x-3 gap-y-1">
      {ids.map((id) => (
        <li key={id}>
          <SampleLink id={id} />
        </li>
      ))}
    </ul>
  );
}
function SampleLink({ id }: { id: string }) {
  const key = useAdminKey();
  const href = id.startsWith("usr_")
    ? adminRoutes.users.show(key, id)
    : id.startsWith("wsp_")
      ? adminRoutes.workspaces.show(key, id)
      : id.startsWith("evt_")
        ? adminRoutes.events.show(key, id)
        : undefined;
  if (!href) return <Code>{id}</Code>;
  return (
    <TextLink href={href} className="font-mono">
      {id}
    </TextLink>
  );
}
const count = (n: number) => n.toLocaleString("en-US");
// A count with the rows named, or a line saying there are none.
function Found({
  title,
  description,
  result,
  none,
  children,
}: {
  title: string;
  description: string;
  result: CheckResult;
  none: string;
  children?: ReactNode;
}) {
  return (
    <MonitoringSection title={title} description={description}>
      {result.count === 0 ? (
        <NoneFound>{none}</NoneFound>
      ) : (
        <>
          <Text>
            {count(result.count)}
            {result.capped && "+"} in this window.
          </Text>
          {children}
          <div className="mt-3 text-sm/6">
            <Samples ids={result.samples} />
          </div>
        </>
      )}
    </MonitoringSection>
  );
}
// A create's kind, singular and plural.
const createLabels: Record<string, [string, string]> = {
  "workspace.created": ["workspace", "workspaces"],
  "project.created": ["project", "projects"],
  "workspace.invitation.created": ["invitation", "invitations"],
};
function Duplicates({ view }: { view: View }) {
  const d = view.sections.duplicates;
  return (
    <Found
      title="Created twice"
      description="The same account creating the same workspace, project or invitation (by name or email) within 10 seconds: a double submit, or a retry of a create that had worked. Named by the second event."
      result={d}
      none="Nothing was created twice in this window."
    >
      <Text className="mt-1">
        {d.byName
          .map((n) => {
            const [one, many] = createLabels[n.name] ?? [n.name, n.name];
            return `${count(n.count)} ${n.count === 1 ? one : many}`;
          })
          .join(", ")}
        .
      </Text>
    </Found>
  );
}
function TimedOut({ view }: { view: View }) {
  const rows = view.sections.timedOutWrites;
  return (
    <MonitoringSection
      title="Writes that timed out"
      description="Changes the 10-second deadline ended: the client saw an error, but the change may have been saved, and a retry then makes it twice."
    >
      {rows.length === 0 ? (
        <NoneFound>No write timed out in this window.</NoneFound>
      ) : (
        <MonitoringTable head={["Route", "Timed out"]}>
          {rows.map((r) => (
            <TableRow key={r.route}>
              <TableCell>
                <Code>{r.route}</Code>
              </TableCell>
              <TableCell className="tabular-nums">{count(r.count)}</TableCell>
            </TableRow>
          ))}
        </MonitoringTable>
      )}
    </MonitoringSection>
  );
}
