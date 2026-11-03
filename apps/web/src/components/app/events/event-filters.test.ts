import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { eventTypes } from "@/types/events";
import { eventLabels } from "@/lib/event-log";
import {
  compressTypes,
  eventQuery,
  eventSearch,
  expandTypes,
  indexQuery,
} from "./event-filters";
const actor = "usr_00000000000001";
const read = (query: string) => eventQuery(new URLSearchParams(query));
describe("the Logs URL", () => {
  it("reads what the API takes, forgivingly", () => {
    expect(read("")).toMatchObject({
      types: [],
      window: "7d",
      q: "",
      actor: "",
      from: "",
      to: "",
    });
    const q = read(
      `type=projects,project.created,nope,projects&actor=${actor}&project=x&q=%20site%20&window=30d&before=evt_00000000000009`,
    );
    expect(q).toMatchObject({
      types: ["projects", "project.created"],
      actor,
      project: "",
      q: "site",
      window: "30d",
      before: "evt_00000000000009",
    });
    expect(read("window=1h").window).toBe("7d");
  });
  it("takes a custom range instead of a window, or neither if it isn't one", () => {
    const range = read(
      "window=24h&from=2026-10-01T00:00:00Z&to=2026-10-03T00:00:00Z",
    );
    expect(range).toMatchObject({
      from: "2026-10-01T00:00:00Z",
      to: "2026-10-03T00:00:00Z",
    });
    expect(indexQuery(range)).toMatchObject({
      window: undefined,
      from: "2026-10-01T00:00:00Z",
    });
    expect(read("from=yesterday").from).toBe("");
  });
  it("writes one URL per view and keeps other parameters", () => {
    const base = new URLSearchParams("tab=logs&type=members&q=old&junk=1");
    const next = eventSearch(base, {
      ...read("type=projects&window=30d"),
      before: "evt_00000000000009",
    });
    expect(next.toString()).toBe(
      "tab=logs&junk=1&type=projects&window=30d&before=evt_00000000000009",
    );
    // The default window isn't written.
    expect(
      eventSearch(new URLSearchParams(), read("window=7d")).toString(),
    ).toBe("");
  });
  it("compresses a fully picked group to its ID", () => {
    const projects = [...eventTypes[3].names];
    expect(compressTypes(projects)).toEqual(["projects"]);
    expect(compressTypes(["project.created", "workspace.renamed"])).toEqual([
      "workspace.renamed",
      "project.created",
    ]);
    expect(expandTypes(["projects", "workspace.renamed"]).sort()).toEqual(
      [...projects, "workspace.renamed"].sort(),
    );
  });
});
// The groups and names mirror the Go allow-list: a name the API doesn't know
// is a 400, and one it has that the page lacks can't be filtered.
describe("event types mirror the API's workspace log", () => {
  const go = readFileSync(
    resolve(process.cwd(), "../api/internal/events/workspace_log.go"),
    "utf8",
  );
  const listed = (block: string) =>
    [...block.matchAll(/^\t"([a-z_.]+)":\s+\{/gm)].map((m) => m[1]);
  const logBlock = go.slice(
    go.indexOf("var workspaceLog = "),
    go.indexOf("// workspaceLogLabels"),
  );
  it("lists the same events", () => {
    const names = eventTypes.flatMap((t) => [...t.names]).sort();
    expect(names).toEqual(listed(logBlock).sort());
  });
  it("labels every event", () => {
    for (const t of eventTypes)
      for (const name of t.names) expect(eventLabels[name]).toBeTruthy();
  });
  it("lists the same groups", () => {
    const block = go.slice(go.indexOf("var WorkspaceLogGroupLabels"));
    const ids = [...block.matchAll(/\{"([a-z-]+)", "([A-Za-z ]+)"\}/g)].map(
      (m) => [m[1], m[2]],
    );
    expect(ids).toEqual(eventTypes.map((t) => [t.id, t.label]));
  });
});
