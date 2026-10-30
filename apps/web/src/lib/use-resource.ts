import { useState } from "react";
import { useLocation, useNavigate } from "react-router";
import useSWR from "swr";
import { ApiError, errorMessage, useApi, type ApiPath } from "./api";
import { loginPath } from "./routes";
import { useOptionalSession } from "./session";
// SWR for a typed path from `paths`; the key includes the session's user ID.
// Pages render at once and show a skeleton while `loading`. `fallback` (e.g.
// a record from an already loaded list) is shown until the fetch returns.
// While a new path loads, or if it fails, the last loaded data stays
// available. A null path fetches nothing. Expired sessions go to login,
// returning to this page afterwards.
export function useResource<T>(path: ApiPath<T> | null, fallback?: T) {
  const api = useApi();
  const navigate = useNavigate();
  const location = useLocation();
  const userId = useOptionalSession()?.user.id;
  const { data, error, isValidating, mutate } = useSWR<T>(
    path === null ? null : [path, userId],
    () => api.get(path as ApiPath<T>),
    {
      fallbackData: fallback,
      onError(error) {
        if (error instanceof ApiError && error.status === 401)
          void navigate(loginPath(location.pathname + location.search), {
            replace: true,
          });
      },
    },
  );
  const [kept, setKept] = useState(data);
  if (data !== undefined && data !== kept) setKept(data);
  return {
    data: data ?? kept,
    error: error ? errorMessage(error) : null,
    // The record doesn't exist, or isn't the member's (the API answers 404).
    missing: error instanceof ApiError && error.status === 404,
    // No data for this path yet (an earlier path's may be shown meanwhile).
    loading: data === undefined && isValidating,
    refreshing: isValidating,
    refresh: () => void mutate(),
  };
}
