import { createContext, useContext, type ReactNode } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { LevelBadge, type Level } from "@/components/ui/badge";
import { BarChart, Meter, type ChartSeries } from "@/components/ui/chart";
import { ListSkeleton, Notice } from "@/components/ui/feedback";
import { Heading, Subheading } from "@/components/ui/heading";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import { Stat } from "@/components/ui/stat";
import {
  Table,
  TableBody,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Code, Text, TextLink } from "@/components/ui/text";
import { adminPaths } from "@/lib/api/admin";
import { useAdminKey } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import { cx } from "@/lib/cx";
import { useAdminResource } from "@/lib/use-admin-resource";
import {
  defaultMonitoringWindow,
  monitoringWindows,
  type MonitoringPage,
  type MonitoringView,
  type MonitoringWindow,
  type MetricsStore,
  type MonitorSeries,
  type SharedLimit,
  type Signal,
} from "@/types/admin";
import { eventFilters, formatClock, formatDay } from "./event-filters";
// What the Monitoring pages share (docs/api/monitoring.md): the window in
// the URL, one recorded read per page (useMonitoringPage), the header with
// the window switch, signals as tiles with their level and the Events rows
// behind them, charts by bucket, and loading and failure states. Each page
// keeps its own sections.

export const monitoringWindowLabels: Record<MonitoringWindow, string> = {
  "1h": "Last hour",
  "24h": "Last 24 hours",
  "7d": "Last 7 days",
};
// The window in the URL; anything else is the default.
export function monitoringWindow(search: URLSearchParams): MonitoringWindow {
  const value = search.get("window");
  return monitoringWindows.find((w) => w === value) ?? defaultMonitoringWindow;
}
// A Monitoring page's one request (one recorded read), with its window from
// the URL, and a way to change the window (a navigation, so Back undoes it).
export function useMonitoringPage<S>(
  page: MonitoringPage,
  pageUrl: (key: string, window?: MonitoringWindow) => string,
) {
  const key = useAdminKey();
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const window = monitoringWindow(search);
  const resource = useAdminResource(
    adminPaths.monitoring.show<S>(page, window),
  );
  const setWindow = (w: MonitoringWindow) =>
    void navigate(pageUrl(key, w), { preventScrollReset: true });
  return { ...resource, window, setWindow };
}
// The page's heading, its window switch on the right, and below them the
// window the server resolved (UTC) and what the tiles count instead.
export function MonitoringHeader({
  title,
  window,
  onWindow,
  view,
  signals = true,
}: {
  title: string;
  window: MonitoringWindow;
  onWindow: (w: MonitoringWindow) => void;
  view?: { window: MonitoringView<unknown>["window"] };
  // Signal tiles keep their own windows; a page without them says only
  // what it counted over.
  signals?: boolean;
}) {
  return (
    <>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <Heading>{title}</Heading>
        <div>
          <Listbox aria-label="Time range" value={window} onChange={onWindow}>
            {monitoringWindows.map((w) => (
              <ListboxOption key={w} value={w}>
                <ListboxLabel>{monitoringWindowLabels[w]}</ListboxLabel>
              </ListboxOption>
            ))}
          </Listbox>
        </div>
      </div>
      <Text className="mt-2">
        {view
          ? signals
            ? `Charts and lists: ${spanLabel(view.window.from, view.window.to)} UTC. Each tile counts its own window, shown under it.`
            : `Counted over ${spanLabel(view.window.from, view.window.to)} UTC.`
          : " "}
      </Text>
    </>
  );
}
// "Oct 31, 14:30 – Nov 1, 14:47", in UTC.
function spanLabel(from: string, to: string): string {
  const a = new Date(from);
  const b = new Date(to);
  return `${formatDay(a)}, ${formatClock(a)} – ${formatDay(b)}, ${formatClock(b)}`;
}
// The sections the server couldn't read in time, and how to try again,
// for SectionGate.
const MonitoringState = createContext<{
  unavailable: string[];
  retry: () => void;
}>({ unavailable: [], retry: () => {} });
// The parts a page couldn't read in time (a page's `unavailable`; none for
// a page without parts).
function unavailableParts(data: object): string[] {
  return "unavailable" in data && Array.isArray(data.unavailable)
    ? data.unavailable.filter((p): p is string => typeof p === "string")
    : [];
}
// The page's states around its content: the failure (keeping what was
// shown, with Retry), a skeleton while nothing has loaded, the content
// dimmed while a new window loads.
export function MonitoringBody<V extends object>({
  resource,
  label,
  children,
}: {
  resource: {
    data?: V;
    error: string | null;
    loading: boolean;
    refreshing: boolean;
    refresh: () => void;
  };
  label: string;
  children: (view: V) => ReactNode;
}) {
  return (
    <>
      {resource.error && (
        <Notice variant="error" className="mt-6" onRetry={resource.refresh}>
          {resource.error}
        </Notice>
      )}
      {resource.data ? (
        <div
          aria-busy={resource.refreshing}
          className={cx(resource.refreshing && "opacity-60")}
        >
          <MonitoringState.Provider
            value={{
              unavailable: unavailableParts(resource.data),
              retry: resource.refresh,
            }}
          >
            {children(resource.data)}
          </MonitoringState.Provider>
        </div>
      ) : (
        resource.loading && <ListSkeleton label={label} rows={4} />
      )}
    </>
  );
}
const counts = new Intl.NumberFormat("en-US", { maximumFractionDigits: 1 });
// A signal's value in its unit: "1,204", "52.3%", "1.8 s", "12 min".
export function formatSignal(
  signal: Pick<Signal, "unit" | "value"> & { sample?: number },
): string {
  const v = signal.value;
  // An age of nothing: there's nothing waiting.
  if (signal.unit === "minutes" && signal.sample === 0) return "None waiting";
  switch (signal.unit) {
    case "percent":
      return `${counts.format(v)}%`;
    case "ms":
      return v >= 1000 ? `${counts.format(v / 1000)} s` : `${Math.round(v)} ms`;
    case "minutes":
      return v < 1 ? "under a minute" : `${counts.format(v)} min`;
    case "per_minute":
      return `${counts.format(v)}/min`;
    default:
      return counts.format(v);
  }
}
// What a signal counts over: "Last hour", "Last 24 hours", "Now".
export function signalWindow(signal: Signal): string {
  const s = signal.windowSeconds;
  if (s === 3600) return "Last hour";
  if (s === 86400) return "Last 24 hours";
  if (s === 7 * 86400) return "Last 7 days";
  if (s === 360) return "Last 6 minutes";
  return "Now";
}
// Said where metric numbers would be when the live servers don't store
// them (METRICS_STORE=off): they read zero, which isn't "no traffic".
export function MetricsStoreNotice({
  store,
  className,
}: {
  store: MetricsStore;
  className?: string;
}) {
  if (store !== "off" && store !== "mixed") return null;
  return (
    <Notice variant="warning" className={className}>
      {store === "off"
        ? "Metric storage is off (METRICS_STORE=off): requests, latency, the pool and the worker read zero here. /metrics and the logs still have them."
        : "Metric storage is off on some live servers (METRICS_STORE=off): their requests, latency, pool and worker aren't counted here."}
    </Notice>
  );
}
// Its thresholds, in words: "Warning at 100 · critical at 1,000".
export function thresholds(signal: Signal): string | null {
  const parts: string[] = [];
  const at = (v: number) => formatSignal({ unit: signal.unit, value: v });
  if (signal.warn) parts.push(`warning at ${at(signal.warn)}`);
  if (signal.critical) parts.push(`critical at ${at(signal.critical)}`);
  if (!parts.length) return null;
  const text = parts.join(" · ");
  const min = signal.minSample
    ? ` (from ${counts.format(signal.minSample)})`
    : "";
  return text[0].toUpperCase() + text.slice(1) + min;
}
// The Events page filtered to a signal's rows.
export function useDrillHref() {
  const key = useAdminKey();
  return (drill?: Record<string, string>) =>
    drill
      ? adminRoutes.events.index(key, eventFilters(new URLSearchParams(drill)))
      : undefined;
}
// A signal as a tile: its label (linking to its Events rows), its level
// in words, its value, and its window and thresholds.
export function SignalTile({
  signal,
  chart,
  href,
  children,
}: {
  signal: Signal;
  chart?: ReactNode;
  href?: string;
  children?: ReactNode;
}) {
  const drill = useDrillHref();
  const limits = thresholds(signal);
  if (signal.unavailable) {
    return (
      <Stat title={signal.label} value="Not loaded">
        <span className="text-zinc-500 dark:text-zinc-400">
          It took too long to read. Try again in a moment.
        </span>
      </Stat>
    );
  }
  return (
    <Stat
      title={signal.label}
      value={formatSignal(signal)}
      level={signal.level}
      href={href ?? drill(signal.drill)}
      chart={chart}
    >
      <span className="text-zinc-500 dark:text-zinc-400">
        {signalWindow(signal)}
        {limits && ` · ${limits}`}
      </span>
      {children}
    </Stat>
  );
}
// Signals as a grid of tiles.
export function SignalGrid({
  signals,
  className,
}: {
  signals: Signal[];
  className?: string;
}) {
  return (
    <div
      className={cx(
        className,
        "grid gap-8 sm:grid-cols-2 xl:grid-cols-4 [&>*]:min-w-0",
      )}
    >
      {signals.map((s) => (
        <SignalTile key={s.name} signal={s} />
      ))}
    </div>
  );
}
// The signals named, in that order (missing ones left out).
export function pickSignals(signals: Signal[], names: string[]): Signal[] {
  return names.flatMap((n) => signals.filter((s) => s.name === n));
}
// A bucket's label: "14:30" up to 2 days, "Oct 31 15:00" beyond.
export function bucketLabel(start: string, bucketSeconds: number): string {
  const time = new Date(start);
  return bucketSeconds >= 3 * 3600
    ? `${formatDay(time)} ${formatClock(time)}`
    : formatClock(time);
}
// A series' columns as chart rows, its series keyed and labelled by
// `label`.
export function seriesRows(
  m: MonitorSeries,
  bucketSeconds: number,
): { label: string; values: Record<string, number> }[] {
  return m.buckets.map((b) => ({
    label: bucketLabel(b.start, bucketSeconds),
    values: Object.fromEntries(m.series.map((s, i) => [s, b.counts[i] ?? 0])),
  }));
}
// A section that reads the parts named: when the server couldn't read one
// in time (each part has its own budget, so the rest of the page still
// shows), its heading and a notice with Try again in its place. Its
// children aren't rendered then: their data is missing.
export function SectionGate({
  names,
  title,
  children,
}: {
  names: string[];
  title: string;
  children: ReactNode;
}) {
  const { unavailable, retry } = useContext(MonitoringState);
  if (!names.some((n) => unavailable.includes(n))) return children;
  return (
    <MonitoringSection title={title}>
      <Notice variant="error" onRetry={retry}>
        This section took too long to load.
      </Notice>
    </MonitoringSection>
  );
}
// A section of a page: its heading, a line saying what it shows, then its
// content.
export function MonitoringSection({
  title,
  description,
  children,
  className,
}: {
  title: string;
  description?: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={cx(className, "mt-14")}>
      <Subheading>{title}</Subheading>
      {description && <Text className="mt-1">{description}</Text>}
      <div className="mt-4">{children}</div>
    </section>
  );
}
// A stacked chart of a series over the page's window, its numbers a table
// on request, with a summary sentence of its total and busiest column.
export function SeriesChart({
  label,
  series,
  labels,
  bucketSeconds,
  noun,
}: {
  label: string;
  series: MonitorSeries;
  labels: Record<string, string>;
  bucketSeconds: number;
  noun: [string, string];
}) {
  const rows = seriesRows(series, bucketSeconds);
  const chartSeries: ChartSeries<string>[] = series.series.map((s) => ({
    key: s,
    label: labels[s] ?? s,
  }));
  const totals = rows.map((r) =>
    Object.values(r.values).reduce((a, b) => a + b, 0),
  );
  const total = totals.reduce((a, b) => a + b, 0);
  const busiest = rows[totals.indexOf(Math.max(...totals))];
  const summary =
    total === 0
      ? `No ${noun[1]} in this window.`
      : `${total.toLocaleString("en-US")} ${total === 1 ? noun[0] : noun[1]}, the most at ${busiest.label} UTC.`;
  return (
    <BarChart
      label={label}
      summary={summary}
      rows={rows}
      series={chartSeries}
      tableToggle
    />
  );
}
// A review list's or a count's table: a dense table with a header row.
export function MonitoringTable({
  head,
  children,
}: {
  head: ReactNode[];
  children: ReactNode;
}) {
  return (
    <Table dense>
      <TableHead>
        <TableRow>
          {head.map((h, i) => (
            <TableHeader key={i}>{h}</TableHeader>
          ))}
        </TableRow>
      </TableHead>
      <TableBody>{children}</TableBody>
    </Table>
  );
}
// An address (or an IPv6 /64) linking to its page, over the window shown.
export function AddressLink({
  ip,
  window,
}: {
  ip: string;
  window?: MonitoringWindow;
}) {
  const key = useAdminKey();
  return (
    <TextLink
      href={adminRoutes.monitoring.ip(key, ip, window)}
      className="font-mono"
    >
      {ip}
    </TextLink>
  );
}
// A short empty line for a review list with nothing in it.
export function NoneFound({ children }: { children: ReactNode }) {
  return (
    <Text className="rounded-lg bg-zinc-950/[2.5%] px-4 py-3 dark:bg-white/[2.5%]">
      {children}
    </Text>
  );
}
// A limit shared by everyone against its cap: its live window's use as a
// meter, with its level in words (half used: warning; reached: critical,
// the feature is off for everyone until the window ends), and how often it
// was reached in the page's window.
export function SharedLimitMeter({ limit }: { limit: SharedLimit }) {
  const level: Level =
    limit.hits >= limit.max
      ? "critical"
      : limit.hits * 2 >= limit.max
        ? "warn"
        : "ok";
  const used = `${limit.hits.toLocaleString("en-US")} of ${limit.max.toLocaleString("en-US")}`;
  return (
    <div>
      <div className="flex items-center justify-between gap-2 text-sm/6">
        <Code>{limit.name}</Code>
        <span className="flex items-center gap-2">
          <span className="text-zinc-500 tabular-nums dark:text-zinc-400">
            {used}
          </span>
          <LevelBadge level={level} />
        </span>
      </div>
      <Meter
        className="mt-2"
        label={`${limit.name} used`}
        value={limit.hits}
        max={limit.max}
        level={level}
        valueText={`${used} used${limit.resetsAt ? `, until ${formatClock(new Date(limit.resetsAt))} UTC` : ""}`}
      />
      <div className="mt-1 text-sm/6 text-zinc-500 sm:text-xs/6 dark:text-zinc-400">
        {limit.resetsAt
          ? `This window ends at ${formatClock(new Date(limit.resetsAt))} UTC.`
          : "No live window."}{" "}
        {limit.reached > 0
          ? `Reached ${limit.reached} ${limit.reached === 1 ? "time" : "times"} in this window, last ${formatDay(new Date(limit.lastReached!))}, ${formatClock(new Date(limit.lastReached!))} UTC.`
          : "Not reached in this window."}
      </div>
    </div>
  );
}
