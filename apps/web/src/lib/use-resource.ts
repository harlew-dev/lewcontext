import { useState } from "react";
import { useRouter } from "next/router";
import useSWR from "swr";
import { ApiError, errorMessage, request } from "./api";
import { routes } from "./routes";
import type { Initial } from "./types";
// SWR for a signed-in API GET; the key includes the user ID. Server-rendered
// data is used while `path` matches it. While a new path loads, or if it
// fails, the last loaded data stays available. Expired sessions go to login.
export function useApi<T>(path: string, userId: number, initial?: Initial<T>) {
  const router = useRouter();
  const rendered = initial?.path === path;
  const { data, error, isValidating, mutate } = useSWR<T>(
    [path, userId],
    ([key]: [string, number]) => request<T>(key),
    {
      fallbackData: rendered ? (initial.data ?? undefined) : undefined,
      onError(error) {
        if (error instanceof ApiError && error.status === 401)
          void router.replace(routes.login);
      },
    },
  );
  const [kept, setKept] = useState(data);
  if (data !== undefined && data !== kept) setKept(data);
  return {
    data: data ?? kept,
    error: error
      ? errorMessage(error)
      : rendered && data === undefined
        ? initial.error
        : null,
    // No data for this path yet (an earlier path's may be shown meanwhile).
    loading: data === undefined && isValidating,
    refreshing: isValidating,
    refresh: () => void mutate(),
  };
}
