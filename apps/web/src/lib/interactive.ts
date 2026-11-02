import { useState, type FocusEvent, type PointerEvent } from "react";
type Handlers<T> = {
  onPointerEnter?: (event: PointerEvent<T>) => void;
  onPointerLeave?: (event: PointerEvent<T>) => void;
  onPointerDown?: (event: PointerEvent<T>) => void;
  onPointerUp?: (event: PointerEvent<T>) => void;
  onFocus?: (event: FocusEvent<T>) => void;
  onBlur?: (event: FocusEvent<T>) => void;
};
const flag = (on: boolean) => (on ? "" : undefined);
// Sets the data-hover / data-active / data-focus attributes the Tailwind classes
// target (Headless UI's convention), for plain links and buttons. BaseUI-backed
// controls get the same attributes from their state props (see lib/baseui.tsx).
export function useInteractive<T extends HTMLElement>(handlers: Handlers<T>) {
  const [hover, setHover] = useState(false);
  const [active, setActive] = useState(false);
  const [focus, setFocus] = useState(false);
  return {
    "data-hover": flag(hover),
    "data-active": flag(active),
    "data-focus": flag(focus),
    onPointerEnter(event: PointerEvent<T>) {
      if (event.pointerType === "mouse") setHover(true);
      handlers.onPointerEnter?.(event);
    },
    onPointerLeave(event: PointerEvent<T>) {
      setHover(false);
      setActive(false);
      handlers.onPointerLeave?.(event);
    },
    onPointerDown(event: PointerEvent<T>) {
      setActive(true);
      handlers.onPointerDown?.(event);
    },
    onPointerUp(event: PointerEvent<T>) {
      setActive(false);
      handlers.onPointerUp?.(event);
    },
    onFocus(event: FocusEvent<T>) {
      setFocus(event.currentTarget.matches(":focus-visible"));
      handlers.onFocus?.(event);
    },
    onBlur(event: FocusEvent<T>) {
      setFocus(false);
      handlers.onBlur?.(event);
    },
  };
}
