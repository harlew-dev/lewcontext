import { useState } from "react";
// How many times `open` has turned true. Key a dialog's form with it, so each
// opening starts empty (no values, errors or step left from the last one)
// while the closing dialog keeps its content as it animates out.
export function useOpenCount(open: boolean): number {
  const [count, setCount] = useState(open ? 1 : 0);
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setCount(count + 1);
  }
  return count;
}
