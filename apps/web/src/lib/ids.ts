// Records are identified by public IDs: a type prefix and 14 base62
// characters, e.g. prj_j292s2S2j7Qx4B. PostgreSQL generates them and the API
// checks the same shape (validPublicID in httpapi/ids.go).
export type IdPrefix = "usr" | "wsp" | "prj" | "evt";
// The pattern for one prefix's IDs, for building route patterns.
export function publicIdPattern(prefix: IdPrefix): string {
  return `${prefix}_[0-9A-Za-z]{14}`;
}
// The ID in a URL segment or query value, or null unless it is a well-formed
// public ID with this prefix.
export function parsePublicId(
  prefix: IdPrefix,
  value: string | string[] | undefined,
): string | null {
  if (typeof value !== "string") return null;
  return new RegExp(`^${publicIdPattern(prefix)}$`).test(value) ? value : null;
}
