import { vi } from "vitest";
// A stand-in for the Go API in component and hook tests: answers fetch from a
// table of replies and records what was sent.
//
//   const api = stubApi({
//     "POST /api/auth/password/verify": { status: 204 },
//     "PATCH /api/auth/password": [{ status: 422, body: { error, field } }, { body: session }],
//     "*": { body: {} },
//   });
//   api.sent("PATCH");   // [{ method, url, body, csrf, adminKey }]
//
// A key is "METHOD url": the exact URL first, then the path without its query,
// then "*". A list of replies is used in order and its last one repeats.
// Anything unmatched gets a 404, so a missing stub shows up as an error.
export type Reply = {
  status?: number; // 200 by default
  body?: unknown; // sent as JSON; no body when left out
  gate?: Promise<unknown>; // hold the response until this settles
  network?: boolean; // fail as if the API were unreachable
};
export type Sent = {
  method: string;
  url: string;
  body: unknown;
  csrf: string | null;
  // The admin pages' X-Admin-Key, only on requests that have one.
  adminKey?: string;
};
export function stubApi(replies: Record<string, Reply | Reply[]>) {
  const used = new Map<string, number>();
  function replyFor(method: string, url: string): Reply | undefined {
    const path = url.split("?")[0];
    for (const key of [`${method} ${url}`, `${method} ${path}`, "*"]) {
      const reply = replies[key];
      if (reply === undefined) continue;
      if (!Array.isArray(reply)) return reply;
      const index = used.get(key) ?? 0;
      used.set(key, index + 1);
      return reply[Math.min(index, reply.length - 1)];
    }
    return undefined;
  }
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? "GET";
    const reply = replyFor(method, url);
    if (!reply) return json({ error: `No stub for ${method} ${url}` }, 404);
    await reply.gate;
    if (reply.network) throw new TypeError("Failed to fetch");
    const status = reply.status ?? 200;
    return reply.body === undefined
      ? new Response(null, { status })
      : json(reply.body, status);
  });
  vi.stubGlobal("fetch", fetch);
  return {
    fetch,
    // The requests sent so far, optionally only one method's.
    sent(method?: string): Sent[] {
      return fetch.mock.calls
        .map(([url, init]) => {
          const headers = new Headers(init?.headers);
          const adminKey = headers.get("X-Admin-Key");
          return {
            method: init?.method ?? "GET",
            url,
            body:
              typeof init?.body === "string"
                ? JSON.parse(init.body)
                : undefined,
            csrf: headers.get("X-CSRF-Token"),
            ...(adminKey === null ? {} : { adminKey }),
          };
        })
        .filter((request) => !method || request.method === method);
    },
  };
}
// A fresh Response per call: a body can only be read once.
function json(body: unknown, status: number) {
  return new Response(JSON.stringify(body), { status });
}
// A promise the test settles, for holding a reply (Reply.gate) to check the
// pending state.
export function gate() {
  let release: () => void = () => {};
  const promise = new Promise<void>((resolve) => (release = resolve));
  return { promise, release };
}
