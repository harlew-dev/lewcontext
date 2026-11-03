// UTC days and ranges, shared by the time range pickers (the admin
// dashboard's Events and Alerts, a workspace's Logs): every time in them is
// UTC, so a browser's own zone never moves a day.

const dayMs = 24 * 60 * 60 * 1000;
// Whether a time is a UTC midnight.
export function isMidnight(time: Date): boolean {
  return time.getTime() % dayMs === 0;
}
const dayFormat = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  timeZone: "UTC",
});
const timeFormat = new Intl.DateTimeFormat("en-US", {
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
  timeZone: "UTC",
});
const secondsFormat = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
  timeZone: "UTC",
});
// "Oct 31", in UTC.
export function formatDay(time: Date): string {
  return dayFormat.format(time);
}
// "14:30", in UTC.
export function formatClock(time: Date): string {
  return timeFormat.format(time);
}
// "Nov 1, 14:29:58", in UTC: an event's time in the list.
export function formatEventTime(value: string): string {
  return secondsFormat.format(new Date(value));
}
// "Oct 31, 14:30".
export function formatMoment(time: Date): string {
  return `${formatDay(time)}, ${formatClock(time)}`;
}
// A custom range: whole UTC days as days (its end is exclusive), anything
// else to the minute.
export function rangeLabel(from?: string, to?: string): string {
  const start = from ? new Date(from) : null;
  const end = to ? new Date(to) : null;
  if (start && end && isMidnight(start) && isMidnight(end)) {
    const last = new Date(end.getTime() - dayMs);
    return last.getTime() <= start.getTime()
      ? `${formatDay(start)} UTC`
      : `${formatDay(start)} – ${formatDay(last)} UTC`;
  }
  if (start && end) {
    const sameDay = formatDay(start) === formatDay(end);
    return `${formatMoment(start)} – ${sameDay ? formatClock(end) : formatMoment(end)} UTC`;
  }
  if (start) return `Since ${formatMoment(start)} UTC`;
  if (end) return `Before ${formatMoment(end)} UTC`;
  return "All time";
}
// A calendar's local date (its year, month and day) as that UTC day's
// midnight, in RFC 3339: never toISOString() on the local date itself,
// which moves the day for anyone east of UTC.
export function utcDay(date: Date, plusDays = 0): string {
  const time = Date.UTC(
    date.getFullYear(),
    date.getMonth(),
    date.getDate() + plusDays,
  );
  return new Date(time).toISOString().replace(".000Z", "Z");
}
// A calendar's local date at a UTC time of day ("14:30"), in RFC 3339.
export function utcAt(date: Date, clock: string): string {
  const [hours, minutes] = clock.split(":").map(Number);
  const time = Date.UTC(
    date.getFullYear(),
    date.getMonth(),
    date.getDate(),
    hours,
    minutes,
  );
  return new Date(time).toISOString().replace(".000Z", "Z");
}
// A UTC day as the calendar's local date at local midnight.
export function localDay(value: string, minusDays = 0): Date {
  const time = new Date(new Date(value).getTime() - minusDays * dayMs);
  return new Date(time.getUTCFullYear(), time.getUTCMonth(), time.getUTCDate());
}
