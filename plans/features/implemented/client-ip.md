# Client IP behind a proxy

Status: **implemented 2026-10-31** (see [How the build differs](#how-the-build-differs)), with every recommended decision taken. Docs: [deployment.md](../../../docs/deployment.md#behind-a-proxy-or-load-balancer), [api/README.md](../../../docs/api/README.md), [api/auth.md](../../../docs/api/auth.md#rate-limits), [api/rate-limits.md](../../../docs/api/rate-limits.md), [api/events.md](../../../docs/api/events.md), [api/testing.md](../../../docs/api/testing.md), `.env.example`, `README.md` and AGENTS.md. Fills in decision 13 of [CAPTCHA challenges](captcha-challenges.md). Used by every per-IP limit ([rate limits](rate-limits.md)) and by the `ip` field of events.

## Why

The Go server serves plain HTTP, so a production deployment always has something in front of it to terminate TLS: a reverse proxy, a load balancer, Cloudflare, or a Cloudflare Tunnel. The API reads the client's address from the TCP connection only (`clientIP(r)`, `httpapi/middleware.go`), so behind any of these every request appears to come from the proxy. The effects:

- every per-IP limit counts the whole site together: `auth:per-ip` would challenge everyone after 5 sign-ins and refuse everyone after 60;
- audit events (`auth.login_attempted`, `auth.password_reset.requested`, `rate_limit.reached`) record the proxy's address;
- the CAPTCHA check sends Cloudflare the proxy's address as `remoteip`.

Proxies send the real address in a header (`X-Forwarded-For`, `X-Real-IP`, `CF-Connecting-IP`). But anyone can send those headers too. If the API believed them from every request, an attacker would put a random address in each one, get a fresh per-IP budget every time, and fill the audit trail with fake addresses. So the header is believed **only on connections from addresses the deployment says are its proxies**. That is the AGENTS.md rule "do not trust forwarding headers blindly", made concrete.

## Design

### Config (`internal/config`)

| Variable | Example | Meaning |
| --- | --- | --- |
| `CLIENT_IP_HEADER` | `X-Forwarded-For` | The header your proxy puts the client's address in. One of `X-Forwarded-For`, `X-Real-IP`, `CF-Connecting-IP`, `True-Client-IP` (case-insensitive). Empty, the default, means use the connection's address, as today |
| `TRUSTED_PROXIES` | `10.0.0.0/8,127.0.0.1/32` | Comma-separated addresses or CIDR ranges your proxies connect from. The header is believed only on connections from these |

Startup refuses:

- a header without `TRUSTED_PROXIES`, or `TRUSTED_PROXIES` without a header (they only make sense together);
- an unknown header name;
- a range that can't be parsed;
- `0.0.0.0/0` or `::/0`, which would trust everyone and be the same as believing the header blindly.

`internal/config` parses the ranges into `[]netip.Prefix` once.

### One middleware, set once per request

```go
// httpapi/client_ip.go
// clientAddress works out the request's client address once, before any
// other handler, and stores it in the context. The header is believed only
// when the connection comes from a trusted proxy.
func (s *Server) clientAddress(next http.Handler) http.Handler

// clientIP is the address every limit, event and CAPTCHA check uses.
func clientIP(r *http.Request) string // reads the context; falls back to RemoteAddr
```

- `Routes()` wraps everything in `clientAddress`, both the API and the pages, so there is one answer per request everywhere. The existing `clientIP(r)` keeps its signature, and its callers (`limitAuth`, the login and reset events, the reset request's limits) don't change.
- If the connection isn't from a trusted proxy, the header is ignored entirely, even if present.

### Reading the header

- **Single-value headers** (`X-Real-IP`, `CF-Connecting-IP`, `True-Client-IP`): there must be exactly one value, and it must parse as an IP (`netip.ParseAddr`). Anything else falls back to the connection address.
- **`X-Forwarded-For`** is a list (`client, proxy1, proxy2`), and each proxy appends the address it received from. The client controls everything at the left, so the list is read **from the right**:
  1. start from the connection address (a trusted proxy);
  2. take entries right to left, skipping each one that is itself in `TRUSTED_PROXIES`;
  3. the first entry that isn't trusted is the client;
  4. if an entry doesn't parse, stop and use the last trusted hop, so a forged value can't choose the answer;
  5. if every entry is trusted, use the leftmost.

  Several `X-Forwarded-For` header lines are joined in order, as RFC 7230 allows. At most 20 entries are read.
- Addresses are normalised: IPv4-mapped IPv6 is unmapped, and zones are dropped.

### IPv6 and per-IP limits

One IPv6 user usually controls a whole /64 (2⁶⁴ addresses), so counting single IPv6 addresses lets anyone step around a per-IP limit by changing the last bits. This is true today, with or without a proxy. Per-IP rate-limit keys therefore use the address's **/64 for IPv6**, and the full address for IPv4 (`rateLimit.key` gets `limitSubject(addr)`). Events still store the full address.

### With `PER_IP_LIMITS`

`PER_IP_LIMITS=off` (CAPTCHA plan) stays, as the fallback for a proxy that can't send the client's address at all. With `CLIENT_IP_HEADER` set, per-IP limits work normally behind the proxy and `PER_IP_LIMITS` should stay `on`. Setting both is allowed; the startup warning says `PER_IP_LIMITS=off` isn't needed.

### Examples

| Deployment | `CLIENT_IP_HEADER` | `TRUSTED_PROXIES` |
| --- | --- | --- |
| nginx or Caddy on the same machine | `X-Forwarded-For` (nginx: `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for`) | `127.0.0.1/32,::1/128` |
| A cloud load balancer in a private network | `X-Forwarded-For` | the load balancer's subnet, e.g. `10.0.0.0/16` |
| Cloudflare proxying straight to the server | `CF-Connecting-IP` | Cloudflare's published ranges (`https://www.cloudflare.com/ips-v4`, `…/ips-v6`) |
| Cloudflare Tunnel (`cloudflared` on the same machine) | `CF-Connecting-IP` | `127.0.0.1/32,::1/128` |

With Cloudflare, the server must also accept connections **only** from Cloudflare (a firewall allowing only Cloudflare's ranges, or a Tunnel with no open port). Otherwise anyone who finds the server's own address can connect directly. A header from a direct connection is ignored, but that client still bypasses Cloudflare's edge rules. Cloudflare's ranges change rarely, but they do change, so they are listed in [deployment.md](../../../docs/deployment.md) with a note to recheck them.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| A client forges the header to get fresh limits or fake audit IPs | The header is believed only from `TRUSTED_PROXIES`. `X-Forwarded-For` is read from the right, so values the client adds at the left are never chosen. |
| Trusting everyone by mistake | `0.0.0.0/0` and `::/0` are refused at startup. A header with no proxies, or proxies with no header, is refused too. |
| A garbage or huge header | It must parse with `netip`; at most 20 list entries are read; anything else falls back. `MaxHeaderBytes` is already 16 KiB. |
| IPv6 users stepping around per-IP limits | Limit keys use the /64. |
| Direct connections bypassing Cloudflare | Documented: lock the origin to Cloudflare (firewall or Tunnel). The app ignores the header on such connections. |
| A proxy that passes on a client's own header unchanged | With single-value headers the whole chain is trusted, so the proxy must overwrite the header (Cloudflare does for `CF-Connecting-IP`; nginx needs `proxy_set_header X-Real-IP $remote_addr`). Documented per example. |
| Personal data | Unchanged: events already store an IP, pruned after 90 days. They now store the real one. |

## Tests

- **Go unit (`config`):**
  - header and proxies must be set together;
  - unknown header names, unparseable ranges, `0.0.0.0/0` and `::/0` are refused;
  - single addresses and CIDRs both parse.
- **Go unit (`httpapi`, table-driven over the resolver):**
  - **Untrusted connections:** a connection from outside the trusted ranges ignores the header.
  - **`X-Forwarded-For`:**
    - the rightmost untrusted entry wins;
    - a forged `X-Forwarded-For: 6.6.6.6` from the client is never chosen behind one trusted proxy;
    - several proxy hops;
    - every entry trusted gives the leftmost;
    - an unparseable entry stops at the last trusted hop;
    - several header lines are joined;
    - the 20-entry cap.
  - **Single-value headers:** one valid value is used; two values or a bad value fall back.
  - **Normalisation:** IPv4-mapped IPv6 and zones.
  - **Limit keys:** two IPv6 addresses in one /64 share a key; IPv4 addresses don't.
- **Go integration:**
  - behind a trusted proxy, two different header addresses get separate `auth:per-ip` budgets;
  - from an untrusted connection, varying the header doesn't reset the budget;
  - events record the header's address.
- **Existing tests:** with the variables unset, behaviour is unchanged, so they pass as they are.

## Docs

`docs/deployment.md` (proxies, the examples, locking the origin to Cloudflare), `docs/api/README.md` (env vars), `docs/api/auth.md` (replace "forwarded-IP headers are ignored, so everyone behind a reverse proxy shares one IP quota" with the trusted-proxy rule, plus IPv6 /64), `.env.example`, and AGENTS.md ("do not trust forwarding headers blindly" → "only from `TRUSTED_PROXIES`").

## Decisions

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Names | `CLIENT_IP_HEADER` and `TRUSTED_PROXIES`. |
| 2 | Required together | Yes: one without the other refuses to start. |
| 3 | Headers supported | `X-Forwarded-For`, `X-Real-IP`, `CF-Connecting-IP`, `True-Client-IP`. Not the RFC 7239 `Forwarded` header, which few proxies send and which needs its own parser. |
| 4 | Reading `X-Forwarded-For` | From the right, skipping trusted proxies. |
| 5 | IPv6 limit keys | Count by /64. This applies with or without a proxy. |
| 6 | `PER_IP_LIMITS=off` | Keep it as the fallback for a proxy that can't send any client address. |
| 7 | Cloudflare's ranges | Pasted into `TRUSTED_PROXIES` by the operator and rechecked occasionally, not fetched by the app at startup (no outbound call needed just to start). |

## How the build differs

- **Built before the CAPTCHA plan.** `PER_IP_LIMITS` and the CAPTCHA `remoteip` don't exist yet, so there is no startup warning about setting both, and decision 6 waits for that plan. `clientIP(r)` is what it will use.
- **`limitSubject` is called through `rateLimit.ipKey(r)`** (`l.key(limitSubject(clientIP(r)))`), so per-IP call sites can't forget the /64.
- **`TRUSTED_PROXIES` also refuses IPv4-mapped IPv6 ranges** (`::ffff:10.0.0.0/104`): addresses are unmapped before they're compared, so such a range would silently match nothing. Single addresses are unmapped, and ranges are stored masked (`10.1.2.3/16` is `10.1.0.0/16`).
- **An empty `X-Forwarded-For` entry** (`a,,b`) counts as unparseable, so it stops at the last trusted hop too.
- **The server logs the header** and how many trusted ranges it uses when it starts.
- **`clientIP` outside `clientAddress`** (a handler tested without `Routes()`) gives the connection's address, the old behaviour.

## Not included

- The `Forwarded` header (RFC 7239), and the PROXY protocol.
- Fetching Cloudflare's IP ranges automatically.
- Trusting different headers from different proxies.
- Vite's dev proxy forwarding the address. Locally everyone is `127.0.0.1` anyway.
