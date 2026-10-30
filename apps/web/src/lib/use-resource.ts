import { useState } from "react";
import { useRouter } from "next/router";
import useSWR from "swr";
import { ApiError, errorMessage, useApi, type ApiPath } from "./api";
import { loginPath } from "./routes";
import { useOptionalSession } from "./session";
import type { Initial } from "./types";
// SWR for a typed path from `paths`; the key includes the session's user ID.
// Server-rendered data is used while `path` matches it. While a new path
// loads, or if it fails, the last loaded data stays available. Expired
// sessions go to login, returning to this page afterwards.
export function useResource<T>(path: ApiPath<T>, initial?: Initial<T>) {
  const api = useApi();
  const router = useRouter();
  const userId = useOptionalSession()?.user.id;
  const rendered = initial?.path === path;
  const { data, error, isValidating, mutate } = useSWR<T>(
    [path, userId],
    () => api.get(path),
    {
      fallbackData: rendered ? (initial.data ?? undefined) : undefined,
      onError(error) {
        if (error instanceof ApiError && error.status === 401)
          void router.replace(loginPath(router.asPath));
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
