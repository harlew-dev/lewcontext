import { useEffect, useState } from "react";
import { useRouter } from "next/router";
// Thin bar at the top of the viewport while a slow client-side navigation loads.
export function RouteProgress() {
  const router = useRouter();
  const [loading, setLoading] = useState(false);
  useEffect(() => {
    // Only show the bar for navigations slower than this, so quick page
    // changes don't flash it.
    let timer: ReturnType<typeof setTimeout> | undefined;
    const start = () => {
      clearTimeout(timer);
      timer = setTimeout(() => setLoading(true), 200);
    };
    const stop = () => {
      clearTimeout(timer);
      setLoading(false);
    };
    router.events.on("routeChangeStart", start);
    router.events.on("routeChangeComplete", stop);
    router.events.on("routeChangeError", stop);
    return () => {
      clearTimeout(timer);
      router.events.off("routeChangeStart", start);
      router.events.off("routeChangeComplete", stop);
      router.events.off("routeChangeError", stop);
    };
  }, [router.events]);
  return loading ? (
    <div
      role="status"
      aria-label="Loading page"
      className="fixed inset-x-0 top-0 z-50 h-0.5 animate-pulse bg-zinc-950 dark:bg-white"
    />
  ) : null;
}
