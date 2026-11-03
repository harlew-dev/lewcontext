import {
  eventWindows,
  type AdminCatalogue,
  type EventFilters,
  type EventGroup,
  type EventWindow,
} from "@/types/admin";
import {
  formatClock,
  formatDay,
  formatMoment,
  isMidnight,
  rangeLabel,
} from "@/lib/date-range";
// The date helpers moved to lib/date-range.ts; these are still exported from here.
export {
  formatClock,
  formatDay,
  formatEventTime,
  localDay,
  rangeLabel,
  utcAt,
  utcDay,
} from "@/lib/date-range";
// The Events page's filters as the dashboard reads and writes them, shared
// by Events, an event's page and the pages linking in (a user's and a
// workspace's Activity, Mail, Email blocks). The URL is read forgivingly:
// anything the API wouldn't take is left out, so a hand-edited link still
// shows a list. Event names are written canonically: a fully picked group
// as its ID, then the names left over, in catalogue order.

// Name tokens: a catalogue name (two or three segments), a group ID
// ("auth.*", "auth.mfa.*") or "admin-reads".
const tokenPattern =
  /^(admin-reads|[a-z][a-z_]*(\.[a-z][a-z_]*){1,2}|[a-z][a-z_]*(\.[a-z][a-z_]*)?\.\*)$/;
// The API's bounds on name tokens (docs/api/admin.md#events).
const maxTokens = 64;
const maxNameParam = 4096;
const timePattern = /^\d{4}-\d\d-\d\dT[\d:.]+(Z|[+-]\d\d:\d\d)$/;
export const filterPatterns = {
  account: /^usr_[0-9A-Za-z]{14}$/,
  workspace: /^wsp_[0-9A-Za-z]{14}$/,
  email: /^[^\s@]+@[^\s@]+$/,
  ip: /^[0-9A-Fa-f:.]+(\/\d{1,3})?$/,
  before: /^evt_[0-9A-Za-z]{14}$/,
} as const;
// The filters in a URL's query, read forgivingly.
export function eventFilters(search: URLSearchParams): EventFilters {
  const filters: EventFilters = {};
  for (const [field, pattern] of Object.entries(filterPatterns)) {
    const value = search.get(field);
    if (value && value.length <= 254 && pattern.test(value))
      filters[field as keyof typeof filterPatterns] = value;
  }
  const name = search.get("name");
  if (name && name.length <= maxNameParam) {
    const tokens = [...new Set(name.split(","))].filter((t) =>
      tokenPattern.test(t),
    );
    if (tokens.length > 0) filters.names = tokens.slice(0, maxTokens);
  }
  if (search.get("outcome") === "failed") filters.outcome = "failed";
  const window = eventWindows.find((w) => w === search.get("window"));
  const from = validTime(search.get("from"));
  const to = validTime(search.get("to"));
  // A window or a range, never both: the window wins.
  if (window) filters.window = window;
  else if (from || to) {
    if (!(from && to && Date.parse(from) >= Date.parse(to))) {
      if (from) filters.from = from;
      if (to) filters.to = to;
    }
  }
  return filters;
}
function validTime(value: string | null): string | undefined {
  if (!value || !timePattern.test(value) || Number.isNaN(Date.parse(value)))
    return undefined;
  return value;
}
// The catalogue's names and groups, ready for expanding and compressing.
export type NameIndex = {
  order: string[];
  groups: EventGroup[];
  names: Map<string, string[]>;
};
export function nameIndex(catalogue: AdminCatalogue): NameIndex {
  const names = new Map<string, string[]>();
  const walk = (groups: EventGroup[]) => {
    for (const group of groups) {
      names.set(group.id, groupNames(group));
      walk(group.groups);
    }
  };
  walk(catalogue.eventGroups);
  return { order: catalogue.events, groups: catalogue.eventGroups, names };
}
// Every name in a group and its sub-groups.
export function groupNames(group: EventGroup): string[] {
  return [...group.groups.flatMap(groupNames), ...group.names];
}
// Tokens to the names they stand for, in catalogue order; unknown tokens
// are dropped.
export function expandTokens(tokens: string[], index: NameIndex): string[] {
  const picked = new Set<string>();
  for (const token of tokens) {
    if (index.order.includes(token)) picked.add(token);
    else index.names.get(token)?.forEach((name) => picked.add(name));
  }
  return index.order.filter((name) => picked.has(name));
}
// Names to canonical tokens: each fully picked group as its ID (the
// highest one), then the names left over, in catalogue order.
export function compressNames(names: string[], index: NameIndex): string[] {
  const picked = new Set(names);
  const groups: string[] = [];
  const covered = new Set<string>();
  const walk = (list: EventGroup[]) => {
    for (const group of list) {
      const all = groupNames(group);
      if (all.length > 0 && all.every((name) => picked.has(name))) {
        groups.push(group.id);
        all.forEach((name) => covered.add(name));
      } else walk(group.groups);
    }
  };
  walk(index.groups);
  const rest = index.order.filter(
    (name) => picked.has(name) && !covered.has(name),
  );
  return [...groups, ...rest];
}
// Tokens as the page writes them: expanded, then compressed.
export function canonicalTokens(tokens: string[], index: NameIndex): string[] {
  return compressNames(expandTokens(tokens, index), index);
}
// A short text for a list of tokens: the first two, then "+N".
export function selectionLabel(tokens: string[], shown = 2): string {
  const head = tokens.slice(0, shown).join(", ");
  return tokens.length > shown ? `${head} +${tokens.length - shown}` : head;
}
export const windowLabels: Record<EventWindow, string> = {
  "1h": "Last hour",
  "24h": "Last 24 hours",
  today: "Today (UTC)",
  yesterday: "Yesterday (UTC)",
  "7d": "Last 7 days",
  all: "All time",
};
// What a set of filters' time says: a window's name, or a custom range
// ("Oct 20 – Oct 23 UTC", "Nov 1, 14:00 – 14:30 UTC").
export function windowLabel(filters: EventFilters): string {
  if (filters.from || filters.to) return rangeLabel(filters.from, filters.to);
  return windowLabels[filters.window ?? "24h"];
}
// The caption under the heading: the window the API resolved.
export function windowCaption(
  window: { from: string | null; to: string } | null,
): string {
  if (!window) return "All time, newest first";
  // Whole days read as days ("Oct 20 – Oct 23 UTC").
  if (
    window.from &&
    isMidnight(new Date(window.from)) &&
    isMidnight(new Date(window.to))
  )
    return rangeLabel(window.from, window.to);
  const to = new Date(window.to);
  if (!window.from) return `Up to ${formatMoment(to)} UTC`;
  const from = new Date(window.from);
  const sameDay = formatDay(from) === formatDay(to);
  return `${formatMoment(from)} – ${sameDay ? formatClock(to) : formatMoment(to)} UTC`;
}
// The filters that narrow the list, apart from time and the cursor.
export function narrowingFilters(filters: EventFilters): EventFilters {
  const { names, outcome, account, workspace, email, ip } = filters;
  return { names, outcome, account, workspace, email, ip };
}
// Whether any filter apart from time is applied.
export function isFiltered(filters: EventFilters): boolean {
  return Object.values(narrowingFilters(filters)).some((v) =>
    Array.isArray(v) ? v.length > 0 : v !== undefined,
  );
}
