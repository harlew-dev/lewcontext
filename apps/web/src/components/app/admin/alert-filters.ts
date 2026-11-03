import {
  alertHealths,
  alertStates,
  alertWindows,
  type AlertListQuery,
  type AlertRule,
  type AlertWindow,
} from "@/types/admin";
import { rangeLabel } from "./event-filters";

// The Alerts page's filters, in the URL: parsed with anything unknown left
// out, so a stale or hand-typed link shows the list rather than an error.

const level = ["warn", "critical"] as const;
const yesNo = ["yes", "no"] as const;
const pick = <T extends string>(
  list: readonly T[],
  value: string | null,
): T | undefined => list.find((item) => item === value);
const rfc3339 = /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$/;
const signalName = /^[a-z][a-z0-9_]*(\.[a-z0-9_]+){1,2}$/;

export function alertListQuery(search: URLSearchParams): AlertListQuery {
  const from = search.get("from") ?? "";
  const to = search.get("to") ?? "";
  const range = rfc3339.test(from) || rfc3339.test(to);
  const signals = (search.get("signals") ?? "")
    .split(",")
    .filter((s) => signalName.test(s));
  const before = search.get("before") ?? "";
  return {
    state: pick(alertStates, search.get("state")) ?? "open",
    q: (search.get("q") ?? "").trim().slice(0, 100) || undefined,
    level: pick(level, search.get("level")),
    health: pick(alertHealths, search.get("health")),
    signals: signals.length > 0 ? [...new Set(signals)] : undefined,
    acknowledged: pick(yesNo, search.get("acknowledged")),
    silenced: pick(yesNo, search.get("silenced")),
    window: range ? undefined : pick(alertWindows, search.get("window")),
    from: rfc3339.test(from) ? from : undefined,
    to: rfc3339.test(to) ? to : undefined,
    before: /^alt_[0-9A-Za-z]{14}$/.test(before) ? before : undefined,
  };
}

export const windowLabels: Record<AlertWindow, string> = {
  "24h": "Last 24 hours",
  "7d": "Last 7 days",
  "30d": "Last 30 days",
};
const windowMs: Record<AlertWindow, number> = {
  "24h": 24 * 3600_000,
  "7d": 7 * 24 * 3600_000,
  "30d": 30 * 24 * 3600_000,
};
// The query the API takes: a window back from now becomes its start, to the
// minute, so the same page asks the same question.
export function apiAlertQuery(
  query: AlertListQuery,
  now: number,
): AlertListQuery {
  if (!query.window) return query;
  const minute = Math.floor((now - windowMs[query.window]) / 60_000) * 60_000;
  return {
    ...query,
    window: undefined,
    from: new Date(minute).toISOString().replace(/\.\d+Z$/, "Z"),
  };
}
// The time range as the listbox says it.
export function opened(query: AlertListQuery): string {
  if (query.from || query.to) return rangeLabel(query.from, query.to);
  return query.window ? windowLabels[query.window] : "Any time";
}

// One badge per applied filter, each with the query it leaves.
export type AlertBadge = { id: string; text: string; without: AlertListQuery };
export function alertBadges(
  query: AlertListQuery,
  titles: (signal: string) => string = (s) => s,
): AlertBadge[] {
  const badges: AlertBadge[] = [];
  const without = (...fields: (keyof AlertListQuery)[]) => {
    const next = { ...query };
    for (const field of fields) delete next[field];
    return next;
  };
  if (query.q)
    badges.push({ id: "q", text: `Search: ${query.q}`, without: without("q") });
  if (query.state && query.state !== "open")
    badges.push({
      id: "state",
      text: query.state === "all" ? "Open and resolved" : "Resolved",
      without: without("state"),
    });
  if (query.level)
    badges.push({
      id: "level",
      text: query.level === "warn" ? "Warning" : "Critical",
      without: without("level"),
    });
  if (query.health)
    badges.push({
      id: "health",
      text: `Recovery: ${query.health}`,
      without: without("health"),
    });
  if (query.signals?.length)
    badges.push({
      id: "signals",
      text:
        query.signals.length <= 2
          ? `Signals: ${query.signals.map(titles).join(", ")}`
          : `Signals: ${titles(query.signals[0])} +${query.signals.length - 1}`,
      without: without("signals"),
    });
  if (query.acknowledged)
    badges.push({
      id: "acknowledged",
      text: query.acknowledged === "yes" ? "Acknowledged" : "Not acknowledged",
      without: without("acknowledged"),
    });
  if (query.silenced)
    badges.push({
      id: "silenced",
      text: query.silenced === "yes" ? "Silenced" : "Not silenced",
      without: without("silenced"),
    });
  if (query.window || query.from || query.to)
    badges.push({
      id: "opened",
      text: `Opened: ${opened(query)}`,
      without: without("window", "from", "to"),
    });
  return badges;
}

// The catalogue's own filters, applied in the page to the rules it loaded.
export type CatalogQuery = {
  q?: string;
  page?: string;
  level?: "warn" | "critical";
  subscribed?: "yes" | "no";
  cadence?: "minute" | "hourly";
};
export function catalogQuery(search: URLSearchParams): CatalogQuery {
  return {
    q: (search.get("q") ?? "").trim().slice(0, 100) || undefined,
    page: /^[a-z]+$/.test(search.get("page") ?? "")
      ? (search.get("page") ?? undefined)
      : undefined,
    level: pick(level, search.get("level")),
    subscribed: pick(yesNo, search.get("subscribed")),
    cadence: pick(["minute", "hourly"] as const, search.get("cadence")),
  };
}
// The rules matching: the search in the title, signal or meaning (any case),
// the monitoring page, a level the rule can alert at, whether you're
// subscribed, and how often it's checked.
export function filterRules(
  rules: AlertRule[],
  query: CatalogQuery,
): AlertRule[] {
  const q = query.q?.toLowerCase();
  return rules.filter(
    (r) =>
      (!q ||
        r.title.toLowerCase().includes(q) ||
        r.signal.toLowerCase().includes(q) ||
        r.meaning.toLowerCase().includes(q)) &&
      (!query.page || r.page === query.page) &&
      (!query.level || r.levels.includes(query.level)) &&
      (!query.subscribed ||
        (query.subscribed === "yes") === (r.preference !== "off")) &&
      (!query.cadence ||
        (query.cadence === "hourly") === r.cadenceSeconds >= 3600),
  );
}
export function catalogBadges(
  query: CatalogQuery,
): { id: string; text: string; without: CatalogQuery }[] {
  const without = (field: keyof CatalogQuery) => {
    const next = { ...query };
    delete next[field];
    return next;
  };
  const out: { id: string; text: string; without: CatalogQuery }[] = [];
  if (query.q)
    out.push({ id: "q", text: `Search: ${query.q}`, without: without("q") });
  if (query.page)
    out.push({
      id: "page",
      text: `Page: ${query.page}`,
      without: without("page"),
    });
  if (query.level)
    out.push({
      id: "level",
      text: `Can alert at: ${query.level === "warn" ? "warning" : "critical"}`,
      without: without("level"),
    });
  if (query.subscribed)
    out.push({
      id: "subscribed",
      text: query.subscribed === "yes" ? "Subscribed" : "Not subscribed",
      without: without("subscribed"),
    });
  if (query.cadence)
    out.push({
      id: "cadence",
      text:
        query.cadence === "hourly" ? "Checked hourly" : "Checked each minute",
      without: without("cadence"),
    });
  return out;
}
