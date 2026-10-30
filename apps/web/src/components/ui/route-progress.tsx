import { useEffect, useState } from "react";
import { useNavigation } from "react-router";
// Thin bar at the top of the viewport while a slow client-side navigation
// waits for a route loader.
export function RouteProgress() {
  const busy = useNavigation().state !== "idle";
  const [loading, setLoading] = useState(false);
  if (!busy && loading) setLoading(false);
  useEffect(() => {
    if (!busy) return;
    // Only show the bar for navigations slower than this, so quick page
    // changes don't flash it.
    const timer = setTimeout(() => setLoading(true), 200);
    return () => clearTimeout(timer);
  }, [busy]);
  return loading ? (
    <div
      role="status"
      aria-label="Loading page"
      className="fixed inset-x-0 top-0 z-50 h-0.5 animate-pulse bg-zinc-950 dark:bg-white"
    />
  ) : null;
}
