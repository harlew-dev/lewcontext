import { describe, expect, it } from "vitest";
import { parsePublicId } from "./ids";
describe("parsePublicId", () => {
  it("reads IDs with the expected prefix", () => {
    expect(parsePublicId("prj", "prj_j292s2S2j7Qx4B")).toBe(
      "prj_j292s2S2j7Qx4B",
    );
    expect(parsePublicId("wsp", "wsp_00000000000000")).toBe(
      "wsp_00000000000000",
    );
  });
  it("rejects other prefixes, lengths and characters", () => {
    for (const value of [
      "wsp_j292s2S2j7Qx4B",
      "prj_j292s2S2j7Qx4",
      "prj_j292s2S2j7Qx4BB",
      "prj_j292s2S2j7Qx-B",
      "prj-j292s2S2j7Qx4B",
      " prj_j292s2S2j7Qx4B",
      "42",
      "",
    ])
      expect(parsePublicId("prj", value)).toBeNull();
    expect(parsePublicId("prj", ["prj_j292s2S2j7Qx4B"])).toBeNull();
    expect(parsePublicId("prj", undefined)).toBeNull();
  });
});
