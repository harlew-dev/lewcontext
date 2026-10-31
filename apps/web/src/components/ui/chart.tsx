import { useId } from "react";
import { useReducedMotion } from "motion/react";
import {
  Bar,
  BarChart as Bars,
  CartesianGrid,
  Rectangle,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
  type BarShapeProps,
  type TooltipContentProps,
} from "recharts";
import { cx } from "@/lib/cx";
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
// sentence what it shows.
export function BarChart<K extends string>({
  label,
  summary,
  rows,
  series,
  className,
}: {
  label: string;
  summary: string;
  rows: ChartRow<K>[];
  series: ChartSeries<K>[];
  className?: string;
}) {
  const reducedMotion = useReducedMotion() ?? false;
  const summaryId = useId();
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
        className="mt-4 h-60 text-xs text-zinc-500 dark:text-zinc-400"
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
              axisLine={false}
              tickLine={false}
              tick={{ fill: "currentColor" }}
              width={48}
            />
            <Tooltip
              content={(props) => <ChartTooltip {...props} series={series} />}
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
      <ChartTable label={label} rows={rows} series={series} total={total} />
    </figure>
  );
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
}: TooltipContentProps & { series: ChartSeries<K>[] }) {
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
    </div>
  );
}
// The chart's numbers as a table, for screen readers.
function ChartTable<K extends string>({
  label,
  rows,
  series,
  total,
}: {
  label: string;
  rows: ChartRow<K>[];
  series: ChartSeries<K>[];
  total: (row: ChartRow<K>) => number;
}) {
  return (
    <table className="sr-only">
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
