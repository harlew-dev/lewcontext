import { describe, expect, it } from "vitest";
import { parseId } from "./ids";
describe("parseId", () => {
  it("reads positive integer IDs up to the largest exact number", () => {
    expect(parseId("1")).toBe(1);
    expect(parseId("88")).toBe(88);
    expect(parseId("9007199254740991")).toBe(Number.MAX_SAFE_INTEGER);
  });
  it("rejects IDs JavaScript would round", () => {
    expect(parseId("9007199254740992")).toBeNull();
    expect(parseId("9007199254740993")).toBeNull();
    expect(parseId("123456789012345678")).toBeNull();
  });
  it("rejects anything but plain digits", () => {
    for (const value of ["0", "007", "-1", "+1", "1.0", "1e3", " 1", "abc", ""])
      expect(parseId(value)).toBeNull();
    expect(parseId(["1", "2"])).toBeNull();
    expect(parseId(undefined)).toBeNull();
  });
});
