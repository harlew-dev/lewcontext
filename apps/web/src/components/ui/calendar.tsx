import { useSyncExternalStore, type ReactNode } from "react";
import { Calendar as BaseCalendar } from "baseui/datepicker";
import { ChevronLeftIcon, ChevronRightIcon } from "@heroicons/react/20/solid";
import { slot } from "@/lib/baseui";
import { cx } from "@/lib/cx";
// A month grid for picking a day or, with `range`, a range of days: BaseUI's
// Calendar (keyboard: arrows move, Enter picks; Page Up and Down change
// month) with its slots drawn by our classes. One month on phones, two from
// `sm:`. The month's name is plain text: the arrows change it.
//
// BaseUI hands back local Dates at local midnight. Read their year, month
// and day (getFullYear, getMonth, getDate), never toISOString(), which shifts
// the day for anyone east of UTC (docs/ui/base-ui.md).
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
  const first = Array.isArray(value) ? value[0] : value;
  return (
    <BaseCalendar
      // BaseUI picks a range only into an array: nothing picked is [].
      value={range && !Array.isArray(value) ? (value ? [value] : []) : value}
      // With nothing picked, two months end at the latest pickable one
      // rather than starting there.
      highlightedDate={!first && wide && maxDate ? monthBefore(maxDate) : null}
      onChange={({ date }) => onChange(date ?? null)}
      range={range}
      minDate={minDate}
      maxDate={maxDate}
      monthsShown={wide ? 2 : 1}
      peekNextMonth={false}
      fixedHeight
      overrides={{
        Root: {
          component: CalendarRoot,
          props: {
            className,
            role: "group",
            "aria-roledescription": "calendar",
          },
        },
        MonthContainer: { component: CalendarMonths },
        CalendarContainer: { component: CalendarGrid },
        CalendarHeader: { component: CalendarHeader },
        PrevButton: { component: CalendarArrow },
        NextButton: { component: CalendarArrow },
        PrevButtonIcon: { component: PreviousIcon },
        NextButtonIcon: { component: NextIcon },
        MonthYearSelectPopover: { component: PlainMonthTitle },
        MonthYearSelectButton: { component: CalendarTitle },
        MonthYearSelectIconContainer: { component: Nothing },
        MonthHeader: { component: CalendarWeekdays },
        WeekdayHeader: { component: CalendarWeekday },
        Month: { component: CalendarMonth },
        Week: { component: CalendarWeek },
        Day: { component: CalendarDay },
      }}
    />
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
const CalendarRoot = slot("div", "w-fit max-w-full select-none");
const CalendarMonths = slot("div", "flex flex-col gap-6 sm:flex-row sm:gap-8");
const CalendarGrid = slot("div", "outline-hidden");
const CalendarHeader = slot(
  "div",
  "flex h-9 items-center justify-between gap-2",
);
// Previous and next month: hidden (but keeping their place) where the
// other month of two shows them.
const CalendarArrow = slot(
  "button",
  [
    "grid size-9 place-items-center rounded-lg text-zinc-500 dark:text-zinc-400",
    "data-hover:bg-zinc-950/5 data-hover:text-zinc-950 dark:data-hover:bg-white/10 dark:data-hover:text-white",
    "data-focus:outline-2 data-focus:outline-blue-500",
    "data-disabled:opacity-30 data-disabled:data-hover:bg-transparent",
  ].join(" "),
);
function PreviousIcon() {
  return <ChevronLeftIcon className="size-5" aria-hidden="true" />;
}
function NextIcon() {
  return <ChevronRightIcon className="size-5" aria-hidden="true" />;
}
// The month and year, as text: BaseUI's month and year menus are left out.
function PlainMonthTitle({ children }: { children?: ReactNode }) {
  return <>{children}</>;
}
const CalendarTitle = slot(
  "span",
  "flex-1 text-center text-sm/6 font-semibold text-zinc-950 dark:text-white",
);
function Nothing() {
  return null;
}
const CalendarWeekdays = slot("div", "mt-2 flex");
const CalendarWeekday = slot(
  "div",
  "grid size-10 place-items-center text-xs/6 font-medium text-zinc-500 sm:size-9 dark:text-zinc-400",
);
const CalendarMonth = slot("div", "");
const CalendarWeek = slot("div", "flex");
// A day: blue at the ends of the range (or when picked), a pale band
// between them and while choosing the end, a ring when the keyboard is on
// it, faded when it can't be picked.
const CalendarDay = slot(
  "div",
  cx(
    "relative grid size-10 place-items-center text-sm/6 text-zinc-950 outline-hidden sm:size-9 dark:text-white",
    "not-data-disabled:cursor-pointer",
    "data-hover:not-data-selected:bg-zinc-950/5 dark:data-hover:not-data-selected:bg-white/10",
    "data-range:bg-blue-500/15 dark:data-range:bg-blue-500/25",
    "data-selected:rounded-lg data-selected:bg-blue-500 data-selected:font-semibold data-selected:text-white",
    "data-start:rounded-r-none data-end:rounded-l-none",
    "rounded-lg data-range:rounded-none",
    "data-focus:outline-2 data-focus:-outline-offset-2 data-focus:outline-blue-500",
    "data-disabled:cursor-default data-disabled:text-zinc-950/25 dark:data-disabled:text-white/25",
    "forced-colors:data-selected:bg-[Highlight] forced-colors:data-selected:text-[HighlightText]",
  ),
  {
    // $isHighlighted is the day under the pointer or the keyboard: the
    // pointer gets data-hover from $isHovered, the keyboard a ring.
    ignore: ["$isHighlighted"],
    props: (state) => ({
      "data-range":
        state.$pseudoSelected || state.$pseudoHighlighted ? "" : undefined,
      "data-start": state.$startDate ? "" : undefined,
      "data-end": state.$endDate ? "" : undefined,
      "data-focus":
        state.$isHighlighted && state.$isFocusVisible ? "" : undefined,
    }),
  },
);
// The same day a month earlier (or the month's last day, if shorter).
function monthBefore(date: Date): Date {
  const day = new Date(date.getFullYear(), date.getMonth() - 1, 1);
  const last = new Date(day.getFullYear(), day.getMonth() + 1, 0).getDate();
  day.setDate(Math.min(date.getDate(), last));
  return day;
}
