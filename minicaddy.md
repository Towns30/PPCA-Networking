# Mini Caddy — Build a Web Server from the Socket Up

[中文版](minicaddy.zh.md)

> Elective project (6' core + up to 12' bonus) — Networking track

## Motivation

So far you've lived on the client/middle side: SOCKS5, MITM, DNS hijacking. This
project flips the roles — **you are the origin server.** A request arrives on a
raw TCP socket and you are the code that turns those bytes into a response.

You will build **minicaddy**, a [Caddy](https://caddyserver.com/)-flavoured web
server: static files, reverse proxy, virtual hosting, middleware, and — the
signature Caddy feature — automatic HTTPS via ACME.

## The Red Line

The whole point is the part `net/http` would otherwise do for you. Therefore:

**BANNED:**
- Server-side `net/http`: `http.Server`, `http.ListenAndServe`, `http.Handler`,
  `http.FileServer`, `httputil.ReverseProxy`, `http.ReadRequest`, `http.ReadResponse`
- Any third-party HTTP server/router/ACME library (`fasthttp`, `gin`, `chi`,
  `echo`, `golang.org/x/crypto/acme`, `certmagic`, etc.)

**ALLOWED:**
- `net`, `crypto/tls`, `bufio`, `io`, `net/url`, `compress/gzip`, `encoding/*`
- `net/http` **client** (`http.Client`, `http.NewRequest`) — **only** for outbound
  ACME REST calls to the CA
- `http.Header` as a data structure (but you do the parsing yourself)

Violation = automatic zero on the affected component.

## Core (6')

### 1. HTTP/1.1 Engine
- Hand-written request parser over `bufio.Reader`
- Response framing: `Content-Length` or **chunked** transfer-encoding
- **Keep-alive**: reuse connections, drain unread bodies, idle timeout
- Decode chunked request bodies

### 2. Static File Server
- Directory index resolution; MIME type by extension
- `ETag` + `Last-Modified`, conditional `304`
- Single **Range** request → `206 Partial Content`
- Path traversal protection

### 3. Reverse Proxy
- Forward to upstream, streaming both bodies
- Strip hop-by-hop headers; add `X-Forwarded-For`/`X-Forwarded-Proto`
- `502` on upstream failure

### 4. Virtual Hosting / Routing
- Route by `Host` header to the right site
- If you want tls, probably you also need to look at sni

### 5. Configuration
- Parse a Caddyfile-style config to drive all features

## Bonus 1: Automatic HTTPS (+4')

Implement an **ACME v2 (RFC 8555)** client using **HTTP-01** challenge:

- JWS-signed account registration
- New order → publish key authorization at `/.well-known/acme-challenge/<token>`
  (served by **your** HTTP stack)
- Poll to `valid`, finalize CSR, install certificate into TLS listener (by SNI)
- Test against [Pebble](https://github.com/letsencrypt/pebble) — no real domain needed

## Bonus 2: Middleware (+3')

Composable, config-driven middleware:

- **basic auth** — `401` + `WWW-Authenticate`, constant-time compare
- **rate limiting** — token bucket, per client IP
- **gzip** — respect `Accept-Encoding`, set `Vary`
- **access logging** — request line, status, duration

## Bonus 3: HTTP/2 (+5')

Full HTTP/2 over TLS:
- HPACK header compression
- Stream multiplexing and flow control
- Server push (optional)
- ALPN negotiation (`h2` / `http/1.1`)

## Testing

A conformance script (`testbed/conformance.sh`) drives the server with `curl`/`nc`
and checks core behaviours. Use **real Caddy as an oracle**: run the same request
against Caddy and diff the response.

For ACME: run Pebble locally:
```bash
ACME_DIRECTORY=https://localhost:14000/dir ACME_INSECURE=1 ./minicaddy -config Caddyfile
```

## Deliverables

- Source that builds with `go build ./...`
- `Caddyfile` demonstrating every feature you implemented
- `testbed/conformance.sh` passing for your core features
- `REPORT.md` (2–4 pages): architecture, framing design, one throughput number vs
  real Caddy, what you have implemented & not

## Grading Breakdown

| Component | Pts |
|-----------|----:|
| Build & integrity (no banned imports) | 1 |
| HTTP/1.1 engine (parser, framing, keep-alive) | 2 |
| Static file server (MIME, conditional, Range) | 1.5 |
| Reverse proxy (streaming, hop-by-hop, 502) | 1.5 |
| Bonus: ACME HTTP-01 (full flow against Pebble) | +4 |
| Bonus: Middleware (composable, config-driven) | +3 |
| Bonus: HTTP/2 (HPACK, streams, flow control) | +5 |

## References

- [RFC 9112: HTTP/1.1 Message Syntax](https://www.rfc-editor.org/rfc/rfc9112.html)
- [RFC 9113: HTTP/2](https://www.rfc-editor.org/rfc/rfc9113.html)
- [RFC 8555: ACME](https://www.rfc-editor.org/rfc/rfc8555)
- [Caddy documentation](https://caddyserver.com/docs/)
- [Pebble (ACME test CA)](https://github.com/letsencrypt/pebble)
