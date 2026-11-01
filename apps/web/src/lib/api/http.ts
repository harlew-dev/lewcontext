import type { ApiPath } from "./paths";
export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
    // The request body field the error is about, when the API names one.
    public field?: string,
    // The rest of the error's body, for errors that carry more (the
    // workspaces in the 409 for deleting an account).
    public details: Record<string, unknown> = {},
  ) {
    super(message);
  }
}
export function errorMessage(error: unknown): string {
  return error instanceof Error
    ? error.message
    : "Something went wrong. Please try again.";
}
export type Method = "POST" | "PATCH" | "DELETE";
// How resource files reach the API.
export type Http = {
  get<T>(path: ApiPath<T>): Promise<T>;
  send<T>(method: Method, path: string, body?: unknown): Promise<T>;
};
// Where and how requests go: the base URL, headers sent with every request,
// cookies and a timeout.
export type Transport = {
  base: string;
  headers: Record<string, string>;
  credentials?: RequestCredentials;
  timeout: number;
};
// An Http on a transport: browserHttp below, and the admin dashboard's
// adminHttp (lib/api/admin.ts).
export function httpWith(transport: Transport): Http {
  const call = <T>(method: string, path: string, body?: unknown) =>
    request<T>(transport, method, path, body);
  return {
    get: (path) => call("GET", path),
    send: (method, path, body) => call(method, path, body),
  };
}
async function request<T>(
  { base, headers, credentials, timeout }: Transport,
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const init = new Headers(headers);
  if (body !== undefined) init.set("Content-Type", "application/json");
  const response = await fetch(base + path, {
    method,
    headers: init,
    credentials,
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(timeout),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    throw new ApiError(
      data.error || "Unable to connect. Please try again.",
      response.status,
      typeof data.field === "string" ? data.field : undefined,
      typeof data === "object" && data !== null ? data : {},
    );
  }
  // 204, and 202 for work done later (a password reset link), carry no body.
  const text = await response.text();
  return (text === "" ? undefined : JSON.parse(text)) as T;
}
// Requests go to the same-origin /api (Go serves it; Vite proxies it in
// development) with the session cookie; mutations add the session's CSRF
// token when one is given.
export function browserHttp(csrfToken?: string): Http {
  return httpWith({
    base: "",
    headers: csrfToken ? { "X-CSRF-Token": csrfToken } : {},
    credentials: "same-origin",
    timeout: 12000,
  });
}
