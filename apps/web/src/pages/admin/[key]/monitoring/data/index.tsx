import { Badge, LevelBadge } from "@/components/ui/badge";
import { BarList, Sparkline } from "@/components/ui/chart";
import { Stat } from "@/components/ui/stat";
import { TableCell, TableRow } from "@/components/ui/table";
import { Code, Text, TextLink } from "@/components/ui/text";
import {
  formatDay,
  formatEventTime,
} from "@/components/app/admin/event-filters";
import {
  formatBytes,
  MonitoringBody,
  MonitoringHeader,
  MonitoringSection,
  MonitoringTable,
  NoneFound,
  SectionGate,
  SeriesChart,
  SignalGrid,
  useDrillHref,
  useMonitoringPage,
} from "@/components/app/admin/monitoring";
import { useAdminKey } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import type {
  DataSections,
  MonitoringView,
  RetentionTarget,
} from "@/types/admin";
// How big is the database, what's growing, is pruning running? The
// database's size and what's connected now (counts and durations only:
// never a query's text, a role or an address), each table's sizes and
// upkeep, growth from the daily sizes, what fills the events table, and
// each prune target's oldest row against its retention with its last
// run. One request, one recorded read.
export default function AdminMonitoringData() {
  const page = useMonitoringPage<DataSections>(
    "data",
    adminRoutes.monitoring.data,
  );
  return (
    <>
      <MonitoringHeader
        title="Data"
        window={page.window}
        onWindow={page.setWindow}
        view={page.data}
      />
      <MonitoringBody resource={page} label="Loading the data page">
        {(view) => <Data view={view} />}
      </MonitoringBody>
    </>
  );
}
type View = MonitoringView<DataSections>;
function Data({ view }: { view: View }) {
  return (
    <>
      <SignalGrid className="mt-8" signals={view.signals} />
      <SectionGate names={["retention"]} title="Retention">
        <Retention view={view} />
      </SectionGate>
      <SectionGate names={["database"]} title="Database">
        <Database view={view} />
      </SectionGate>
      <SectionGate names={["growth"]} title="Growth">
        <Growth view={view} />
      </SectionGate>
      <SectionGate names={["tables"]} title="Tables">
        <Tables view={view} />
      </SectionGate>
      <SectionGate names={["log"]} title="What fills the log">
        <Log view={view} />
      </SectionGate>
    </>
  );
}
const count = (n: number) => Math.round(n).toLocaleString("en-US");
// "45 s", "12 min", "3.2 h": a duration in seconds.
function duration(seconds: number): string {
  if (seconds < 1) return "under a second";
  if (seconds < 120) return `${Math.round(seconds)} s`;
  if (seconds < 7200) return `${Math.round(seconds / 60)} min`;
  return `${(seconds / 3600).toFixed(1)} h`;
}
// "96 days", "5 hours": how old a time is, at the page's time.
function age(value: string, now: string): string {
  const hours = (Date.parse(now) - Date.parse(value)) / 3_600_000;
  if (hours < 48) return `${Math.max(0, Math.round(hours))} hours`;
  return `${Math.floor(hours / 24)} days`;
}
// "+1.2 MB", "−8 kB": a change in size.
function signedBytes(bytes: number): string {
  return bytes < 0 ? `−${formatBytes(-bytes)}` : `+${formatBytes(bytes)}`;
}
function Retention({ view }: { view: View }) {
  const key = useAdminKey();
  const targets = view.sections.retention;
  const runs = adminRoutes.events.index(key, {
    names: ["prune.completed"],
    window: "all",
  });
  return (
    <MonitoringSection
      title="Retention"
      description="Each prune target's oldest row a run would remove (or, for pseudonymise, minimise) against its age, and its last confirmed run. Overdue: a week past its age. Not running: no run in 2 days, or none ever while it has held rows that long. Schedule every target daily from cron."
    >
      <MonitoringTable
        head={["Target", "Kept", "Oldest row", "Last run", "State"]}
      >
        {targets.map((t) => (
          <TableRow key={t.target}>
            <TableCell className="max-w-xs whitespace-normal">
              <Code>{t.target}</Code>
              <div className="mt-1 text-xs/5 text-zinc-500 dark:text-zinc-400">
                {t.description}
              </div>
            </TableCell>
            <TableCell className="tabular-nums">{t.keepDays} days</TableCell>
            <TableCell className="tabular-nums">
              {t.oldest ? age(t.oldest, view.window.to) : "None"}
            </TableCell>
            <TableCell>
              <LastRun target={t} />
            </TableCell>
            <TableCell>
              <RetentionState target={t} />
            </TableCell>
          </TableRow>
        ))}
      </MonitoringTable>
      <Text className="mt-3">
        Every confirmed run records prune.completed, the evidence that retention
        runs: <TextLink href={runs}>every run on Events</TextLink>.
      </Text>
    </MonitoringSection>
  );
}
function LastRun({ target }: { target: RetentionTarget }) {
  const key = useAdminKey();
  const run = target.lastRun;
  if (!run) return <>Never</>;
  const verb = target.minimises ? "minimised" : "deleted";
  return (
    <>
      <TextLink href={adminRoutes.events.show(key, run.event)}>
        {formatEventTime(run.at)} UTC
      </TextLink>
      <div className="text-xs/5 text-zinc-500 tabular-nums dark:text-zinc-400">
        {count(run.deleted)} {verb}
        {run.kept > 0 && `, ${count(run.kept)} kept`}
      </div>
    </>
  );
}
// A target's state in words: never colour alone.
function RetentionState({ target }: { target: RetentionTarget }) {
  if (!target.overdue && !target.notRunning) return <LevelBadge level="ok" />;
  return (
    <span className="flex flex-wrap items-center gap-2">
      <LevelBadge level="warn" />
      <span className="text-sm/6">
        {[target.overdue && "Overdue", target.notRunning && "Not running"]
          .filter(Boolean)
          .join(", ")}
      </span>
    </span>
  );
}
// Connection states as PostgreSQL names them, in words.
const stateLabels: Record<string, string> = {
  active: "Running a query",
  idle: "Idle",
  "idle in transaction": "Idle in a transaction",
  "idle in transaction (aborted)": "Idle in a failed transaction",
  "fastpath function call": "Fast-path call",
  disabled: "Not tracked",
  hidden: "Another role's (state hidden)",
};
function Database({ view }: { view: View }) {
  const d = view.sections.database;
  const total = d.connections.reduce((n, c) => n + c.count, 0);
  return (
    <MonitoringSection
      title="Database"
      description="Right now, from PostgreSQL's statistics: counts and durations only, never a query's text, a role or a client's address. Disk space on the database server isn't visible from SQL: the provider's monitoring has it."
    >
      <div className="grid gap-8 sm:grid-cols-2 xl:grid-cols-4">
        <Stat title="Size" value={formatBytes(d.sizeBytes)}>
          <span className="text-zinc-500 dark:text-zinc-400">
            The whole database, indexes included
          </span>
        </Stat>
        <Stat title="Connections" value={count(total)}>
          <span className="text-zinc-500 dark:text-zinc-400">
            Of {count(d.maxConnections)} allowed (every server&rsquo;s pool and
            anyone else)
          </span>
        </Stat>
        <Stat title="Longest query" value={duration(d.longestQuerySeconds)}>
          <span className="text-zinc-500 dark:text-zinc-400">
            Running now, this page&rsquo;s own left out
          </span>
        </Stat>
        <Stat
          title="Cache hits"
          value={
            d.cacheHitShare === null ? "–" : `${d.cacheHitShare.toFixed(1)}%`
          }
        >
          <span className="text-zinc-500 dark:text-zinc-400">
            Blocks read from memory since the statistics were reset
          </span>
        </Stat>
      </div>
      <div className="mt-8 grid gap-x-14 gap-y-8 lg:grid-cols-2">
        <MonitoringTable head={["Connections by state", "Count"]}>
          {d.connections.map((c) => (
            <TableRow key={c.state}>
              <TableCell>{stateLabels[c.state] ?? c.state}</TableCell>
              <TableCell className="tabular-nums">{count(c.count)}</TableCell>
            </TableRow>
          ))}
        </MonitoringTable>
        <MonitoringTable head={["Holding things up", "Now"]}>
          <TableRow>
            <TableCell>Longest open transaction</TableCell>
            <TableCell className="tabular-nums">
              {duration(d.longestTransactionSeconds)}
            </TableCell>
          </TableRow>
          <TableRow>
            <TableCell>Idle in a transaction</TableCell>
            <TableCell className="tabular-nums">
              {count(d.idleInTransaction)}
            </TableCell>
          </TableRow>
          <TableRow>
            <TableCell>Waiting for a lock</TableCell>
            <TableCell className="tabular-nums">{count(d.lockWaits)}</TableCell>
          </TableRow>
        </MonitoringTable>
      </div>
    </MonitoringSection>
  );
}
function Growth({ view }: { view: View }) {
  const g = view.sections.growth;
  const description = `The known tables' sizes, written once a day by one server after the hourly rollup (table_stats_daily, kept 400 days), over the last 30 days. Each rate runs from a table's first day here to its latest.`;
  if (g.days.length < 2) {
    return (
      <MonitoringSection title="Growth" description={description}>
        <NoneFound>
          Growth needs two days of sizes: the server writes one a day.
        </NoneFound>
      </MonitoringSection>
    );
  }
  const latest = g.days[g.days.length - 1];
  const points = g.days.map((d) => ({
    label: formatDay(new Date(`${d.day}T00:00:00Z`)),
    value: d.totalBytes,
  }));
  const projection =
    g.projectedBytes === null
      ? ""
      : ` At this rate, ${formatBytes(g.projectedBytes)} in ${g.projectionDays} days.`;
  return (
    <MonitoringSection title="Growth" description={description}>
      <div className="grid gap-8 lg:grid-cols-3 [&>*]:min-w-0">
        <Stat title="Known tables now" value={formatBytes(latest.totalBytes)}>
          <span className="text-zinc-500 dark:text-zinc-400">
            {signedBytes(g.bytesPerDay)} a day.{projection}
          </span>
        </Stat>
        <Sparkline
          className="lg:col-span-2 lg:self-end"
          label="The known tables' size per day"
          summary={`From ${formatBytes(g.days[0].totalBytes)} on ${points[0].label} to ${formatBytes(latest.totalBytes)} on ${points[points.length - 1].label}.`}
          points={points}
          format={formatBytes}
        />
      </div>
      <div className="mt-8">
        <MonitoringTable
          head={["Growing fastest", "Rows", "A day", "Size", "A day"]}
        >
          {g.tables.map((t) => (
            <TableRow key={t.name}>
              <TableCell>
                <Code>{t.name}</Code>
              </TableCell>
              <TableCell className="tabular-nums">
                {count(t.liveRows)}
              </TableCell>
              <TableCell className="tabular-nums">
                {t.rowsPerDay < 0 ? "−" : "+"}
                {count(Math.abs(t.rowsPerDay))}
              </TableCell>
              <TableCell className="tabular-nums">
                {formatBytes(t.totalBytes)}
              </TableCell>
              <TableCell className="tabular-nums">
                {signedBytes(t.bytesPerDay)}
              </TableCell>
            </TableRow>
          ))}
        </MonitoringTable>
      </div>
    </MonitoringSection>
  );
}
function Tables({ view }: { view: View }) {
  const tables = view.sections.tables;
  return (
    <MonitoringSection
      title="Tables"
      description="Each table's size with its indexes, its rows (PostgreSQL's running counts: close, not exact), when it was last vacuumed and analyzed, and how it was read since the statistics were reset. Mostly sequential scans on a big table usually means a missing index."
    >
      <MonitoringTable
        head={[
          "Table",
          "Size",
          "Indexes",
          "Live rows",
          "Dead rows",
          "Vacuumed",
          "Analyzed",
          "Scans (seq / index)",
        ]}
      >
        {tables.map((t) => (
          <TableRow key={t.name}>
            <TableCell>
              <span className="flex flex-wrap items-center gap-2">
                <Code>{t.name}</Code>
                {t.scanned && <Badge color="zinc">Mostly sequential</Badge>}
                {t.bloated && <Badge color="zinc">Needs a vacuum</Badge>}
              </span>
            </TableCell>
            <TableCell className="tabular-nums">
              {formatBytes(t.totalBytes)}
            </TableCell>
            <TableCell className="tabular-nums">
              {formatBytes(t.indexBytes)}
            </TableCell>
            <TableCell className="tabular-nums">{count(t.liveRows)}</TableCell>
            <TableCell className="tabular-nums">
              {count(t.deadRows)}
              {t.deadRows > 0 && ` (${t.deadShare.toFixed(0)}%)`}
            </TableCell>
            <TableCell>
              {t.lastVacuum ? formatEventTime(t.lastVacuum) : "Never"}
            </TableCell>
            <TableCell>
              {t.lastAnalyze ? formatEventTime(t.lastAnalyze) : "Never"}
            </TableCell>
            <TableCell className="tabular-nums">
              {count(t.seqScans)} / {count(t.indexScans)}
            </TableCell>
          </TableRow>
        ))}
      </MonitoringTable>
    </MonitoringSection>
  );
}
function Log({ view }: { view: View }) {
  const drill = useDrillHref();
  const { chart, names, total } = view.sections.log;
  const labels = Object.fromEntries(
    chart.series.map((s) => [s, s === "other" ? "Other" : s]),
  );
  return (
    <MonitoringSection
      title="What fills the log"
      description="Events recorded over the window by name, the four recorded most stacked and the rest as Other; then the names recorded most with their share. Each opens its events."
    >
      <SeriesChart
        label="Events recorded"
        series={chart}
        labels={labels}
        bucketSeconds={view.window.bucketSeconds}
        noun={["event", "events"]}
      />
      {names.length === 0 ? (
        <NoneFound>No event in this window.</NoneFound>
      ) : (
        <BarList
          className="mt-6"
          label="Recorded most"
          total={total}
          items={names.map((n) => ({
            key: n.name,
            label: <span className="font-mono text-xs/6">{n.name}</span>,
            value: n.count,
            href: drill({ name: n.name, window: view.window.name }) ?? "#",
          }))}
        />
      )}
    </MonitoringSection>
  );
}
