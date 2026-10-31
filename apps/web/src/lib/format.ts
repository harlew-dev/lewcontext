// Dates are shown in UTC and en-US so server and browser render the same
// text, whoever reads them.
export function formatDate(
  value: string,
  style: "short" | "long" = "short",
): string {
  return new Date(value).toLocaleDateString(
    "en-US",
    style === "short"
      ? { month: "short", day: "numeric", timeZone: "UTC" }
      : { dateStyle: "long", timeZone: "UTC" },
  );
}
// A date and time, e.g. "Oct 30, 2026, 6:43 PM UTC".
export function formatDateTime(value: string): string {
  const text = new Date(value).toLocaleString("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "UTC",
  });
  return `${text} UTC`;
}
// Up to two initials for an avatar, e.g. "Ada Lovelace" → "AL".
export function initials(name: string): string {
  return name
    .split(" ")
    .map((part) => part[0])
    .slice(0, 2)
    .join("");
}
