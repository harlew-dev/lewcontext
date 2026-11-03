import { useSWRConfig } from "swr";
// Forgets every API response SWR has cached, infinite lists included, without
// refetching anything: after signing out or deleting the account, so no
// user data stays in memory. Call it once the signed-in pages have unmounted
// (after navigating away), so nothing on screen loses its data first.
export function useClearCache() {
  const { cache } = useSWRConfig();
  return () => {
    for (const key of Array.from(cache.keys())) cache.delete(key);
  };
}
