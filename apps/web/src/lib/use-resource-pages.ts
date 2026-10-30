import { useCallback } from "react";
import { useRouter } from "next/router";
import { useSWRConfig } from "swr";
import useSWRInfinite, { unstable_serialize } from "swr/infinite";
import { ApiError, errorMessage, useApi, type ApiPath } from "./api";
import type { Page } from "./pagination";
import { loginPath } from "./routes";
import { useOptionalSession } from "./session";
import type { Initial } from "./types";
type PathFor<T> = (page: number) => ApiPath<Page<T>>;
type Key<T> = [ApiPath<Page<T>>, string | undefined];
// SWR keys for each page of a list: [path, userId], like useResource. No key
// after the last page, so loading stops there.
function pageKeys<T>(pathFor: PathFor<T>, userId: string | undefined) {
  return (index: number, previous: Page<T> | null): Key<T> | null =>
    previous && previous.pages.current >= previous.pages.total
      ? null
      : [pathFor(index + 1), userId];
}
// SWR over consecutive pages of a typed list path, for infinite scrolling.
// Page 1 can come from the server, and is revalidated whenever the list is
// (mount, focus, refresh), so new records show up. Items are de-duplicated by ID, since
// offset pages shift when records are added. A failed page keeps the loaded
// items; `retry` loads it again. Expired sessions go to login.
export function useResourcePages<T extends { id: string }>(
  pathFor: PathFor<T>,
  initial?: Initial<Page<T>>,
) {
  const api = useApi();
  const router = useRouter();
  const userId = useOptionalSession()?.user.id;
  const rendered = initial?.path === pathFor(1) && initial.data;
  const { data, error, size, setSize, isValidating, mutate } = useSWRInfinite<
    Page<T>,
    unknown,
    (index: number, previous: Page<T> | null) => Key<T> | null
  >(pageKeys(pathFor, userId), ([path]) => api.get(path), {
    fallbackData: rendered ? [rendered] : undefined,
    onError(error) {
      if (error instanceof ApiError && error.status === 401)
        void router.replace(loginPath(router.asPath));
    },
  });
  const pages = data ?? [];
  const last = pages.at(-1);
  const hasMore = last ? last.pages.current < last.pages.total : false;
  // A requested page that hasn't arrived yet.
  const loadingMore = isValidating && size > pages.length;
  const loadMore = useCallback(() => {
    if (!loadingMore && hasMore && !error) void setSize(pages.length + 1);
  }, [loadingMore, hasMore, error, setSize, pages.length]);
  return {
    items: uniqueById(pages.flatMap((page) => page.data)),
    total: pages[0]?.pages.items,
    hasMore,
    loading: pages.length === 0 && isValidating,
    loadingMore,
    loadMore,
    error: error ? errorMessage(error) : null,
    retry: () => void mutate(),
  };
}
// Revalidates every loaded page of a useResourcePages list, from anywhere,
// e.g. after creating or renaming a record it lists.
export function useRefreshPages<T>(pathFor: PathFor<T>) {
  const { mutate } = useSWRConfig();
  const userId = useOptionalSession()?.user.id;
  return useCallback(
    () => mutate(unstable_serialize(pageKeys(pathFor, userId))),
    [mutate, pathFor, userId],
  );
}
function uniqueById<T extends { id: string }>(items: T[]): T[] {
  const seen = new Set<string>();
  return items.filter((item) => !seen.has(item.id) && seen.add(item.id));
}
