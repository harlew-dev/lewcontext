import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { Calendar, type CalendarValue } from "./calendar";
function Range({ onChange }: { onChange: (value: CalendarValue) => void }) {
  const [value, setValue] = useState<CalendarValue>(null);
  return (
    <Calendar
      range
      value={value}
      onChange={(next) => {
        setValue(next);
        onChange(next);
      }}
      minDate={new Date(2026, 9, 5)}
      maxDate={new Date(2026, 10, 1)}
    />
  );
}
describe("Calendar", () => {
  it("picks a range of local dates, from nothing picked, within its bounds", () => {
    vi.useFakeTimers({ toFake: ["Date"], now: new Date(2026, 10, 1, 12) });
    try {
      const changes = vi.fn();
      render(<Range onChange={changes} />);
      // With nothing picked, the two months end at the latest pickable one.
      expect(screen.getByText("October")).toBeInTheDocument();
      expect(screen.getByText("November")).toBeInTheDocument();
      fireEvent.click(
        screen.getByRole("gridcell", { name: /October 23rd 2026/ }),
      );
      fireEvent.click(
        screen.getByRole("gridcell", { name: /October 20th 2026/ }),
      );
      // Picked backwards, the range still runs forwards.
      expect(changes).toHaveBeenLastCalledWith([
        new Date(2026, 9, 20),
        new Date(2026, 9, 23),
      ]);
      // Outside the bounds, a day can't be picked.
      fireEvent.click(
        screen.getByRole("gridcell", { name: /November 2nd 2026/ }),
      );
      fireEvent.click(
        screen.getByRole("gridcell", { name: /October 4th 2026/ }),
      );
      expect(changes).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });
});
