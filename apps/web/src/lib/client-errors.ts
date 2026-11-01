import { isRouteErrorResponse, matchPath } from "react-router";
import { ApiError, client, type ClientErrorReport } from "@/lib/api";
import { browserHttp } from "@/lib/api/http";
// The web app's own errors, sent to the API to be grouped with the server's
// (POST /api/client-errors, docs/api/observability.md#errors-from-the-browser):
// what the root error boundary shows the outage page for, and what reaches
// window's error and unhandledrejection events. Signed in only: the API
// refuses anyone else, and this asks for the session first, so a signed-out
// visitor sends nothing. One report per distinct error per page load, at
// most maxReports, never the page's URL or query (they hold reset and
// invitation tokens, searches and emails): only its route pattern. The
// admin build (the one not served from "/") reports nothing: its pages have
// an admin session, which this endpoint doesn't take, and an app session
// beside it would put the dashboard's errors in the app's name.

// The app build's page routes (routes.ts), static ones first, and "*" for
// the not-found page. The Go server keeps the same list (clientRoutes in
// endpoints/account/client_errors.go) and stores any other as "unknown";
// client-errors.test.ts checks the three agree.
export const clientRoutePatterns = [
  "/",
  "/auth/login",
  "/auth/register",
  "/auth/register/verify",
  "/auth/forgot-password",
  "/app",
  "/app/onboarding",
  "/app/verify-email",
  "/app/settings",
  "/auth/forgot-password/:token",
  "/auth/invitations/:token",
  "/auth/email-blocks/:token",
  "/app/:workspace/dashboard",
  "/app/:workspace/projects",
  "/app/:workspace/projects/:project",
  "/app/:workspace/settings",
  "/app/:workspace/settings/members",
] as const;
const notFoundPattern = "*";
// The route pattern of a page's path: never the path itself.
export function routePattern(pathname: string): string {
  const match = clientRoutePatterns.find(
    (pattern) => matchPath({ path: pattern, end: true }, pathname) !== null,
  );
  return match ?? notFoundPattern;
}
// How many errors a page load reports, at most.
const maxReports = 10;
// The API takes 4 KiB; the message and stack are cut to fit well inside it.
const maxMessage = 500;
const maxStack = 2500;
const maxBody = 3800;
// Errors already reported in this page load, by a hash of their message and
// first stack line: nothing of the error itself is kept.
const reported = new Set<number>();
// Set once the API refuses a report for a limit (429 or 503): nothing more
// is sent until the page loads again.
let refused = false;
// Reports error, unless it isn't worth one or was reported already. Never
// throws, and never reports its own failure.
export async function reportClientError(error: unknown): Promise<void> {
  try {
    if (!appBuild() || refused) return;
    const report = describe(error);
    if (!report) return;
    const key = hash(`${report.message}\n${firstLine(report.stack)}`);
    if (reported.has(key) || reported.size >= maxReports) return;
    reported.add(key);
    // The page it happened on, before anything awaited moves on.
    const route = routePattern(window.location.pathname);
    const session = await client(browserHttp()).auth().me();
    await client(browserHttp(session.csrfToken))
      .clientErrors()
      .store(fit({ ...report, route }));
  } catch (failure) {
    // Signed out (401) or unreachable: nothing more to do. Over a limit:
    // stop for this page load, so an error loop doesn't keep asking.
    if (
      failure instanceof ApiError &&
      (failure.status === 429 || failure.status === 503)
    )
      refused = true;
  }
}
// Listens for errors nothing else caught, until the returned function is
// called. The root route calls it once.
export function listenForClientErrors(): () => void {
  const onError = (event: ErrorEvent) =>
    void reportClientError(event.error ?? event.message);
  const onRejection = (event: PromiseRejectionEvent) =>
    void reportClientError(event.reason);
  window.addEventListener("error", onError);
  window.addEventListener("unhandledrejection", onRejection);
  return () => {
    window.removeEventListener("error", onError);
    window.removeEventListener("unhandledrejection", onRejection);
  };
}
// Forgets what this page load reported, for tests.
export function resetClientErrorReports() {
  reported.clear();
  refused = false;
}
type Described = Pick<ClientErrorReport, "message" | "stack">;
// What to report of a thrown value, or null for one that isn't the web
// app's bug: an API answer (the server has its own record of a 5xx), a
// route's 404, the network failing, or a cross-origin script's opaque
// "Script error.". A value that isn't an Error is reported by its type
// only, since its printed form could hold anything.
function describe(error: unknown): Described | null {
  if (error instanceof ApiError || isRouteErrorResponse(error)) return null;
  if (error instanceof Error) {
    if (networkFailure(error)) return null;
    const message = withoutSnippet(`${error.name}: ${error.message}`);
    return { message, stack: withoutSnippet(error.stack ?? "") };
  }
  if (typeof error === "string") {
    if (error === "" || error === "Script error.") return null;
    return { message: withoutSnippet(error), stack: "" };
  }
  if (error === undefined || error === null) return null;
  return { message: `A ${typeof error} was thrown`, stack: "" };
}
// V8's JSON.parse quotes the text it couldn't read (Unexpected token 'o',
// "{"name":"Ada"}" is not valid JSON), often an API answer with someone's
// data in it: the quote goes before anything is sent. The API scrubs it
// too.
const jsonSnippet = /(?:\.\.\.)?"[^\n]*"(?:\.\.\.)?(\s+is not valid JSON)/g;
function withoutSnippet(text: string): string {
  return text.replace(jsonSnippet, '"…"$1');
}
const networkMessages = [
  "Failed to fetch",
  "NetworkError when attempting to fetch resource.",
  "Load failed",
];
function networkFailure(error: Error): boolean {
  return (
    error.name === "AbortError" ||
    error.name === "TimeoutError" ||
    (error.name === "TypeError" && networkMessages.includes(error.message))
  );
}
// The app build is served from "/", the admin build from its own base.
function appBuild(): boolean {
  return import.meta.env.BASE_URL === "/";
}
// The report cut to fit the API's limit.
function fit(report: Described & { route: string }): ClientErrorReport {
  const release = import.meta.env.RELEASE ?? "";
  let stack = report.stack.slice(0, maxStack);
  const message = report.message.slice(0, maxMessage);
  const size = (s: string) =>
    new TextEncoder().encode(
      JSON.stringify({ message, stack: s, route: report.route, release }),
    ).length;
  while (stack !== "" && size(stack) > maxBody)
    stack = stack.slice(0, Math.floor(stack.length / 2));
  return { message, stack, route: report.route, release };
}
function firstLine(stack: string): string {
  const lines = stack.split("\n");
  return lines.find((line) => /^\s*at\s|@/.test(line)) ?? "";
}
// FNV-1a, 32 bits: enough to tell ten errors apart.
function hash(text: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h;
}
