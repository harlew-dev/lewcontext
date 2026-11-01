import { useId, useState, type ReactNode } from "react";
import { useReducedMotion } from "motion/react";
import {
  Area,
  AreaChart,
  Bar,
  BarChart as Bars,
  CartesianGrid,
  Rectangle,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
  type BarRectangleItem,
  type BarShapeProps,
  type TooltipContentProps,
} from "recharts";
import { cx } from "@/lib/cx";
import { Link } from "./link";
import { TextButton } from "./text";
import type { Level } from "./badge";
// Charts, drawn by Recharts (docs/ui/design.md). This is the only file that
// imports it: pages pass rows and series and never style Recharts.

// Series colours, in this fixed order and never cycled: a sixth series is
// folded into "Other" by the page. Checked with the dataviz validator against
// white and zinc-900 with every pair compared, since any two segments of a
// stack can touch: full-colour ΔE ≥ 16.8, every mark ≥ 3:1, colour-blind
// ΔE ≥ 7.3 (the band that needs a second cue: the gaps, legend, tooltip and
// table).
const seriesColors = [
  "text-sky-600",
  "text-orange-600",
  "text-emerald-600",
  "text-violet-600 dark:text-violet-500",
  "text-pink-500",
] as const;
export type ChartSeries<K extends string> = { key: K; label: string };
export type ChartRow<K extends string> = {
  label: string;
  values: Record<K, number>;
};
// A column per row (a time bucket), its series stacked: a legend above, a
// tooltip per column, and the numbers as a table for screen readers (the
// SVG is hidden from them). `label` names the chart; `summary` says in a
// sentence what it shows. With `onSelect`, clicking a column's bar picks its
// row, and the tooltip ends with `selectHint`. It's a pointer shortcut only
// (the SVG is hidden from the keyboard and screen readers), so a page using
// it must offer the same another way: the Events page's custom range takes
// days and UTC times, reaching any column.
export function BarChart<
  K extends string,
  R extends ChartRow<K> = ChartRow<K>,
>({
  label,
  summary,
  rows,
  series,
  onSelect,
  selectHint,
  tableToggle = false,
  className,
}: {
  label: string;
  summary: string;
  rows: R[];
  series: ChartSeries<K>[];
  onSelect?: (row: R) => void;
  selectHint?: string;
  // A "Show as table" button under the chart, which shows its numbers to
  // everyone (they're always there for screen readers).
  tableToggle?: boolean;
  className?: string;
}) {
  const reducedMotion = useReducedMotion() ?? false;
  const summaryId = useId();
  const [asTable, setAsTable] = useState(false);
  if (series.length > seriesColors.length)
    throw new Error(`BarChart: at most ${seriesColors.length} series`);
  const total = (row: ChartRow<K>) =>
    series.reduce((sum, { key }) => sum + row.values[key], 0);
  return (
    <figure className={cx(className)} aria-describedby={summaryId}>
      <figcaption>
        <span className="sr-only">{label}. </span>
        <span
          id={summaryId}
          className="text-sm/6 text-zinc-500 dark:text-zinc-400"
        >
          {summary}
        </span>
      </figcaption>
      {series.length > 1 && <Legend series={series} />}
      <div
        aria-hidden="true"
        className={cx(
          "mt-4 h-60 text-xs text-zinc-500 dark:text-zinc-400",
          onSelect && "cursor-pointer",
        )}
      >
        <ResponsiveContainer width="100%" height="100%">
          <Bars
            data={rows}
            margin={{ top: 4, right: 0, bottom: 0, left: -16 }}
            accessibilityLayer={false}
          >
            <CartesianGrid
              vertical={false}
              stroke="currentColor"
              className="text-zinc-950/10 dark:text-white/10"
            />
            <XAxis
              dataKey="label"
              axisLine={false}
              tickLine={false}
              tick={{ fill: "currentColor" }}
              minTickGap={24}
            />
            <YAxis
              allowDecimals={false}
              tickFormatter={compact}
              axisLine={false}
              tickLine={false}
              tick={{ fill: "currentColor" }}
              width={48}
            />
            <Tooltip
              content={(props) => (
                <ChartTooltip {...props} series={series} hint={selectHint} />
              )}
              cursor={{
                fill: "currentColor",
                className: "text-zinc-950/5 dark:text-white/5",
              }}
              isAnimationActive={false}
            />
            {series.map(({ key, label }, i) => (
              <Bar
                key={key}
                name={label}
                dataKey={(row: ChartRow<K>) => row.values[key]}
                stackId="stack"
                maxBarSize={24}
                fill="currentColor"
                className={seriesColors[i]}
                isAnimationActive={!reducedMotion}
                // The bar's own row: the chart's hover index can lag a
                // quick pointer.
                onClick={
                  onSelect &&
                  ((bar: BarRectangleItem) => onSelect(bar.payload as R))
                }
                shape={(props: BarShapeProps) => (
                  <Segment
                    {...props}
                    isTop={topKey(props.payload as ChartRow<K>, series) === key}
                  />
                )}
              />
            ))}
          </Bars>
        </ResponsiveContainer>
      </div>
      <ChartTable
        label={label}
        rows={rows}
        series={series}
        total={total}
        visible={asTable}
      />
      {tableToggle && (
        <TextButton
          className="mt-2 text-sm/6"
          aria-expanded={asTable}
          onClick={() => setAsTable(!asTable)}
        >
          {asTable ? "Hide table" : "Show as table"}
        </TextButton>
      )}
    </figure>
  );
}
// An axis tick, short enough for its gutter: 950, 1.2K, 35K, 1.4M.
const compactFormat = new Intl.NumberFormat("en-US", { notation: "compact" });
function compact(value: number): string {
  return compactFormat.format(value);
}
// The last series with a value in a row: its segment gets the rounded end.
function topKey<K extends string>(
  row: ChartRow<K>,
  series: ChartSeries<K>[],
): K | undefined {
  return series.findLast(({ key }) => row.values[key] > 0)?.key;
}
// A stacked segment: a 2px gap in the surface colour between segments
// (a stroke the colour of the card), and a 4px rounded end on the top one,
// square at the baseline.
function Segment({ isTop, ...props }: BarShapeProps & { isTop: boolean }) {
  return (
    <Rectangle
      {...props}
      radius={isTop ? [4, 4, 0, 0] : 0}
      strokeWidth={2}
      className="stroke-white dark:stroke-zinc-900"
    />
  );
}
function Legend<K extends string>({ series }: { series: ChartSeries<K>[] }) {
  return (
    <ul className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-sm/6 text-zinc-600 dark:text-zinc-300">
      {series.map(({ key, label }, i) => (
        <li key={key} className="flex items-center gap-2">
          <span
            aria-hidden="true"
            className={cx(seriesColors[i], "size-2.5 rounded-xs bg-current")}
          />
          {label}
        </li>
      ))}
    </ul>
  );
}
// The column's values, the value first, every series, keyed by a short
// line of its colour.
function ChartTooltip<K extends string>({
  active,
  payload,
  label,
  series,
  hint,
}: TooltipContentProps & { series: ChartSeries<K>[]; hint?: string }) {
  if (!active || !payload?.length) return null;
  const row = payload[0]?.payload as ChartRow<K>;
  return (
    <div className="rounded-lg bg-white/95 px-3 py-2 text-xs shadow-lg ring-1 ring-zinc-950/10 dark:bg-zinc-800/95 dark:ring-white/10">
      <div className="font-medium text-zinc-950 dark:text-white">{label}</div>
      <ul className="mt-1 space-y-0.5">
        {series.map(({ key, label }, i) => (
          <li key={key} className="flex items-center gap-2">
            <span
              aria-hidden="true"
              className={cx(
                seriesColors[i],
                "h-0.5 w-3 rounded-full bg-current",
              )}
            />
            <span className="font-semibold text-zinc-950 tabular-nums dark:text-white">
              {row.values[key]}
            </span>
            <span className="text-zinc-500 dark:text-zinc-400">{label}</span>
          </li>
        ))}
      </ul>
      {hint && (
        <div className="mt-1.5 text-zinc-500 dark:text-zinc-400">{hint}</div>
      )}
    </div>
  );
}
// The chart's numbers as a table, for screen readers.
function ChartTable<K extends string>({
  label,
  rows,
  series,
  total,
  visible = false,
}: {
  label: string;
  rows: ChartRow<K>[];
  series: ChartSeries<K>[];
  total: (row: ChartRow<K>) => number;
  visible?: boolean;
}) {
  return (
    <table
      className={
        visible
          ? "mt-4 w-full text-left text-sm/6 tabular-nums *:*:*:border-b *:*:*:border-zinc-950/5 *:*:*:py-1 *:*:*:pr-4 dark:*:*:*:border-white/5"
          : "sr-only"
      }
    >
      <caption>{label}</caption>
      <thead>
        <tr>
          <th scope="col">Time</th>
          {series.map(({ key, label }) => (
            <th key={key} scope="col">
              {label}
            </th>
          ))}
          <th scope="col">Total</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <tr key={row.label}>
            <th scope="row">{row.label}</th>
            {series.map(({ key }) => (
              <td key={key}>{row.values[key]}</td>
            ))}
            <td>{total(row)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
export type BarListItem = {
  key: string;
  label: ReactNode;
  value: number;
  href: string;
};
// A ranked list of links, each over a bar as wide as its share of the
// largest: the name, the count and its share of `total`. Plain HTML, so it
// reads as a list of links as it is, with no hidden table.
export function BarList({
  label,
  items,
  total,
  className,
}: {
  label: string;
  items: BarListItem[];
  total: number;
  className?: string;
}) {
  const largest = Math.max(1, ...items.map((item) => item.value));
  return (
    <ol aria-label={label} className={cx(className, "space-y-1")}>
      {items.map((item) => (
        <li key={item.key}>
          <Link
            href={item.href}
            className="group relative flex items-center justify-between gap-x-4 rounded-md px-2 py-1.5 text-sm/6 data-focus:outline-2 data-focus:outline-blue-500"
          >
            <span
              aria-hidden="true"
              style={
                { "--share": `${(item.value / largest) * 100}%` } as object
              }
              className="absolute inset-y-0 left-0 w-(--share) rounded-md bg-sky-600/15 group-data-hover:bg-sky-600/25 dark:bg-sky-500/20 dark:group-data-hover:bg-sky-500/30"
            />
            <span className="relative min-w-0 truncate text-zinc-950 dark:text-white">
              {item.label}
            </span>
            <span className="relative shrink-0 text-zinc-500 tabular-nums dark:text-zinc-400">
              <span className="font-medium text-zinc-950 dark:text-white">
                {item.value.toLocaleString("en-US")}
              </span>
              {" · "}
              {percent(item.value, total)}
            </span>
          </Link>
        </li>
      ))}
    </ol>
  );
}
// A share as a whole percentage, "<1%" for a small one.
function percent(value: number, total: number): string {
  if (total <= 0) return "0%";
  const share = (value / total) * 100;
  return share > 0 && share < 1 ? "<1%" : `${Math.round(share)}%`;
}

// A tile's trend: one series over time as a small area, with a tooltip per
// point, the numbers in a table for screen readers (the SVG is hidden from
// them) and `summary` saying what it shows. One series, so no legend: the
// tile names it. Sky, the first series colour; a level is the tile's
// badge, never the line's colour.
export type SparkPoint = { label: string; value: number };
export function Sparkline({
  label,
  summary,
  points,
  format = (value) => value.toLocaleString("en-US"),
  className,
}: {
  label: string;
  summary: string;
  points: SparkPoint[];
  format?: (value: number) => string;
  className?: string;
}) {
  const reducedMotion = useReducedMotion() ?? false;
  const gradient = useId();
  return (
    <figure className={cx(className)}>
      <figcaption className="sr-only">
        {label}. {summary}
      </figcaption>
      <div aria-hidden="true" className="h-10 text-sky-600 dark:text-sky-500">
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart
            data={points}
            margin={{ top: 2, right: 0, bottom: 0, left: 0 }}
            accessibilityLayer={false}
          >
            <defs>
              <linearGradient id={gradient} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="currentColor" stopOpacity={0.25} />
                <stop offset="100%" stopColor="currentColor" stopOpacity={0} />
              </linearGradient>
            </defs>
            <Tooltip
              content={({ active, payload }) => {
                const point = payload?.[0]?.payload as SparkPoint | undefined;
                if (!active || !point) return null;
                return (
                  <div className="rounded-lg bg-white/95 px-2 py-1 text-xs shadow-lg ring-1 ring-zinc-950/10 dark:bg-zinc-800/95 dark:ring-white/10">
                    <span className="font-semibold text-zinc-950 tabular-nums dark:text-white">
                      {format(point.value)}
                    </span>{" "}
                    <span className="text-zinc-500 dark:text-zinc-400">
                      {point.label}
                    </span>
                  </div>
                );
              }}
              cursor={{ stroke: "currentColor", strokeOpacity: 0.3 }}
              isAnimationActive={false}
            />
            <Area
              type="monotone"
              dataKey="value"
              stroke="currentColor"
              strokeWidth={2}
              fill={`url(#${gradient})`}
              isAnimationActive={!reducedMotion}
            />
          </AreaChart>
        </ResponsiveContainer>
      </div>
      <table className="sr-only">
        <caption>{label}</caption>
        <thead>
          <tr>
            <th scope="col">Time</th>
            <th scope="col">{label}</th>
          </tr>
        </thead>
        <tbody>
          {points.map((p) => (
            <tr key={p.label}>
              <th scope="row">{p.label}</th>
              <td>{format(p.value)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </figure>
  );
}
const meterLevels: Record<Level, string> = {
  ok: "bg-sky-600 dark:bg-sky-500",
  warn: "bg-amber-500",
  critical: "bg-red-600 dark:bg-red-500",
};
// How full something capped is: a bar to `max`, filled in sky, or in a
// level's colour once it has one (the page writes the level's word beside
// it, never colour alone). A native meter for assistive technology, its
// value text written out.
export function Meter({
  label,
  value,
  max,
  level = "ok",
  valueText,
  className,
}: {
  label: string;
  value: number;
  max: number;
  level?: Level;
  valueText: string;
  className?: string;
}) {
  const share = max > 0 ? Math.min(1, value / max) : 0;
  return (
    <div
      role="meter"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={max}
      aria-valuenow={Math.min(value, max)}
      aria-valuetext={valueText}
      className={cx(
        className,
        "h-2 overflow-hidden rounded-full bg-zinc-950/5 dark:bg-white/10",
      )}
    >
      <div
        style={{ "--share": `${share * 100}%` } as object}
        className={cx("h-full w-(--share) rounded-full", meterLevels[level])}
      />
    </div>
  );
}
