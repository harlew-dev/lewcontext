import { useMemo, useState, type FormEvent, type ReactNode } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { FunnelIcon } from "@heroicons/react/16/solid";
import { CursorNav } from "@/components/app/admin/cursor-nav";
import {
  canonicalTokens,
  compressNames,
  eventFilters,
  expandTokens,
  filterPatterns,
  formatClock,
  formatDay,
  isFiltered,
  localDay,
  nameIndex,
  narrowingFilters,
  rangeLabel,
  selectionLabel,
  utcDay,
  windowCaption,
  windowLabel,
  windowLabels,
  type NameIndex,
} from "@/components/app/admin/event-filters";
import { EventTable } from "@/components/app/admin/events";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Calendar, type CalendarValue } from "@/components/ui/calendar";
import { BarChart, BarList, type ChartRow } from "@/components/ui/chart";
import { Combobox, type ComboboxNode } from "@/components/ui/combobox";
import {
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { EmptyState, ListSkeleton, Notice } from "@/components/ui/feedback";
import {
  Description,
  ErrorMessage,
  Field,
  Label,
} from "@/components/ui/fieldset";
import { Heading, Subheading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import { Stat } from "@/components/ui/stat";
import { Text, TextButton } from "@/components/ui/text";
import { Tooltip } from "@/components/ui/tooltip";
import { adminPaths } from "@/lib/api/admin";
import { useAdminKey } from "@/lib/admin";
import { adminRoutes, eventQuery } from "@/lib/admin-routes";
import { cx } from "@/lib/cx";
import { parsePublicId } from "@/lib/ids";
import { useAdminResource } from "@/lib/use-admin-resource";
import { useOpenCount } from "@/lib/use-open-count";
import {
  eventWindows,
  failureLabels,
  type AdminEventPage,
  type EventFilters,
  type EventSummary,
  type EventWindow,
} from "@/types/admin";
// The audit trail: charts of what happened in the window, then every event,
// newest first. The time range sits in the header; the other filters in a
// dialog, shown once applied as removable badges. Everything lives in the
// URL, so a link opens the same view. Every load is one recorded read
// (admin.event.listed: its filters and the charts together), so nothing
// reloads on its own and nothing applies as you type.
export default function AdminEventIndex() {
  const key = useAdminKey();
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const catalogue = useAdminResource(adminPaths.catalogue());
  const index = useMemo(
    () => (catalogue.data ? nameIndex(catalogue.data) : null),
    [catalogue.data],
  );
  const filters = canonical(eventFilters(search), index);
  // Names from a link are made canonical with the catalogue first, so the
  // API never gets one it would refuse.
  const waiting = Boolean(filters.names?.length) && !index;
  const list = useAdminResource(
    waiting
      ? null
      : adminPaths.events.index({ ...filters, summary: !filters.before }),
  );
  const charts = useCharts(filters, list);
  const [dialog, setDialog] = useState<"filters" | "range" | null>(null);
  const apply = (next: EventFilters) =>
    void navigate(
      adminRoutes.events.index(key, { ...next, before: undefined }),
      { preventScrollReset: true },
    );
  const time = { window: filters.window, from: filters.from, to: filters.to };
  const sameWindow = (window: EventWindow) =>
    window === (filters.window ?? "24h") && !filters.from && !filters.to;
  return (
    <>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <Heading>Events</Heading>
          <Text className="mt-1">
            {list.data && !list.stale
              ? windowCaption(list.data.window)
              : windowLabel(filters)}
          </Text>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <RangeListbox
            filters={filters}
            onWindow={(window) =>
              sameWindow(window)
                ? list.refresh()
                : apply({ ...narrowingFilters(filters), window })
            }
            onCustom={() => setDialog("range")}
          />
          <FiltersButton
            count={filterBadges(filters, undefined).length}
            onClick={() => setDialog("filters")}
          />
        </div>
      </div>
      <FilterBadges
        filters={filters}
        refs={list.data?.refs}
        time={time}
        onRemove={apply}
        onOpen={() => setDialog("filters")}
      />
      {catalogue.error && waiting && (
        <Notice variant="error" className="mt-6" onRetry={catalogue.refresh}>
          {catalogue.error}
        </Notice>
      )}
      {list.error && (
        <Notice variant="error" className="mt-6" onRetry={list.refresh}>
          {list.error}
        </Notice>
      )}
      <Charts
        state={charts}
        filters={filters}
        index={index}
        onApply={apply}
        onRetry={list.refresh}
      />
      <EventList
        list={list}
        waiting={waiting && !catalogue.error}
        filters={filters}
        onApply={apply}
      />
      <FiltersDialog
        open={dialog === "filters"}
        onClose={() => setDialog(null)}
        filters={filters}
        index={index}
        onApply={(next) => {
          setDialog(null);
          apply({ ...next, ...time });
        }}
      />
      <RangeDialog
        open={dialog === "range"}
        onClose={() => setDialog(null)}
        filters={filters}
        onApply={(from, to) => {
          setDialog(null);
          apply({ ...narrowingFilters(filters), from, to });
        }}
      />
    </>
  );
}
// The URL's names as the page writes them, once the catalogue is in;
// unknown ones dropped.
function canonical(
  filters: EventFilters,
  index: NameIndex | null,
): EventFilters {
  if (!filters.names || !index) return filters;
  const names = canonicalTokens(filters.names, index);
  return { ...filters, names: names.length > 0 ? names : undefined };
}
type EventListResource = ReturnType<typeof useAdminResource<AdminEventPage>>;
// What the charts show: the summary that came with the newest page, kept
// in this page's state (keyed by the filters without the cursor) while
// paging Older and Newer, and the last one dimmed while another loads.
type ChartState =
  | { kind: "summary"; summary: EventSummary; dim: boolean }
  | { kind: "loading" }
  | { kind: "older" }
  | { kind: "none" };
function useCharts(filters: EventFilters, list: EventListResource): ChartState {
  const summaryKey = JSON.stringify(
    eventQuery({ ...filters, before: undefined }),
  );
  const [kept, setKept] = useState<{
    key: string;
    summary: EventSummary;
  } | null>(null);
  const fresh = list.data !== undefined && !list.stale;
  const incoming = fresh ? list.data?.summary : undefined;
  if (incoming && (kept?.summary !== incoming || kept.key !== summaryKey))
    setKept({ key: summaryKey, summary: incoming });
  if (!fresh) {
    if (kept && list.loading)
      return { kind: "summary", summary: kept.summary, dim: true };
    return list.loading ? { kind: "loading" } : { kind: "none" };
  }
  if (incoming) return { kind: "summary", summary: incoming, dim: false };
  if (!filters.before) return { kind: "none" };
  return kept?.key === summaryKey
    ? { kind: "summary", summary: kept.summary, dim: false }
    : { kind: "older" };
}
// The time range: a window, or a custom range (shown as its value, and
// chosen again to change it).
function RangeListbox({
  filters,
  onWindow,
  onCustom,
}: {
  filters: EventFilters;
  onWindow: (window: EventWindow) => void;
  onCustom: () => void;
}) {
  const custom = Boolean(filters.from || filters.to);
  const value = custom ? "custom" : (filters.window ?? "24h");
  return (
    <Listbox
      aria-label="Time range"
      value={value}
      onChange={(v) => (v === "custom" ? onCustom() : onWindow(v))}
      className="sm:w-60"
    >
      {eventWindows.map((window) => (
        <ListboxOption key={window} value={window}>
          <ListboxLabel>{windowLabels[window]}</ListboxLabel>
        </ListboxOption>
      ))}
      <ListboxOption value="custom">
        <ListboxLabel>
          {custom ? rangeLabel(filters.from, filters.to) : "Custom range…"}
        </ListboxLabel>
      </ListboxOption>
    </Listbox>
  );
}
function FiltersButton({
  count,
  onClick,
}: {
  count: number;
  onClick: () => void;
}) {
  return (
    <Button outline onClick={onClick}>
      <FunnelIcon />
      Filters
      {count > 0 && (
        <Badge>
          {count}
          <span className="sr-only"> applied</span>
        </Badge>
      )}
    </Button>
  );
}
// One badge per applied filter, in the dialog's order: its text, a
// tooltip with every name for a long Events list, and the filters without
// it.
type FilterBadge = {
  id: string;
  text: string;
  tooltip?: string;
  without: EventFilters;
};
function filterBadges(
  filters: EventFilters,
  refs: AdminEventPage["refs"] | undefined,
): FilterBadge[] {
  const badges: FilterBadge[] = [];
  const without = (field: keyof EventFilters) => ({
    ...filters,
    [field]: undefined,
  });
  if (filters.names?.length)
    badges.push({
      id: "names",
      text: `Events: ${selectionLabel(filters.names)}`,
      tooltip: filters.names.length > 2 ? filters.names.join(", ") : undefined,
      without: without("names"),
    });
  if (filters.outcome)
    badges.push({
      id: "outcome",
      text: "Failed checks only",
      without: without("outcome"),
    });
  if (filters.account) {
    const ref = refs?.account?.id === filters.account ? refs.account : null;
    badges.push({
      id: "account",
      text: `Account: ${ref?.name || filters.account}`,
      without: without("account"),
    });
  }
  if (filters.workspace) {
    const ref =
      refs?.workspace?.id === filters.workspace ? refs.workspace : null;
    badges.push({
      id: "workspace",
      text: `Workspace: ${ref?.name || filters.workspace}`,
      without: without("workspace"),
    });
  }
  if (filters.email)
    badges.push({
      id: "email",
      text: `Email: ${filters.email}`,
      without: without("email"),
    });
  if (filters.ip)
    badges.push({
      id: "ip",
      text: `IP: ${filters.ip}`,
      without: without("ip"),
    });
  return badges;
}
function FilterBadges({
  filters,
  refs,
  time,
  onRemove,
  onOpen,
}: {
  filters: EventFilters;
  refs: AdminEventPage["refs"] | undefined;
  time: EventFilters;
  onRemove: (filters: EventFilters) => void;
  onOpen: () => void;
}) {
  const badges = filterBadges(filters, refs);
  if (badges.length === 0) return null;
  return (
    <ul
      aria-label="Applied filters"
      className="mt-4 flex flex-wrap items-center gap-2"
    >
      {badges.map((badge) => (
        <li key={badge.id}>
          <Badge
            onRemove={() => onRemove(badge.without)}
            removeLabel={`Remove filter: ${badge.text}`}
          >
            <BadgeText badge={badge} onClick={onOpen} />
          </Badge>
        </li>
      ))}
      {badges.length >= 2 && (
        <li>
          <TextButton className="text-sm/6" onClick={() => onRemove(time)}>
            Clear all
          </TextButton>
        </li>
      )}
    </ul>
  );
}
// A badge's text opens the dialog; the Events badge's tooltip lists every
// name when some are hidden.
function BadgeText({
  badge,
  onClick,
}: {
  badge: FilterBadge;
  onClick: () => void;
}) {
  const button = (
    <button
      type="button"
      onClick={onClick}
      className="max-w-72 truncate text-left focus-visible:outline-2 focus-visible:outline-blue-500"
    >
      {badge.text}
    </button>
  );
  return badge.tooltip ? (
    <Tooltip content={badge.tooltip}>{button}</Tooltip>
  ) : (
    button
  );
}
// The tiles and charts, or why they aren't shown.
function Charts({
  state,
  filters,
  index,
  onApply,
  onRetry,
}: {
  state: ChartState;
  filters: EventFilters;
  index: NameIndex | null;
  onApply: (filters: EventFilters) => void;
  onRetry: () => void;
}) {
  if (state.kind === "none") return null;
  if (state.kind === "loading") return <ChartsLoading />;
  if (state.kind === "older")
    return (
      <Text className="mt-8">
        Charts show with the newest events.{" "}
        <TextButton onClick={() => onApply(filters)}>Show newest</TextButton>
      </Text>
    );
  const { summary, dim } = state;
  if (summary.status === "too_wide")
    return (
      <Text className="mt-8">
        Charts cover 7 days or less.{" "}
        <TextButton
          onClick={() =>
            onApply({ ...narrowingFilters(filters), window: "7d" })
          }
        >
          Show the last 7 days
        </TextButton>
      </Text>
    );
  if (summary.status !== "ok")
    return (
      <Notice variant="warning" className="mt-8" onRetry={onRetry}>
        {summary.status === "busy"
          ? "Charts are busy. Try again in a moment."
          : "Charts took too long. Add filters or pick a shorter range."}
      </Notice>
    );
  if (summary.total === 0) return null;
  return (
    <div aria-busy={dim} className={cx("mt-8", dim && "opacity-60")}>
      <SummaryCharts
        summary={summary}
        filters={filters}
        index={index}
        onApply={onApply}
      />
    </div>
  );
}
const tileTitles = ["Events", "Failed checks", "Limits reached", "Busiest"];
function ChartsLoading() {
  return (
    <div className="mt-8">
      <div className="grid gap-8 sm:grid-cols-2 lg:grid-cols-4">
        {tileTitles.map((title) => (
          <Stat key={title} title={title} value={undefined} />
        ))}
      </div>
      <ChartPlaceholder className="mt-12" />
      <div className="mt-12 grid gap-12 lg:grid-cols-2">
        <ChartPlaceholder />
        <ChartPlaceholder />
      </div>
    </div>
  );
}
function ChartPlaceholder({ className }: { className?: string }) {
  return (
    <div
      role="img"
      aria-label="Loading chart"
      className={cx(
        className,
        "h-60 animate-pulse rounded-lg bg-zinc-950/5 motion-reduce:animate-none dark:bg-white/5",
      )}
    />
  );
}
type OkSummary = Extract<EventSummary, { status: "ok" }>;
type Failures = NonNullable<OkSummary["failures"]>;
// The window as words, for the charts' sentences: "the last 24 hours".
function windowPhrase(filters: EventFilters): string {
  if (filters.from || filters.to) return rangeLabel(filters.from, filters.to);
  const window = filters.window ?? "24h";
  if (window === "today") return "today (UTC)";
  if (window === "yesterday") return "yesterday (UTC)";
  return `the ${windowLabels[window].toLowerCase()}`;
}
// A column's label: the time up to 2 days, the day and time beyond.
function bucketLabel(start: string, bucketSeconds: number): string {
  const time = new Date(start);
  return bucketSeconds < 3 * 60 * 60
    ? formatClock(time)
    : `${formatDay(time)} ${formatClock(time)}`;
}
function sum(counts: number[]): number {
  return counts.reduce((total, n) => total + n, 0);
}
// A bucket's end, in RFC 3339.
function bucketEnd(start: string, bucketSeconds: number): string {
  return new Date(new Date(start).getTime() + bucketSeconds * 1000)
    .toISOString()
    .replace(".000Z", "Z");
}
function SummaryCharts({
  summary,
  filters,
  index,
  onApply,
}: {
  summary: OkSummary;
  filters: EventFilters;
  index: NameIndex | null;
  onApply: (filters: EventFilters) => void;
}) {
  const key = useAdminKey();
  const busiest = summary.buckets.reduce((best, bucket) =>
    sum(bucket.counts) > sum(best.counts) ? bucket : best,
  );
  const busiestAt = bucketLabel(busiest.start, summary.bucketSeconds);
  const busiestCount = sum(busiest.counts).toLocaleString("en-US");
  // Clicking a column narrows the window to its bucket.
  const zoom = (row: BucketRow) =>
    onApply({
      ...narrowingFilters(filters),
      from: row.start,
      to: bucketEnd(row.start, summary.bucketSeconds),
    });
  const phrase = windowPhrase(filters);
  return (
    <>
      <div className="grid gap-8 sm:grid-cols-2 lg:grid-cols-4">
        <Stat title="Events" value={summary.total.toLocaleString("en-US")}>
          In {phrase}
        </Stat>
        <FailedTile summary={summary} filters={filters} />
        <Stat
          title="Limits reached"
          value={summary.limitsReached.toLocaleString("en-US")}
        >
          rate_limit.reached
        </Stat>
        <Stat title="Busiest" value={busiestCount}>
          {busiestAt}–
          {formatClock(
            new Date(bucketEnd(busiest.start, summary.bucketSeconds)),
          )}{" "}
          UTC
        </Stat>
      </div>
      <div className="mt-12">
        <Subheading>Events over time</Subheading>
        <BarChart
          label="Events over time"
          summary={`${summary.total.toLocaleString("en-US")} events in ${phrase}, most at ${busiestAt} UTC (${busiestCount}).`}
          rows={bucketRows(
            summary.buckets,
            summary.series,
            summary.bucketSeconds,
          )}
          series={summary.series.map((s) => ({
            key: s,
            label: seriesLabel(s, summary.stackedBy, index),
          }))}
          onSelect={zoom}
          selectHint="Click to zoom in"
          className="mt-2"
        />
      </div>
      <div className="mt-12 grid gap-12 lg:grid-cols-2">
        {summary.failures && (
          <div>
            <Subheading>Failed checks over time</Subheading>
            <BarChart
              label="Failed checks over time"
              summary={failuresSentence(summary.failures)}
              rows={bucketRows(
                summary.failures.buckets,
                summary.failures.series,
                summary.bucketSeconds,
              )}
              series={summary.failures.series.map((kind) => ({
                key: kind,
                label: failureLabels[kind],
              }))}
              onSelect={zoom}
              selectHint="Click to zoom in"
              className="mt-2"
            />
          </div>
        )}
        <div>
          <Subheading>Top events</Subheading>
          <BarList
            label="Top events"
            className="mt-4"
            total={summary.total}
            items={summary.topNames.map(({ name, count }) => ({
              key: name,
              label: name,
              value: count,
              href: adminRoutes.events.index(key, {
                ...filters,
                names: [name],
                before: undefined,
              }),
            }))}
          />
        </div>
      </div>
    </>
  );
}
// Failed checks, and how many sign-ins failed.
function FailedTile({
  summary,
  filters,
}: {
  summary: OkSummary;
  filters: EventFilters;
}) {
  const failures = summary.failures;
  if (!failures)
    return (
      <Stat title="Failed checks" value="–">
        None of these events can fail
      </Stat>
    );
  const signIns = kindTotals(failures)[0];
  return (
    <Stat title="Failed checks" value={failures.total.toLocaleString("en-US")}>
      {summary.signInAttempts > 0 && filters.outcome !== "failed"
        ? `${Math.round((signIns / summary.signInAttempts) * 100)}% of sign-ins`
        : "Sign-ins, codes, passwords, admin door"}
    </Stat>
  );
}
// Failed checks per kind, in the series' order.
function kindTotals(failures: Failures): number[] {
  return failures.series.map((_, i) =>
    failures.buckets.reduce((total, b) => total + b.counts[i], 0),
  );
}
function failuresSentence(failures: Failures): string {
  if (failures.total === 0) return "No failed checks.";
  const totals = kindTotals(failures);
  const top = totals.indexOf(Math.max(...totals));
  return `${failures.total.toLocaleString("en-US")} failed checks, most of them ${failureLabels[failures.series[top]].toLowerCase()} (${totals[top].toLocaleString("en-US")}).`;
}
type BucketRow = ChartRow<string> & { start: string };
function bucketRows(
  buckets: OkSummary["buckets"],
  series: string[],
  bucketSeconds: number,
): BucketRow[] {
  return buckets.map((bucket) => ({
    label: bucketLabel(bucket.start, bucketSeconds),
    start: bucket.start,
    values: Object.fromEntries(series.map((s, i) => [s, bucket.counts[i]])),
  }));
}
// A series' name: a group's label from the catalogue, a name as it is,
// "Other" for the rest.
function seriesLabel(
  series: string,
  stackedBy: OkSummary["stackedBy"],
  index: NameIndex | null,
): string {
  if (series === "other") return "Other";
  if (stackedBy === "name") return series;
  return index?.groups.find((g) => g.id === series)?.label ?? series;
}
// The list, its states, and Older / Newer.
function EventList({
  list,
  waiting,
  filters,
  onApply,
}: {
  list: EventListResource;
  waiting: boolean;
  filters: EventFilters;
  onApply: (filters: EventFilters) => void;
}) {
  const key = useAdminKey();
  const time = { window: filters.window, from: filters.from, to: filters.to };
  if (!list.data)
    return list.loading || waiting ? (
      <div className="mt-12">
        <ListSkeleton label="Loading events" />
      </div>
    ) : null;
  if (list.data.data.length === 0 && !list.stale)
    return isFiltered(filters) ? (
      <EmptyState
        title="No events match"
        description="Try fewer filters or a wider range."
        action={<Button onClick={() => onApply(time)}>Clear filters</Button>}
      />
    ) : (
      <EmptyState
        title={`No events in ${windowPhrase(filters)}`}
        description="Sign-ins, changes and admin actions appear here as they happen."
        action={<WiderButton filters={filters} onApply={onApply} />}
      />
    );
  return (
    <div
      aria-busy={list.refreshing}
      className={cx("mt-12", list.refreshing && "opacity-60")}
    >
      <EventTable
        events={list.data.data}
        ipHref={(ip) =>
          adminRoutes.events.index(key, { ...filters, ip, before: undefined })
        }
      />
      <CursorNav
        label="Event pages"
        next={list.data.next}
        hrefFor={(before) =>
          adminRoutes.events.index(key, { ...filters, before })
        }
      />
    </div>
  );
}
// A wider window, when there is one.
function WiderButton({
  filters,
  onApply,
}: {
  filters: EventFilters;
  onApply: (filters: EventFilters) => void;
}) {
  if (filters.window === "all") return null;
  const wider: EventWindow = filters.window === "7d" ? "all" : "7d";
  return (
    <Button
      onClick={() => onApply({ ...narrowingFilters(filters), window: wider })}
    >
      {wider === "all" ? "Show all time" : "Show the last 7 days"}
    </Button>
  );
}
// The Filters dialog: events, outcome, account, workspace, email and IP,
// applied together (each Apply is one recorded read). Opened again, it
// starts from what's applied.
function FiltersDialog({
  open,
  onClose,
  filters,
  index,
  onApply,
}: {
  open: boolean;
  onClose: () => void;
  filters: EventFilters;
  index: NameIndex | null;
  onApply: (filters: EventFilters) => void;
}) {
  const opened = useOpenCount(open);
  return (
    <Dialog open={open} onClose={onClose} size="xl">
      <FiltersForm
        key={opened}
        filters={filters}
        index={index}
        onApply={onApply}
        onCancel={onClose}
      />
    </Dialog>
  );
}
type FormValues = {
  names: string[];
  outcome: "any" | "failed";
  account: string;
  workspace: string;
  email: string;
  ip: string;
};
const textFields = [
  { field: "account", label: "Account", placeholder: "usr_…" },
  { field: "workspace", label: "Workspace", placeholder: "wsp_…" },
  { field: "email", label: "Email", placeholder: "ada@example.com" },
  { field: "ip", label: "IP address or range", placeholder: "203.0.113.0/24" },
] as const;
type TextField = (typeof textFields)[number]["field"];
// What's wrong with a typed value, or null.
function problem(field: TextField, value: string): string | null {
  if (!value) return null;
  if (field === "account" && !parsePublicId("usr", value))
    return "An account ID starts with usr_.";
  if (field === "workspace" && !parsePublicId("wsp", value))
    return "A workspace ID starts with wsp_.";
  if (field === "email" && !filterPatterns.email.test(value))
    return "Enter an email address.";
  if (field === "ip" && !filterPatterns.ip.test(value))
    return "Enter an address, or a range such as 10.0.0.0/16.";
  return null;
}
const emptyValues: FormValues = {
  names: [],
  outcome: "any",
  account: "",
  workspace: "",
  email: "",
  ip: "",
};
function FiltersForm({
  filters,
  index,
  onApply,
  onCancel,
}: {
  filters: EventFilters;
  index: NameIndex | null;
  onApply: (filters: EventFilters) => void;
  onCancel: () => void;
}) {
  const [values, setValues] = useState<FormValues>(() => ({
    names: index ? expandTokens(filters.names ?? [], index) : [],
    outcome: filters.outcome ?? "any",
    account: filters.account ?? "",
    workspace: filters.workspace ?? "",
    email: filters.email ?? "",
    ip: filters.ip ?? "",
  }));
  const [checked, setChecked] = useState(false);
  const options = useMemo(() => (index ? eventOptions(index) : []), [index]);
  const set = <F extends keyof FormValues>(field: F, value: FormValues[F]) =>
    setValues((v) => ({ ...v, [field]: value }));
  const clean = (field: TextField) =>
    field === "email"
      ? values.email.trim().toLowerCase()
      : values[field].trim();
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setChecked(true);
    if (textFields.some(({ field }) => problem(field, clean(field)))) return;
    onApply({
      names:
        index && values.names.length > 0
          ? compressNames(values.names, index)
          : undefined,
      outcome: values.outcome === "failed" ? "failed" : undefined,
      account: clean("account") || undefined,
      workspace: clean("workspace") || undefined,
      email: clean("email") || undefined,
      ip: clean("ip") || undefined,
    });
  };
  return (
    <form onSubmit={submit} aria-label="Filter events">
      <DialogTitle>Filter events</DialogTitle>
      <DialogDescription>
        Filters apply together. The time range stays as picked above the list.
      </DialogDescription>
      <DialogBody className="space-y-6">
        <Field>
          <Label>Events</Label>
          <Description>
            Pick events or whole groups. Admin reads show only when picked.
          </Description>
          <Combobox
            value={values.names}
            onChange={(names) => set("names", names)}
            options={options}
            placeholder={
              index ? "All events except admin reads" : "Loading events…"
            }
            disabled={!index}
          />
        </Field>
        <Field>
          <Label>Outcome</Label>
          <Listbox
            value={values.outcome}
            onChange={(outcome) => set("outcome", outcome)}
          >
            <ListboxOption value="any">
              <ListboxLabel>Any outcome</ListboxLabel>
            </ListboxOption>
            <ListboxOption value="failed">
              <ListboxLabel>Failed checks only</ListboxLabel>
            </ListboxOption>
          </Listbox>
        </Field>
        <div className="grid gap-x-4 gap-y-6 sm:grid-cols-2">
          {textFields.map(({ field, label, placeholder }) => {
            const error = checked ? problem(field, clean(field)) : null;
            return (
              <Field key={field}>
                <Label>{label}</Label>
                <Input
                  name={field}
                  value={values[field]}
                  placeholder={placeholder}
                  invalid={error !== null}
                  onChange={(e) => set(field, e.currentTarget.value)}
                />
                {error && <ErrorMessage>{error}</ErrorMessage>}
              </Field>
            );
          })}
        </div>
      </DialogBody>
      <DialogActions>
        <Button
          plain
          onClick={() => {
            setValues(emptyValues);
            setChecked(false);
          }}
        >
          Clear all
        </Button>
        <Button plain onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit">Apply filters</Button>
      </DialogActions>
    </form>
  );
}
// The catalogue's groups as the picker's tree: each group (its ID, then
// its label) holding its sub-groups, then its names.
function eventOptions(index: NameIndex): ComboboxNode[] {
  const leaf = (name: string): ComboboxNode => ({
    value: name,
    text: name,
    label: name,
  });
  const group = (g: NameIndex["groups"][number]): ComboboxNode => ({
    value: g.id,
    text: `${g.id} ${g.label}`,
    chip: g.id,
    label: (
      <>
        <span className="font-medium">{g.id}</span>{" "}
        <span className="text-zinc-500 dark:text-zinc-400">{g.label}</span>
      </>
    ),
    children: [...g.groups.map(group), ...g.names.map(leaf)],
  });
  return index.groups.map(group);
}
// The custom range: whole UTC days, picked on a calendar, from a year ago
// to today.
function RangeDialog({
  open,
  onClose,
  filters,
  onApply,
}: {
  open: boolean;
  onClose: () => void;
  filters: EventFilters;
  onApply: (from: string, to: string) => void;
}) {
  const opened = useOpenCount(open);
  return (
    <Dialog open={open} onClose={onClose} size="2xl">
      <RangeForm
        key={opened}
        filters={filters}
        onApply={onApply}
        onCancel={onClose}
      />
    </Dialog>
  );
}
// The range already applied, when it's whole days, as calendar dates.
function startingRange(filters: EventFilters): CalendarValue {
  const { from, to } = filters;
  const midnight = (time?: string) => Boolean(time?.endsWith("T00:00:00Z"));
  if (!from || !to || !midnight(from) || !midnight(to)) return null;
  return [localDay(from), localDay(to, 1)];
}
// A calendar date as "Oct 20": its own year, month and day.
function calendarDay(date: Date): string {
  return date.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}
function RangeForm({
  filters,
  onApply,
  onCancel,
}: {
  filters: EventFilters;
  onApply: (from: string, to: string) => void;
  onCancel: () => void;
}) {
  const [value, setValue] = useState<CalendarValue>(() =>
    startingRange(filters),
  );
  // Today's UTC day, as the calendar's local date.
  const [today] = useState(() => localDay(new Date().toISOString()));
  const yearAgo = new Date(
    today.getFullYear() - 1,
    today.getMonth(),
    today.getDate(),
  );
  const [start, end] = Array.isArray(value) ? value : [value];
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!start) return;
    onApply(utcDay(start), utcDay(end ?? start, 1));
  };
  let picked: ReactNode = "Pick the first day, then the last.";
  if (start)
    picked =
      end && end.getTime() !== start.getTime()
        ? `${calendarDay(start)} – ${calendarDay(end)}, UTC`
        : `${calendarDay(start)}, UTC`;
  return (
    <form onSubmit={submit} aria-label="Custom range">
      <DialogTitle>Custom range</DialogTitle>
      <DialogDescription>
        Whole days in UTC, up to a year back. Charts cover 7 days or less.
      </DialogDescription>
      <DialogBody>
        <Calendar
          range
          value={value}
          onChange={setValue}
          minDate={yearAgo}
          maxDate={today}
          className="mx-auto"
        />
        <Text className="mt-4" aria-live="polite">
          {picked}
        </Text>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onCancel}>
          Cancel
        </Button>
        <Button
          type="submit"
          disabled={!start}
          disabledReason="Fill in required fields"
        >
          Apply range
        </Button>
      </DialogActions>
    </form>
  );
}
