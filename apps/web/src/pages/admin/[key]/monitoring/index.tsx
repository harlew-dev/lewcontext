import { LevelBadge, type Level } from "@/components/ui/badge";
import { Sparkline } from "@/components/ui/chart";
import { Stat } from "@/components/ui/stat";
import {
  bucketLabel,
  formatSignal,
  MonitoringBody,
  MonitoringHeader,
  signalWindow,
  thresholds,
  useDrillHref,
  useMonitoringPage,
} from "@/components/app/admin/monitoring";
import { useAdminKey } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import type {
  MonitoringTile,
  MonitoringView,
  MonitoringWindow,
  OverviewSections,
  Signal,
} from "@/types/admin";
// The Monitoring Overview: is everything all right, and if not, where? A
// tile per area, each with its signals' levels in words, a sparkline over
// the window, and a link to the page behind it (or its Events rows while
// that page isn't built). One request, one recorded read.
export default function AdminMonitoringOverview() {
  const page = useMonitoringPage<OverviewSections>(
    "overview",
    adminRoutes.monitoring.index,
  );
  return (
    <>
      <MonitoringHeader
        title="Overview"
        window={page.window}
        onWindow={page.setWindow}
        view={page.data}
      />
      <MonitoringBody resource={page} label="Loading the overview">
        {(view) => <Tiles view={view} />}
      </MonitoringBody>
    </>
  );
}
function Tiles({ view }: { view: MonitoringView<OverviewSections> }) {
  return (
    <div className="mt-8 grid gap-x-8 gap-y-10 sm:grid-cols-2 xl:grid-cols-3 [&>*]:min-w-0">
      {view.sections.tiles.map((tile) => (
        <Tile key={tile.id} tile={tile} view={view} />
      ))}
    </div>
  );
}
const levelOrder: Level[] = ["ok", "warn", "critical"];
// The worst level among a tile's signals.
function worst(signals: Signal[]): Level {
  return signals.reduce<Level>(
    (level, s) =>
      !s.unavailable && levelOrder.indexOf(s.level) > levelOrder.indexOf(level)
        ? s.level
        : level,
    "ok",
  );
}
// Where a tile's page is: Security and Rate limits are built, Mail is its
// own page; the rest (Performance, Errors, Usage, Configuration) come
// later, so their tiles link to their Events rows if they have any.
function usePageHref() {
  const key = useAdminKey();
  return (page: string, window: MonitoringWindow): string | undefined => {
    switch (page) {
      case "security":
        return adminRoutes.monitoring.security(key, window);
      case "limits":
        return adminRoutes.monitoring.limits(key, window);
      case "mail":
        return adminRoutes.deliveries.index(key);
    }
    return undefined;
  };
}
function Tile({
  tile,
  view,
}: {
  tile: MonitoringTile;
  view: MonitoringView<OverviewSections>;
}) {
  const pageHref = usePageHref();
  const drill = useDrillHref();
  const signals = tile.signals.flatMap((name) =>
    view.signals.filter((s) => s.name === name),
  );
  const [first, ...rest] = signals;
  if (!first) return null;
  const href = pageHref(tile.page, view.window.name) ?? drill(first.drill);
  const limits = thresholds(first);
  return (
    <Stat
      title={tile.label}
      value={first.unavailable ? "Not loaded" : formatSignal(first)}
      level={worst(signals)}
      href={href}
      chart={
        tile.series && (
          <Sparkline
            label={`${tile.series.label} over the window`}
            summary={sparkSummary(tile, view)}
            points={tile.series.points.map((p) => ({
              label: bucketLabel(p.start, view.window.bucketSeconds),
              value: p.value,
            }))}
            format={(value) => formatSignal({ unit: tile.series!.unit, value })}
          />
        )
      }
    >
      <div className="text-zinc-500 dark:text-zinc-400">
        {first.label} · {signalWindow(first).toLowerCase()}
        {limits && ` · ${limits.toLowerCase()}`}
      </div>
      {rest.length > 0 && (
        <ul className="mt-2 space-y-1">
          {rest.map((s) => (
            <li key={s.name} className="flex items-center gap-2">
              <span className="font-medium text-zinc-950 tabular-nums dark:text-white">
                {s.unavailable ? "Not loaded" : formatSignal(s)}
              </span>
              <span className="text-zinc-500 dark:text-zinc-400">
                {s.label}
              </span>
              {s.level !== "ok" && <LevelBadge level={s.level} />}
            </li>
          ))}
        </ul>
      )}
      {!href && (
        <div className="mt-2 text-zinc-500 dark:text-zinc-400">
          Its page comes later.
        </div>
      )}
    </Stat>
  );
}
// "Failed sign-ins over the window: 312 in all, the most at 14:30 UTC (41)."
function sparkSummary(
  tile: MonitoringTile,
  view: MonitoringView<OverviewSections>,
): string {
  const points = tile.series?.points ?? [];
  if (points.length === 0) return "No data in this window.";
  const top = points.reduce((a, b) => (b.value > a.value ? b : a));
  const peak = formatSignal({ unit: tile.series!.unit, value: top.value });
  return `Highest at ${bucketLabel(top.start, view.window.bucketSeconds)} UTC: ${peak}.`;
}
