import { describe, expect, it } from "vitest";
import type { AdminCatalogue } from "@/types/admin";
import {
  canonicalTokens,
  compressNames,
  eventFilters,
  expandTokens,
  localDay,
  nameIndex,
  rangeLabel,
  selectionLabel,
  utcDay,
  windowCaption,
} from "./event-filters";
const index = nameIndex({
  events: [
    "auth.login_attempted",
    "auth.mfa.enabled",
    "auth.mfa.checked",
    "workspace.created",
    "admin.user.viewed",
  ],
  eventGroups: [
    {
      id: "auth.*",
      label: "Accounts and sign-in",
      names: ["auth.login_attempted"],
      groups: [
        {
          id: "auth.mfa.*",
          label: "Two-factor authentication",
          names: ["auth.mfa.enabled", "auth.mfa.checked"],
          groups: [],
        },
      ],
    },
    {
      id: "workspace.*",
      label: "Workspaces",
      names: ["workspace.created"],
      groups: [],
    },
    {
      id: "admin-reads",
      label: "Admin reads",
      names: ["admin.user.viewed"],
      groups: [],
    },
  ],
  limits: [],
  listeners: [],
} satisfies AdminCatalogue);
describe("event filters", () => {
  it("reads the URL forgivingly", () => {
    const filters = eventFilters(
      new URLSearchParams(
        "name=auth.*,auth.*,Nope,admin-reads,auth.mfa.checked&window=all&from=2026-10-01T00:00:00Z&outcome=ok&account=usr_x&ip=10.0.0.0/16",
      ),
    );
    expect(filters).toEqual({
      names: ["auth.*", "admin-reads", "auth.mfa.checked"],
      window: "all",
      ip: "10.0.0.0/16",
    });
    // A range that ends before it starts is left out.
    expect(
      eventFilters(
        new URLSearchParams(
          "from=2026-10-02T00:00:00Z&to=2026-10-01T00:00:00Z",
        ),
      ),
    ).toEqual({});
  });
  it("expands and compresses names, groups first, then catalogue order", () => {
    expect(
      expandTokens(["auth.mfa.*", "nope.*", "auth.login_attempted"], index),
    ).toEqual(["auth.login_attempted", "auth.mfa.enabled", "auth.mfa.checked"]);
    expect(
      compressNames(
        ["workspace.created", "auth.mfa.checked", "auth.mfa.enabled"],
        index,
      ),
    ).toEqual(["auth.mfa.*", "workspace.*"]);
    expect(
      canonicalTokens(
        [
          "workspace.created",
          "auth.mfa.*",
          "auth.login_attempted",
          "admin.user.viewed",
        ],
        index,
      ),
    ).toEqual(["auth.*", "workspace.*", "admin-reads"]);
    expect(
      selectionLabel([
        "auth.*",
        "workspace.invitation.created",
        "project.created",
      ]),
    ).toBe("auth.*, workspace.invitation.created +1");
  });
  it("labels ranges in UTC, whole days as days", () => {
    expect(rangeLabel("2026-10-20T00:00:00Z", "2026-10-24T00:00:00Z")).toBe(
      "Oct 20 – Oct 23 UTC",
    );
    expect(rangeLabel("2026-10-20T00:00:00Z", "2026-10-21T00:00:00Z")).toBe(
      "Oct 20 UTC",
    );
    expect(rangeLabel("2026-11-01T14:00:00Z", "2026-11-01T14:30:00Z")).toBe(
      "Nov 1, 14:00 – 14:30 UTC",
    );
    expect(
      windowCaption({
        from: "2026-10-31T14:30:00Z",
        to: "2026-11-01T14:47:12Z",
      }),
    ).toBe("Oct 31, 14:30 – Nov 1, 14:47 UTC");
    expect(windowCaption(null)).toBe("All time, newest first");
  });
  it("turns a calendar's local dates into UTC days, east and west of UTC", () => {
    const tz = process.env.TZ;
    try {
      for (const zone of ["Pacific/Kiritimati", "America/Los_Angeles", "UTC"]) {
        process.env.TZ = zone;
        const day = new Date(2026, 9, 20);
        expect(utcDay(day)).toBe("2026-10-20T00:00:00Z");
        expect(utcDay(day, 1)).toBe("2026-10-21T00:00:00Z");
        expect(localDay("2026-10-24T00:00:00Z", 1).getDate()).toBe(23);
        // A month's end.
        expect(utcDay(new Date(2026, 9, 31), 1)).toBe("2026-11-01T00:00:00Z");
      }
    } finally {
      process.env.TZ = tz;
    }
  });
});
