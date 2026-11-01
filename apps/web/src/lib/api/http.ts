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
// What a 428 asks the browser to show: a rate limit's challenge level wants
// a solved CAPTCHA (docs/api/rate-limits.md#challenges).
export type Challenge = { provider: string; siteKey: string; action: string };
// Shows the challenge and resolves with its token, or rejects (cancelled,
// failed): ChallengeProvider's solve (lib/challenge/challenge.tsx).
export type Solve = (challenge: Challenge) => Promise<string>;
// The header the resent request carries the token in.
export const challengeTokenHeader = "X-Challenge-Token";
// How resource files reach the API.
export type Http = {
  get<T>(path: ApiPath<T>): Promise<T>;
  send<T>(method: Method, path: string, body?: unknown): Promise<T>;
};
// Where and how requests go: the base URL, headers sent with every request,
// cookies and a timeout. onResponse sees every response, answered or
// refused, before it's read (the admin pages watch a header with it).
export type Transport = {
  base: string;
  headers: Record<string, string>;
  credentials?: RequestCredentials;
  timeout: number;
  onResponse?: (response: Response) => void;
  // Answers a 428 by showing the CAPTCHA; without it a 428 is an error, as
  // for the loaders' read-only client.
  solve?: Solve;
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
// A request, and once more with the token when the API answers 428 and the
// transport can solve challenges: the awaited promise stays pending while
// the dialog is open, so callers (useAction, useAuthSubmit, direct calls)
// see one request that took longer. Each check runs before any side effect
// on the server, so resending is safe; it happens at most once, and a
// second 428 is an error like any other. The timeout is per fetch.
async function request<T>(
  transport: Transport,
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  try {
    return await send<T>(transport, method, path, body);
  } catch (error) {
    const challenge = challengeOf(error);
    if (!challenge || !transport.solve) throw error;
    const token = await transport.solve(challenge);
    return send<T>(transport, method, path, body, token);
  }
}
// The challenge a 428 carries, or null for any other error.
function challengeOf(error: unknown): Challenge | null {
  if (!(error instanceof ApiError) || error.status !== 428) return null;
  const challenge = error.details.challenge as Partial<Challenge> | undefined;
  return challenge &&
    typeof challenge.provider === "string" &&
    typeof challenge.siteKey === "string" &&
    typeof challenge.action === "string"
    ? (challenge as Challenge)
    : null;
}
async function send<T>(
  { base, headers, credentials, timeout, onResponse }: Transport,
  method: string,
  path: string,
  body?: unknown,
  token?: string,
): Promise<T> {
  const init = new Headers(headers);
  if (body !== undefined) init.set("Content-Type", "application/json");
  if (token !== undefined) init.set(challengeTokenHeader, token);
  const response = await fetch(base + path, {
    method,
    headers: init,
    credentials,
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(timeout),
  });
  onResponse?.(response);
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
// token when one is given. With `solve` (useApi passes the page's
// ChallengeProvider), a 428 shows the CAPTCHA and resends once.
export function browserHttp(csrfToken?: string, solve?: Solve): Http {
  return httpWith({
    base: "",
    headers: csrfToken ? { "X-CSRF-Token": csrfToken } : {},
    credentials: "same-origin",
    timeout: 12000,
    solve,
  });
}
