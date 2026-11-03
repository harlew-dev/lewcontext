import { describe, expect, it } from "vitest";
import type { AlertRule } from "@/types/admin";
import {
  alertBadges,
  alertListQuery,
  apiAlertQuery,
  catalogQuery,
  filterRules,
} from "./alert-filters";
import { alertUrlQuery } from "@/lib/admin-routes";

const params = (query: string) => new URLSearchParams(query);

describe("alertListQuery", () => {
  it("reads every filter, and drops what it doesn't know", () => {
    expect(
      alertListQuery(
        params(
          "state=all&q=%20latency%20&level=critical&health=recovered&signals=http.latency_p95,nope,http.latency_p95&acknowledged=no&silenced=yes&window=7d",
        ),
      ),
    ).toEqual({
      state: "all",
      q: "latency",
      level: "critical",
      health: "recovered",
      signals: ["http.latency_p95"],
      acknowledged: "no",
      silenced: "yes",
      window: "7d",
      from: undefined,
      to: undefined,
      before: undefined,
    });
    expect(
      alertListQuery(params("state=x&level=info&health=resolved&before=nope")),
    ).toMatchObject({
      state: "open",
      level: undefined,
      health: undefined,
      before: undefined,
    });
  });
  it("lets a range beat a window, and keeps the URL short", () => {
    const query = alertListQuery(
      params("window=24h&from=2026-10-01T00:00:00Z&to=2026-10-02T00:00:00Z"),
    );
    expect(query.window).toBeUndefined();
    expect(alertUrlQuery(query)).toMatchObject({
      from: "2026-10-01T00:00:00Z",
      window: undefined,
      state: undefined,
    });
  });
  it("turns a window into a start, to the minute, for the API", () => {
    const now = Date.parse("2026-11-02T12:34:56Z");
    expect(apiAlertQuery({ window: "24h" }, now)).toEqual({
      window: undefined,
      from: "2026-11-01T12:34:00Z",
    });
    expect(apiAlertQuery({ level: "warn" }, now)).toEqual({ level: "warn" });
  });
  it("makes one removable badge per filter", () => {
    const badges = alertBadges(
      { state: "resolved", level: "critical", signals: ["a.b", "c.d", "e.f"] },
      (s) => s.toUpperCase(),
    );
    expect(badges.map((b) => b.text)).toEqual([
      "Resolved",
      "Critical",
      "Signals: A.B +2",
    ]);
    expect(badges[1].without).toEqual({
      state: "resolved",
      signals: ["a.b", "c.d", "e.f"],
    });
  });
});

const rule = (over: Partial<AlertRule>): AlertRule => ({
  signal: "http.latency_p95",
  title: "Latency",
  meaning: "Requests are slow.",
  page: "performance",
  unit: "ms",
  warn: 1,
  critical: 2,
  minSample: 0,
  windowSeconds: 3600,
  from: "warn",
  cap: "critical",
  levels: ["warn", "critical"],
  effectiveLevels: [],
  forSeconds: 0,
  cadenceSeconds: 60,
  recoverySeconds: 600,
  recoveryChecks: 11,
  preference: "off",
  events: [],
  ineffective: false,
  ...over,
});
describe("filterRules", () => {
  const rules = [
    rule({}),
    rule({
      signal: "auth.possible_takeovers",
      title: "Takeovers",
      meaning: "An account changed.",
      page: "security",
      levels: ["warn"],
      preference: "warn",
    }),
    rule({
      signal: "integrity.ownerless",
      title: "Ownerless",
      page: "integrity",
      cadenceSeconds: 3600,
      levels: ["critical"],
    }),
  ];
  const names = (q: string) =>
    filterRules(rules, catalogQuery(params(q))).map((r) => r.title);
  it("combines the search and each filter", () => {
    expect(names("")).toEqual(["Latency", "Takeovers", "Ownerless"]);
    expect(names("q=ACCOUNT")).toEqual(["Takeovers"]);
    expect(names("page=integrity")).toEqual(["Ownerless"]);
    expect(names("level=critical")).toEqual(["Latency", "Ownerless"]);
    expect(names("subscribed=yes")).toEqual(["Takeovers"]);
    expect(names("subscribed=no&cadence=minute")).toEqual(["Latency"]);
    expect(names("cadence=hourly")).toEqual(["Ownerless"]);
    expect(names("q=latency&page=security")).toEqual([]);
  });
});
