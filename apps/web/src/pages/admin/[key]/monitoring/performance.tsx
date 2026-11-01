import type { ReactNode } from "react";
import { useSearchParams } from "react-router";
import { Badge } from "@/components/ui/badge";
import { Sparkline } from "@/components/ui/chart";
import {
  MetricsStoreNotice,
  MonitoringBody,
  MonitoringHeader,
  MonitoringSection,
  MonitoringTable,
  NoneFound,
  SectionGate,
  SeriesChart,
  SignalGrid,
  bucketLabel,
  formatSignal,
  useMonitoringPage,
} from "@/components/app/admin/monitoring";
import { formatEventTime } from "@/components/app/admin/event-filters";
import { Stat } from "@/components/ui/stat";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Code, Text } from "@/components/ui/text";
import { useAdminKey } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import { url } from "@/lib/pagination";
import type {
  GaugePoint,
  LatencyRow,
  MonitoringView,
  PerformanceSections,
  RouteRow,
} from "@/types/admin";
// Which routes are slow, is the database or the worker saturated? From
// the minutes every server writes, all servers summed (and their hourly
// sums over 7 days), with each server's own numbers last. With metric
// storage off it says so: everything from the metrics reads zero. One
// request, one recorded read.
export default function AdminMonitoringPerformance() {
  const page = useMonitoringPage<PerformanceSections>(
    "performance",
    adminRoutes.monitoring.performance,
  );
  return (
    <>
      <MonitoringHeader
        title="Performance"
        window={page.window}
        onWindow={page.setWindow}
        view={page.data}
      />
      <MonitoringBody resource={page} label="Loading performance">
        {(view) => <Performance view={view} />}
      </MonitoringBody>
    </>
  );
}
type View = MonitoringView<PerformanceSections>;
function Performance({ view }: { view: View }) {
  return (
    <>
      <MetricsStoreNotice className="mt-6" store={view.sections.metricsStore} />
      <SignalGrid className="mt-8" signals={view.signals} />
      <SectionGate names={["requests"]} title="Requests">
        <Requests view={view} />
      </SectionGate>
      <SectionGate names={["routes"]} title="Routes">
        <Routes view={view} />
      </SectionGate>
      <SectionGate names={["database"]} title="Database">
        <Database view={view} />
      </SectionGate>
      <SectionGate names={["statements"]} title="Statements">
        <Statements view={view} />
      </SectionGate>
      <SectionGate names={["worker", "hashing"]} title="Event worker">
        <Worker view={view} />
      </SectionGate>
      <SectionGate names={["servers"]} title="Each server">
        <Servers view={view} />
      </SectionGate>
    </>
  );
}
const count = (n: number) => n.toLocaleString("en-US");
const ms = (value: number) => formatSignal({ unit: "ms", value });
function Requests({ view }: { view: View }) {
  const points = view.sections.requests;
  const series = {
    series: ["other", "serverErrors"],
    buckets: points.map((p) => ({
      start: p.start,
      counts: [p.requests - p.serverErrors, p.serverErrors],
    })),
  };
  return (
    <MonitoringSection
      title="Requests"
      description="Every server's requests per bucket, and their latency estimated from the summed histograms."
    >
      <SeriesChart
        label="Requests"
        series={series}
        labels={{ other: "Answered", serverErrors: "Server errors (5xx)" }}
        bucketSeconds={view.window.bucketSeconds}
        noun={["request", "requests"]}
      />
      <div className="mt-8 grid gap-8 sm:grid-cols-3 [&>*]:min-w-0">
        {(["p50", "p95", "p99"] as const).map((q) => (
          <LatencyTile key={q} q={q} view={view} />
        ))}
      </div>
    </MonitoringSection>
  );
}
const quantileLabels = {
  p50: "Median",
  p95: "95th percentile",
  p99: "99th percentile",
};
// A percentile's highest bucket, with its sparkline over the window.
function LatencyTile({ q, view }: { q: "p50" | "p95" | "p99"; view: View }) {
  const points = view.sections.requests.map((p) => ({
    label: bucketLabel(p.start, view.window.bucketSeconds),
    value: p[q],
  }));
  const top = points.reduce((a, b) => (b.value > a.value ? b : a), {
    label: "",
    value: 0,
  });
  return (
    <Stat
      title={quantileLabels[q]}
      value={ms(top.value)}
      chart={
        <Sparkline
          label={`${quantileLabels[q]} latency over the window`}
          summary={
            top.value
              ? `Highest at ${top.label} UTC: ${ms(top.value)}.`
              : "No requests in this window."
          }
          points={points}
          format={ms}
        />
      }
    >
      <span className="text-zinc-500 dark:text-zinc-400">
        The highest bucket in the window
      </span>
    </Stat>
  );
}
// The columns the routes table sorts by (?sort=, requests by default).
const routeColumns = [
  { key: "requests", label: "Requests" },
  { key: "p95", label: "p95" },
  { key: "p99", label: "p99" },
  { key: "status4xx", label: "4xx" },
  { key: "status429", label: "429" },
  { key: "status5xx", label: "5xx" },
  { key: "timeouts", label: "Timeouts" },
] as const;
type RouteSort = (typeof routeColumns)[number]["key"];
// A route's p95 means something from this many requests.
const routeSample = 20;
function Routes({ view }: { view: View }) {
  const key = useAdminKey();
  const [search] = useSearchParams();
  const sort: RouteSort =
    routeColumns.find((c) => c.key === search.get("sort"))?.key ?? "requests";
  const href = (next: RouteSort) =>
    url(adminRoutes.monitoring.performance(key, view.window.name), {
      sort: next === "requests" ? undefined : next,
    });
  // The slowest five by p95, of the routes with enough requests.
  const slowest = new Set(
    view.sections.routes
      .filter((r) => r.requests >= routeSample)
      .sort((a, b) => b.p95 - a.p95)
      .slice(0, 5)
      .map((r) => r.route),
  );
  const rows = [...view.sections.routes].sort(
    (a, b) => b[sort] - a[sort] || a.route.localeCompare(b.route),
  );
  return (
    <MonitoringSection
      title="Routes"
      description={`Every route pattern with requests in the window (the 100 busiest), all servers summed. The slowest five by 95th percentile, of those with ${routeSample} requests or more, are marked.`}
    >
      {rows.length === 0 ? (
        <NoneFound>No requests in this window.</NoneFound>
      ) : (
        <Table dense>
          <TableHead>
            <TableRow>
              <TableHeader>Route</TableHeader>
              <TableHeader>p50</TableHeader>
              {routeColumns.map((c) => (
                <TableHeader
                  key={c.key}
                  href={href(c.key)}
                  sort={c.key === sort ? "descending" : undefined}
                >
                  {c.label}
                </TableHeader>
              ))}
            </TableRow>
          </TableHead>
          <TableBody>
            {rows.map((r) => (
              <RouteTableRow
                key={r.route}
                row={r}
                slow={slowest.has(r.route)}
              />
            ))}
          </TableBody>
        </Table>
      )}
    </MonitoringSection>
  );
}
function RouteTableRow({ row, slow }: { row: RouteRow; slow: boolean }) {
  return (
    <TableRow>
      <TableCell>
        <span className="flex flex-wrap items-center gap-2">
          <Code>{row.route}</Code>
          {slow && <Badge color="zinc">Slowest</Badge>}
        </span>
      </TableCell>
      <TableCell className="tabular-nums">{ms(row.p50)}</TableCell>
      <TableCell className="tabular-nums">{count(row.requests)}</TableCell>
      <TableCell className="tabular-nums">{ms(row.p95)}</TableCell>
      <TableCell className="tabular-nums">{ms(row.p99)}</TableCell>
      <TableCell className="tabular-nums">{count(row.status4xx)}</TableCell>
      <TableCell className="tabular-nums">{count(row.status429)}</TableCell>
      <TableCell className="tabular-nums">{count(row.status5xx)}</TableCell>
      <TableCell className="tabular-nums">{count(row.timeouts)}</TableCell>
    </TableRow>
  );
}
// Percentiles per label (a query's operation, a listener).
function LatencyTable({ label, rows }: { label: string; rows: LatencyRow[] }) {
  return (
    <MonitoringTable head={[label, "Count", "p50", "p95", "p99"]}>
      {rows.map((r) => (
        <TableRow key={r.label}>
          <TableCell>
            <Code>{r.label || "–"}</Code>
          </TableCell>
          <TableCell className="tabular-nums">{count(r.count)}</TableCell>
          <TableCell className="tabular-nums">{ms(r.p50)}</TableCell>
          <TableCell className="tabular-nums">{ms(r.p95)}</TableCell>
          <TableCell className="tabular-nums">{ms(r.p99)}</TableCell>
        </TableRow>
      ))}
    </MonitoringTable>
  );
}
// A gauge's average per bucket as a sparkline tile.
function GaugeTile({
  title,
  points,
  view,
  children,
}: {
  title: string;
  points: GaugePoint[];
  view: View;
  children: ReactNode;
}) {
  const data = points.map((p) => ({
    label: bucketLabel(p.start, view.window.bucketSeconds),
    value: Math.round(p.value * 10) / 10,
  }));
  const top = data.reduce((a, b) => (b.value > a.value ? b : a), {
    label: "",
    value: 0,
  });
  return (
    <Stat
      title={title}
      value={count(top.value)}
      chart={
        <Sparkline
          label={`${title} over the window`}
          summary={
            top.value
              ? `Highest at ${top.label} UTC: ${count(top.value)}.`
              : "None in this window."
          }
          points={data}
        />
      }
    >
      <span className="text-zinc-500 dark:text-zinc-400">{children}</span>
    </Stat>
  );
}
function Database({ view }: { view: View }) {
  const { operations, slowQueries, inUse, waits } = view.sections.database;
  const from = new Date(waits.from);
  const since = new Date(view.window.from);
  return (
    <MonitoringSection
      title="Database"
      description="Query time by operation, slow queries by table (over SLOW_QUERY_MS), and the pool of 10 connections: waits for a connection mean the pool is the bottleneck."
    >
      <div className="grid gap-8 sm:grid-cols-2 [&>*]:min-w-0">
        <GaugeTile title="Connections in use" points={inUse} view={view}>
          Average per server, the highest bucket in the window
        </GaugeTile>
        <Stat
          title="Waits for a connection"
          value={count(waits.waits)}
          level={waits.waits > 0 ? "warn" : undefined}
        >
          <span className="text-zinc-500 dark:text-zinc-400">
            {waits.waits > 0
              ? `${ms(waits.waitedMs / waits.waits)} each on average, in ${count(waits.minutes)} ${waits.minutes === 1 ? "minute" : "minutes"}`
              : "None"}
            {from > since &&
              `; since ${formatEventTime(waits.from)} UTC (minutes are kept 48 hours)`}
          </span>
        </Stat>
      </div>
      <div className="mt-10 grid gap-x-14 gap-y-10 lg:grid-cols-2">
        <div>
          <Text className="mb-3 font-medium">Query time by operation</Text>
          {operations.length === 0 ? (
            <NoneFound>No queries counted in this window.</NoneFound>
          ) : (
            <LatencyTable label="Operation" rows={operations} />
          )}
        </div>
        <div>
          <Text className="mb-3 font-medium">Slow queries by table</Text>
          {slowQueries.length === 0 ? (
            <NoneFound>No slow query in this window.</NoneFound>
          ) : (
            <MonitoringTable head={["Table", "Slow queries"]}>
              {slowQueries.map((q) => (
                <TableRow key={q.label}>
                  <TableCell>
                    <Code>{q.label}</Code>
                  </TableCell>
                  <TableCell className="tabular-nums">
                    {count(q.count)}
                  </TableCell>
                </TableRow>
              ))}
            </MonitoringTable>
          )}
        </div>
      </div>
    </MonitoringSection>
  );
}
function Statements({ view }: { view: View }) {
  const stats = view.sections.database.statements;
  return (
    <MonitoringSection
      title="Statements"
      description="From PostgreSQL's pg_stat_statements: the statements taking the most time in all since its statistics were reset, as PostgreSQL normalised them ($1 for every value). Only this database's, as this server's role."
    >
      {!stats || !stats.available ? (
        <NoneFound>
          This database doesn&rsquo;t have pg_stat_statements (installed with
          CREATE EXTENSION and loaded through shared_preload_libraries).
        </NoneFound>
      ) : (
        <>
          <MonitoringTable head={["Statement", "Calls", "Total", "Mean"]}>
            {stats.statements.map((s) => (
              <TableRow key={s.query}>
                <TableCell className="max-w-xl whitespace-normal">
                  <code className="font-mono text-xs/5 break-words">
                    {s.query}
                  </code>
                </TableCell>
                <TableCell className="tabular-nums">{count(s.calls)}</TableCell>
                <TableCell className="tabular-nums">{ms(s.totalMs)}</TableCell>
                <TableCell className="tabular-nums">{ms(s.meanMs)}</TableCell>
              </TableRow>
            ))}
          </MonitoringTable>
          {stats.hidden > 0 && (
            <Text className="mt-3">
              {count(stats.hidden)} more not shown: past the top 10, or not a
              query PostgreSQL normalises (a utility statement, or text that
              could hold a value).
            </Text>
          )}
        </>
      )}
    </MonitoringSection>
  );
}
const outcomeLabels = {
  delivered: "Delivered",
  retried: "Retried",
  failed: "Failed",
};
function Worker({ view }: { view: View }) {
  const { deliveries, lag, pending } = view.sections.worker;
  return (
    <MonitoringSection
      title="Event worker"
      description="Deliveries per bucket by outcome, how long an event waited for its listener's first attempt (up to 10 s: the histogram's last bound), and how many waited."
    >
      <SeriesChart
        label="Deliveries"
        series={deliveries}
        labels={outcomeLabels}
        bucketSeconds={view.window.bucketSeconds}
        noun={["delivery", "deliveries"]}
      />
      <div className="mt-10 grid gap-x-14 gap-y-10 lg:grid-cols-2 [&>*]:min-w-0">
        <div>
          <Text className="mb-3 font-medium">Lag by listener</Text>
          {lag.length === 0 ? (
            <NoneFound>No delivery in this window.</NoneFound>
          ) : (
            <LatencyTable label="Listener" rows={lag} />
          )}
        </div>
        <div className="grid gap-8 sm:grid-cols-2 lg:grid-cols-1 [&>*]:min-w-0">
          <GaugeTile title="Waiting deliveries" points={pending} view={view}>
            Average, the highest bucket in the window
          </GaugeTile>
          <Stat
            title="Password checks refused"
            value={count(view.sections.slotsBusy)}
            level={view.sections.slotsBusy > 0 ? "warn" : undefined}
          >
            <span className="text-zinc-500 dark:text-zinc-400">
              Every hashing slot (4) was busy: sign-in saturated, by load or an
              attack
            </span>
          </Stat>
        </div>
      </div>
    </MonitoringSection>
  );
}
const bytes = (n: number | null) =>
  n === null ? "–" : `${count(Math.round(n / (1024 * 1024)))} MB`;
const gauge = (n: number | null) => (n === null ? "–" : count(n));
function Servers({ view }: { view: View }) {
  const servers = view.sections.servers;
  return (
    <MonitoringSection
      title="Each server"
      description="Each server heard from in the last day, with its latest numbers (from its last 5 minutes)."
    >
      {servers.length === 0 ? (
        <NoneFound>
          No server has written its minutes in the last day.
        </NoneFound>
      ) : (
        <MonitoringTable
          head={[
            "Server",
            "Version",
            "Started",
            "Goroutines",
            "Heap",
            "Memory",
            "GC paused",
            "Pool open / in use / idle",
          ]}
        >
          {servers.map((s) => (
            <TableRow key={s.id}>
              <TableCell>
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{s.hostname}</span>
                  {!s.live && <Badge color="zinc">Silent</Badge>}
                </div>
                <Code>{s.id}</Code>
              </TableCell>
              <TableCell>
                <Code>{s.version}</Code>
              </TableCell>
              <TableCell>{formatEventTime(s.startedAt)}</TableCell>
              <TableCell className="tabular-nums">
                {gauge(s.goroutines)}
              </TableCell>
              <TableCell className="tabular-nums">
                {bytes(s.heapBytes)}
              </TableCell>
              <TableCell className="tabular-nums">
                {bytes(s.memoryBytes)}
              </TableCell>
              <TableCell className="tabular-nums">
                {s.gcPauseMs === null ? "–" : ms(s.gcPauseMs)}
              </TableCell>
              <TableCell className="tabular-nums">
                {gauge(s.poolOpen)} / {gauge(s.poolInUse)} / {gauge(s.poolIdle)}
              </TableCell>
            </TableRow>
          ))}
        </MonitoringTable>
      )}
    </MonitoringSection>
  );
}
