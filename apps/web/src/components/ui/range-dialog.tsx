import { useState, type FormEvent, type ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { Calendar, type CalendarValue } from "@/components/ui/calendar";
import {
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Description, Field, Label } from "@/components/ui/fieldset";
import { Input } from "@/components/ui/input";
import { Text } from "@/components/ui/text";
import { useOpenCount } from "@/lib/use-open-count";
import { localDay, rangeLabel, utcAt, utcDay } from "@/lib/date-range";
// The custom range: whole UTC days, picked on a calendar, from a year ago
// to today.
export function RangeDialog({
  open,
  onClose,
  from,
  to,
  onApply,
  description = "Days and times in UTC, up to a year back.",
}: {
  open: boolean;
  onClose: () => void;
  // The range already applied, if any (RFC 3339).
  from?: string;
  to?: string;
  onApply: (from: string, to: string) => void;
  // What the dialog says under its title.
  description?: string;
}) {
  const opened = useOpenCount(open);
  return (
    <Dialog open={open} onClose={onClose} size="2xl">
      <RangeForm
        key={opened}
        from={from}
        to={to}
        description={description}
        onApply={onApply}
        onCancel={onClose}
      />
    </Dialog>
  );
}
// The range already applied, as the calendar's days and UTC times: an end
// at midnight is the end of the day before (its time left blank).
function startingRange(from?: string, to?: string): RangeValues {
  if (!from || !to) return { days: null, start: "00:00", end: "" };
  const endsAtMidnight = to.endsWith("T00:00:00Z");
  return {
    days: [localDay(from), localDay(to, endsAtMidnight ? 1 : 0)],
    start: utcClock(from),
    end: endsAtMidnight ? "" : utcClock(to),
  };
}
type RangeValues = { days: CalendarValue; start: string; end: string };
// An RFC 3339 time's hh:mm in UTC.
function utcClock(value: string): string {
  return new Date(value).toISOString().slice(11, 16);
}
// A calendar date as "Oct 20": its own year, month and day.
function calendarDay(date: Date): string {
  return date.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}
const clockPattern = /^([01]\d|2[0-3]):[0-5]\d$/;
// The range's bounds in RFC 3339: the first day at the start time, and the
// last day at the end time (blank: the end of that day). Null when it isn't
// a range: no day, a malformed time, or an end before the start.
function rangeBounds(values: RangeValues): { from: string; to: string } | null {
  const [first, last] = Array.isArray(values.days)
    ? values.days
    : [values.days];
  if (!first || !clockPattern.test(values.start)) return null;
  if (values.end && !clockPattern.test(values.end)) return null;
  const from = utcAt(first, values.start);
  const to = values.end
    ? utcAt(last ?? first, values.end)
    : utcDay(last ?? first, 1);
  return Date.parse(from) < Date.parse(to) ? { from, to } : null;
}
function RangeForm({
  from,
  to,
  description,
  onApply,
  onCancel,
}: {
  from?: string;
  to?: string;
  description: string;
  onApply: (from: string, to: string) => void;
  onCancel: () => void;
}) {
  const [values, setValues] = useState<RangeValues>(() =>
    startingRange(from, to),
  );
  // Today's UTC day, as the calendar's local date.
  const [today] = useState(() => localDay(new Date().toISOString()));
  const yearAgo = new Date(
    today.getFullYear() - 1,
    today.getMonth(),
    today.getDate(),
  );
  const [first, last] = Array.isArray(values.days)
    ? values.days
    : [values.days];
  const bounds = rangeBounds(values);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (bounds) onApply(bounds.from, bounds.to);
  };
  let picked: ReactNode = "Pick the first day, then the last.";
  if (bounds) picked = rangeLabel(bounds.from, bounds.to);
  else if (first)
    picked =
      last && last.getTime() !== first.getTime()
        ? `${calendarDay(first)} – ${calendarDay(last)}: the end comes before the start.`
        : `${calendarDay(first)}: the end comes before the start.`;
  return (
    <form onSubmit={submit} aria-label="Custom range">
      <DialogTitle>Custom range</DialogTitle>
      <DialogDescription>{description}</DialogDescription>
      <DialogBody>
        <Calendar
          range
          value={values.days}
          onChange={(days) => setValues((v) => ({ ...v, days }))}
          minDate={yearAgo}
          maxDate={today}
          className="mx-auto"
        />
        <div className="mt-6 grid gap-4 sm:grid-cols-2">
          <Field>
            <Label>Start time (UTC)</Label>
            <Input
              type="time"
              name="start"
              value={values.start}
              onChange={(e) => {
                const start = e.currentTarget.value;
                setValues((v) => ({ ...v, start }));
              }}
            />
            <Description>On the first day.</Description>
          </Field>
          <Field>
            <Label>End time (UTC)</Label>
            <Input
              type="time"
              name="end"
              value={values.end}
              onChange={(e) => {
                const end = e.currentTarget.value;
                setValues((v) => ({ ...v, end }));
              }}
            />
            <Description>On the last day; blank for its end.</Description>
          </Field>
        </div>
        <Text className="mt-4" aria-live="polite">
          {picked}
        </Text>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onCancel}>
          Cancel
        </Button>
        <Button
          type="submit"
          disabled={!bounds}
          disabledReason="Fill in required fields"
        >
          Apply range
        </Button>
      </DialogActions>
    </form>
  );
}
