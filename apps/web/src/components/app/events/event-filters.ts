import type { EventIndexQuery } from "@/lib/api";
import { rangeLabel } from "@/lib/date-range";
import { parsePublicId } from "@/lib/ids";
import { cleanSearch, param } from "@/lib/pagination";
import {
  defaultEventWindow,
  eventTypes,
  eventWindows,
  type EventQuery,
} from "@/types/events";
// The Logs state as its URL writes it, shared by every list of a workspace's
// changes (the Logs tab, a member's Logs, an API key's Logs). The URL is read
// forgivingly: what the API wouldn't take is left out, so a hand-edited link
// still shows a list. Other parameters (a page's ?tab=) are left alone.

const timePattern = /^\d{4}-\d\d-\d\dT[\d:.]+(Z|[+-]\d\d:\d\d)$/;
const known = new Set<string>([
  ...eventTypes.map((t) => t.id),
  ...eventTypes.flatMap((t) => t.names),
]);
// The parameters this state owns.
const owned = [
  "type",
  "actor",
  "project",
  "q",
  "window",
  "from",
  "to",
  "before",
];
export function eventQuery(search: URLSearchParams): EventQuery {
  const window = param(search, "window");
  const from = param(search, "from");
  const to = param(search, "to");
  const custom =
    (timePattern.test(from) || timePattern.test(to)) &&
    !Number.isNaN(Date.parse(from || to));
  const types = param(search, "type")
    .split(",")
    .filter((token, i, all) => known.has(token) && all.indexOf(token) === i);
  return {
    types,
    actor: parsePublicId("usr", param(search, "actor")) ?? "",
    project: parsePublicId("prj", param(search, "project")) ?? "",
    q: cleanSearch(param(search, "q")),
    window: eventWindows.find((w) => w.id === window)?.id ?? defaultEventWindow,
    from: custom && timePattern.test(from) ? from : "",
    to: custom && timePattern.test(to) ? to : "",
    before: parsePublicId("evt", param(search, "before")) ?? "",
  };
}
// Whether a custom range is applied rather than a window.
export function isCustomRange(query: EventQuery): boolean {
  return Boolean(query.from || query.to);
}
// The search parameters for a state, with `current`'s other parameters kept.
// Defaults are left out, so each view has one URL.
export function eventSearch(
  current: URLSearchParams,
  query: EventQuery,
): URLSearchParams {
  const next = new URLSearchParams(current);
  owned.forEach((key) => next.delete(key));
  const set = (key: string, value: string) => value && next.set(key, value);
  set("type", query.types.join(","));
  set("actor", query.actor);
  set("project", query.project);
  set("q", query.q);
  if (isCustomRange(query)) {
    set("from", query.from);
    set("to", query.to);
  } else if (query.window !== defaultEventWindow) set("window", query.window);
  set("before", query.before);
  return next;
}
// The API's parameters for a state.
export function indexQuery(query: EventQuery): EventIndexQuery {
  const custom = isCustomRange(query);
  return {
    name: query.types.join(",") || undefined,
    actor: query.actor || undefined,
    project: query.project || undefined,
    q: query.q || undefined,
    window:
      custom || query.window === defaultEventWindow ? undefined : query.window,
    from: query.from || undefined,
    to: query.to || undefined,
    before: query.before || undefined,
  };
}
// What the time range says: a preset's name, or the custom range.
export function timeLabel(query: EventQuery): string | null {
  return isCustomRange(query) ? rangeLabel(query.from, query.to) : null;
}
// Picked event names as the URL writes them: a group's ID when all its
// events are picked, then the names left over.
export function compressTypes(picked: string[]): string[] {
  const set = new Set(picked);
  const tokens: string[] = [];
  for (const group of eventTypes)
    if (group.names.every((n) => set.has(n))) tokens.push(group.id);
  for (const group of eventTypes)
    if (!group.names.every((n) => set.has(n)))
      tokens.push(...group.names.filter((n) => set.has(n)));
  return tokens;
}
// The events a list of tokens stands for: group IDs expanded.
export function expandTypes(tokens: string[]): string[] {
  const names = new Set<string>();
  for (const token of tokens) {
    const group = eventTypes.find((t) => t.id === token);
    if (group) group.names.forEach((n) => names.add(n));
    else names.add(token);
  }
  return [...names];
}
