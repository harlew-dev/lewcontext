import {
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type KeyboardEvent,
} from "react";
import { ChevronLeftIcon, ChevronRightIcon } from "@heroicons/react/20/solid";
import { cx } from "@/lib/cx";
// A month grid for picking a day or, with `range`, a range of days, built on
// `Date` and `Intl` (Base UI has no calendar). Keyboard: arrows move by a day
// or a week, Home and End to the week's ends, Page Up and Down by a month,
// Enter or Space picks. One month on phones, two from `sm:`. The month's
// name is plain text: the arrows change it.
//
// It hands back local Dates at local midnight. Read their year, month and day
// (getFullYear, getMonth, getDate), never toISOString(), which shifts the day
// for anyone east of UTC (docs/ui/base-ui.md).
export type CalendarValue = Date | null | (Date | null | undefined)[];
export function Calendar({
  value,
  onChange,
  range = false,
  minDate,
  maxDate,
  className,
}: {
  value: CalendarValue;
  onChange: (value: CalendarValue) => void;
  range?: boolean;
  minDate?: Date;
  maxDate?: Date;
  className?: string;
}) {
  const wide = useWide();
  const [from, to] = picked(value);
  const min = minDate && dayStart(minDate);
  const max = maxDate && dayStart(maxDate);
  // The first month shown: the pick's, or with nothing picked, today's (kept
  // within the bounds); two months end at the latest pickable one rather
  // than starting there.
  const [view, setView] = useState(() => {
    if (from) return monthStart(from);
    if (wide && max) return monthStart(addMonths(max, -1));
    return monthStart(clamp(dayStart(new Date()), min, max));
  });
  const [focus, setFocus] = useState<Date>(
    () => from ?? clamp(dayStart(new Date()), min, max),
  );
  const [hover, setHover] = useState<Date | null>(null);
  const grid = useRef<HTMLDivElement>(null);
  const moved = useRef(false);
  const shown = wide ? 2 : 1;
  const months = Array.from({ length: shown }, (_, i) => addMonths(view, i));
  const last = months[months.length - 1];
  const disabled = (day: Date) =>
    (min !== undefined && day < min) || (max !== undefined && day > max);
  const visible = (day: Date) => months.some((month) => sameMonth(month, day));
  // The one day in the tab order: the focused one while it's on screen.
  const tabbable = visible(focus) ? focus : firstEnabled(view, disabled);
  // After a key moves focus, the day's cell takes it (once it's rendered).
  useEffect(() => {
    if (!moved.current) return;
    moved.current = false;
    grid.current
      ?.querySelector<HTMLElement>(`[data-date="${key(focus)}"]`)
      ?.focus();
  });
  function pick(day: Date) {
    if (disabled(day)) return;
    setFocus(day);
    if (!range) return onChange(day);
    if (from && !to) {
      const [lo, hi] = from <= day ? [from, day] : [day, from];
      return onChange([lo, hi]);
    }
    onChange([day]);
  }
  function go(day: Date) {
    const next = clamp(day, min, max);
    moved.current = true;
    setFocus(next);
    if (!visible(next))
      setView(
        next < view
          ? monthStart(next)
          : monthStart(addMonths(next, -(shown - 1))),
      );
  }
  function onKeyDown(event: KeyboardEvent) {
    const steps: Record<string, () => Date> = {
      ArrowLeft: () => addDays(focus, -1),
      ArrowRight: () => addDays(focus, 1),
      ArrowUp: () => addDays(focus, -7),
      ArrowDown: () => addDays(focus, 7),
      Home: () => addDays(focus, -focus.getDay()),
      End: () => addDays(focus, 6 - focus.getDay()),
      PageUp: () => addMonths(focus, -1),
      PageDown: () => addMonths(focus, 1),
    };
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      return pick(focus);
    }
    const step = steps[event.key];
    if (!step) return;
    event.preventDefault();
    go(step());
  }
  const canGoBack = min === undefined || monthStart(min) < view;
  const canGoForward = max === undefined || monthStart(max) > last;
  // Where the range ends, while choosing its end: under the pointer.
  const end = to ?? (from && hover ? hover : null);
  const band = from && end ? sorted(from, end) : null;
  return (
    <div
      role="group"
      aria-roledescription="calendar"
      className={cx("w-fit max-w-full select-none", className)}
    >
      <div
        ref={grid}
        onKeyDown={onKeyDown}
        onPointerLeave={() => setHover(null)}
        className="flex flex-col gap-6 outline-hidden sm:flex-row sm:gap-8"
      >
        {months.map((month, index) => (
          <Month
            key={key(month)}
            month={month}
            // Previous and next month: hidden (but keeping their place) where
            // the other month of two shows them.
            previous={index === 0 ? canGoBack : null}
            next={index === months.length - 1 ? canGoForward : null}
            onPrevious={() => setView(addMonths(view, -1))}
            onNext={() => setView(addMonths(view, 1))}
          >
            {weeks(month).map((week, row) => (
              <div role="row" key={row} className="flex">
                {week.map((day, column) =>
                  day === null ? (
                    <div key={column} className="size-10 sm:size-9" />
                  ) : (
                    <Day
                      key={column}
                      day={day}
                      disabled={disabled(day)}
                      tabbable={sameDay(day, tabbable)}
                      start={from !== null && sameDay(day, from)}
                      finish={to !== null && sameDay(day, to)}
                      inBand={band !== null && day >= band[0] && day <= band[1]}
                      isEdge={
                        (from !== null && sameDay(day, from)) ||
                        (to !== null && sameDay(day, to))
                      }
                      onPick={() => pick(day)}
                      onHover={() => setHover(day)}
                    />
                  ),
                )}
              </div>
            ))}
          </Month>
        ))}
      </div>
    </div>
  );
}
// Whether the screen is at least `sm:` wide (40rem), following resizes.
const wideQuery = "(min-width: 40rem)";
function useWide(): boolean {
  return useSyncExternalStore(
    (onChange) => {
      const query = window.matchMedia?.(wideQuery);
      query?.addEventListener("change", onChange);
      return () => query?.removeEventListener("change", onChange);
    },
    () => window.matchMedia?.(wideQuery).matches ?? true,
    () => true,
  );
}
const monthName = new Intl.DateTimeFormat("en-US", { month: "long" });
// Su Mo Tu…: January 7th 2024 was a Sunday.
const weekdays = Array.from({ length: 7 }, (_, i) =>
  new Intl.DateTimeFormat("en-US", { weekday: "short" })
    .format(new Date(2024, 0, 7 + i))
    .slice(0, 2),
);
const arrowClasses = [
  "grid size-9 place-items-center rounded-lg text-zinc-500 dark:text-zinc-400",
  "hover:bg-zinc-950/5 hover:text-zinc-950 dark:hover:bg-white/10 dark:hover:text-white",
  "focus-visible:outline-2 focus-visible:outline-blue-500",
  "disabled:opacity-30 disabled:hover:bg-transparent",
].join(" ");
// One month: its header (title, with the arrows where it has them), the
// weekday names and the weeks.
function Month({
  month,
  previous,
  next,
  onPrevious,
  onNext,
  children,
}: {
  month: Date;
  // Null: not this month's arrow. Otherwise whether it can be used.
  previous: boolean | null;
  next: boolean | null;
  onPrevious: () => void;
  onNext: () => void;
  children: React.ReactNode;
}) {
  return (
    <div
      role="grid"
      aria-label={`${monthName.format(month)} ${month.getFullYear()}`}
    >
      <div className="flex h-9 items-center justify-between gap-2">
        <button
          type="button"
          aria-label="Previous month"
          onClick={onPrevious}
          disabled={previous === false}
          tabIndex={previous === null ? -1 : undefined}
          className={cx(arrowClasses, previous === null && "invisible")}
        >
          <ChevronLeftIcon className="size-5" aria-hidden="true" />
        </button>
        <span className="flex-1 text-center text-sm/6 font-semibold text-zinc-950 dark:text-white">
          <span>{monthName.format(month)}</span>{" "}
          <span>{month.getFullYear()}</span>
        </span>
        <button
          type="button"
          aria-label="Next month"
          onClick={onNext}
          disabled={next === false}
          tabIndex={next === null ? -1 : undefined}
          className={cx(arrowClasses, next === null && "invisible")}
        >
          <ChevronRightIcon className="size-5" aria-hidden="true" />
        </button>
      </div>
      <div role="row" className="mt-2 flex">
        {weekdays.map((name, i) => (
          <div
            role="columnheader"
            key={i}
            className="grid size-10 place-items-center text-xs/6 font-medium text-zinc-500 sm:size-9 dark:text-zinc-400"
          >
            {name}
          </div>
        ))}
      </div>
      <div>{children}</div>
    </div>
  );
}
// A day: blue at the ends of the range (or when picked), a pale band between
// them and while choosing the end, a ring when the keyboard is on it, faded
// when it can't be picked.
function Day({
  day,
  disabled,
  tabbable,
  start,
  finish,
  inBand,
  isEdge,
  onPick,
  onHover,
}: {
  day: Date;
  disabled: boolean;
  tabbable: boolean;
  start: boolean;
  finish: boolean;
  inBand: boolean;
  isEdge: boolean;
  onPick: () => void;
  onHover: () => void;
}) {
  return (
    <div
      role="gridcell"
      data-date={key(day)}
      aria-label={label(day)}
      aria-selected={isEdge}
      aria-disabled={disabled || undefined}
      tabIndex={tabbable ? 0 : -1}
      data-selected={isEdge ? "" : undefined}
      data-range={inBand ? "" : undefined}
      data-start={start ? "" : undefined}
      data-end={finish ? "" : undefined}
      data-disabled={disabled ? "" : undefined}
      onClick={onPick}
      onPointerEnter={onHover}
      className={cx(
        "relative grid size-10 place-items-center text-sm/6 text-zinc-950 outline-hidden sm:size-9 dark:text-white",
        "not-data-disabled:cursor-pointer",
        "hover:not-data-selected:bg-zinc-950/5 dark:hover:not-data-selected:bg-white/10",
        "data-range:bg-blue-500/15 dark:data-range:bg-blue-500/25",
        "data-selected:rounded-lg data-selected:bg-blue-500 data-selected:font-semibold data-selected:text-white",
        "data-start:rounded-r-none data-end:rounded-l-none",
        "rounded-lg data-range:rounded-none",
        "focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-blue-500",
        "data-disabled:cursor-default data-disabled:text-zinc-950/25 dark:data-disabled:text-white/25",
        "forced-colors:data-selected:bg-[Highlight] forced-colors:data-selected:text-[HighlightText]",
      )}
    >
      {day.getDate()}
    </div>
  );
}
// The picked days of a value: both ends of a range, or just the first.
function picked(value: CalendarValue): [Date | null, Date | null] {
  const [first, second] = Array.isArray(value) ? value : [value];
  return [first ? dayStart(first) : null, second ? dayStart(second) : null];
}
function sorted(a: Date, b: Date): [Date, Date] {
  return a <= b ? [a, b] : [b, a];
}
function dayStart(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate());
}
function monthStart(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), 1);
}
function addDays(date: Date, days: number): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate() + days);
}
// The same day `months` later, or that month's last day if it is shorter.
function addMonths(date: Date, months: number): Date {
  const first = new Date(date.getFullYear(), date.getMonth() + months, 1);
  const length = new Date(
    first.getFullYear(),
    first.getMonth() + 1,
    0,
  ).getDate();
  first.setDate(Math.min(date.getDate(), length));
  return first;
}
function sameDay(a: Date, b: Date): boolean {
  return key(a) === key(b);
}
function sameMonth(a: Date, b: Date): boolean {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth();
}
function clamp(day: Date, min?: Date, max?: Date): Date {
  if (min && day < min) return min;
  if (max && day > max) return max;
  return day;
}
function firstEnabled(month: Date, disabled: (day: Date) => boolean): Date {
  for (
    let day = monthStart(month);
    sameMonth(day, month);
    day = addDays(day, 1)
  )
    if (!disabled(day)) return day;
  return monthStart(month);
}
// A day's year, month and day as one comparable string.
function key(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}
// Six weeks of the month, Sunday first, with null for the days of the
// neighbouring months (a fixed height, so the dialog doesn't jump).
function weeks(month: Date): (Date | null)[][] {
  const first = monthStart(month);
  const cells: (Date | null)[] = Array.from(
    { length: first.getDay() },
    () => null,
  );
  for (let day = first; sameMonth(day, first); day = addDays(day, 1))
    cells.push(day);
  while (cells.length < 42) cells.push(null);
  return Array.from({ length: 6 }, (_, i) => cells.slice(i * 7, i * 7 + 7));
}
// "Friday, October 23rd 2026": what a screen reader says for a day.
function label(day: Date): string {
  const weekday = day.toLocaleDateString("en-US", { weekday: "long" });
  const n = day.getDate();
  const suffix =
    n % 100 >= 11 && n % 100 <= 13
      ? "th"
      : (["th", "st", "nd", "rd"][n % 10] ?? "th");
  return `${weekday}, ${monthName.format(day)} ${n}${suffix} ${day.getFullYear()}`;
}
