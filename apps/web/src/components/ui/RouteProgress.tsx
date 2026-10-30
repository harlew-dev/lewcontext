import { useEffect, useState } from "react";
import { useRouter } from "next/router";
export function RouteProgress() {
  const router = useRouter();
  const [loading, setLoading] = useState(false);
  useEffect(() => {
    const start = () => setLoading(true);
    const stop = () => setLoading(false);
    router.events.on("routeChangeStart", start);
    router.events.on("routeChangeComplete", stop);
    router.events.on("routeChangeError", stop);
    return () => {
      router.events.off("routeChangeStart", start);
      router.events.off("routeChangeComplete", stop);
      router.events.off("routeChangeError", stop);
    };
  }, [router.events]);
  return loading ? (
    <div
      role="status"
      aria-label="Loading page"
      className="fixed top-0 left-0 z-[9999] h-0.75 w-full bg-mauve-400"
    />
  ) : null;
}
