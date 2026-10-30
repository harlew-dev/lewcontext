// Record IDs are positive integers (PostgreSQL bigint). JavaScript numbers are
// exact only up to Number.MAX_SAFE_INTEGER (2^53 - 1); beyond that a string
// like "9007199254740993" silently becomes a different ID.
// The ID in a URL segment or query value, or null unless it is plain decimal
// digits that convert to a number exactly.
export function parseId(value: string | string[] | undefined): number | null {
  if (typeof value !== "string" || !/^[1-9]\d{0,15}$/.test(value)) return null;
  const id = Number(value);
  return Number.isSafeInteger(id) ? id : null;
}
