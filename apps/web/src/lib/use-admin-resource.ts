import { useState } from "react";
import { useNavigate } from "react-router";
import useSWR from "swr";
import { ApiError, errorMessage, useAdminApi, type ApiPath } from "./api";
import { useAdminKey, useAdminSession } from "./admin";
import { routes } from "./routes";
// useResource for the admin pages: a typed path from paths.admin, loaded with
// the admin API. The SWR key is ["admin", the admin's user ID, path], apart
// from the app's, and never holds the key. An ended admin session goes back
// to the admin sign-in page.
export function useAdminResource<T>(path: ApiPath<T> | null) {
  const api = useAdminApi();
  const navigate = useNavigate();
  const key = useAdminKey();
  const adminId = useAdminSession().user.id;
  const { data, error, isValidating, mutate } = useSWR<T>(
    path === null ? null : ["admin", adminId, path],
    () => api.get(path as ApiPath<T>),
    {
      onError(error) {
        if (error instanceof ApiError && error.status === 401)
          void navigate(routes.admin.login(key), { replace: true });
      },
    },
  );
  const [kept, setKept] = useState(data);
  if (data !== undefined && data !== kept) setKept(data);
  return {
    data: data ?? kept,
    error: error ? errorMessage(error) : null,
    missing: error instanceof ApiError && error.status === 404,
    loading: data === undefined && isValidating,
    refreshing: isValidating,
    refresh: () => void mutate(),
  };
}
