import { useEffect, useRef, useState } from "react";
type DelayOptions = {
  // How long `busy` must last before the loading state shows.
  delay?: number;
  // Once shown, how long it stays at least, so it never flashes.
  minDuration?: number;
};
// Whether to show a loading state for `busy` work: only once it has lasted
// `delay` ms, so a quick request shows nothing, and then for at least
// `minDuration` ms. A request taking 180 ms shows nothing; one taking
// 220 ms shows from 200 ms to 600 ms rather than flashing for 20 ms.
//   const loading = useDelayedLoading(busy);
export function useDelayedLoading(
  busy: boolean,
  { delay = 200, minDuration = 400 }: DelayOptions = {},
) {
  const [shown, setShown] = useState(false);
  const shownAt = useRef(0);
  useEffect(() => {
    if (busy && !shown) {
      const timer = setTimeout(() => {
        shownAt.current = Date.now();
        setShown(true);
      }, delay);
      return () => clearTimeout(timer);
    }
    if (!busy && shown) {
      const left = shownAt.current + minDuration - Date.now();
      const timer = setTimeout(() => setShown(false), Math.max(0, left));
      return () => clearTimeout(timer);
    }
  }, [busy, shown, delay, minDuration]);
  return shown;
}
