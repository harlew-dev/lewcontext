import { url, type Page } from "@/lib/pagination";
import type { ApiKey, ApiKeyInput, ApiKeyCreated } from "@/types/api-keys";
import type { Http } from "./http";
import { path } from "./paths";

export type ApiKeyIndexQuery = {
  page?: number;
  perPage?: number;
  q?: string;
  kind?: ApiKey["kind"];
};
export function apiKeyRoutes(workspace?: string) {
  const base = workspace
    ? `/api/workspaces/${workspace}/api-keys`
    : "/api/auth/api-keys";
  return {
    index: (query: ApiKeyIndexQuery = {}) =>
      path<Page<ApiKey>>(url(base, query)),
    store: () => base,
    update: (id: string) => `${base}/${id}`,
    destroy: (id: string) => `${base}/${id}`,
    matches: (key: string) =>
      key === base || key.startsWith(`${base}?`) || key.startsWith(`${base}/`),
  };
}
export function apiKeys(http: Http, workspace?: string) {
  const routes = apiKeyRoutes(workspace);
  return {
    index: (query: ApiKeyIndexQuery = {}) => http.get(routes.index(query)),
    store: (input: ApiKeyInput & { workspace?: string }) =>
      http.send<ApiKeyCreated>("POST", routes.store(), input),
    update: (id: string, input: ApiKeyInput) =>
      http.send<ApiKey>("PATCH", routes.update(id), input),
    destroy: (id: string) => http.send<void>("DELETE", routes.destroy(id)),
  };
}
