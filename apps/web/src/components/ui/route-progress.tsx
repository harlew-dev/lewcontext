import { useNavigation } from "react-router";
import { useDelayedLoading } from "@/lib/use-delayed-loading";
// Thin bar at the top of the viewport while a slow client-side navigation
// waits for a route loader. Only shown for navigations slower than 200 ms,
// so quick page changes don't flash it.
export function RouteProgress() {
  const loading = useDelayedLoading(useNavigation().state !== "idle");
  return loading ? (
    <div
      role="status"
      aria-label="Loading page"
      className="fixed inset-x-0 top-0 z-50 h-0.5 animate-pulse bg-zinc-950 dark:bg-white"
    />
  ) : null;
}
