# PPCA 2025 — Networking Track

[中文版](README.zh.md)

## Introduction

Understand the core building blocks of computer networks, learn the protocols that make the internet work, and build tools you'll actually use in daily life.

## Important Notes

* PPCA is not designed for intense competition. Follow the rules, finish the required tasks, and your grade will be fine.
* Choose a project you're genuinely interested in, not just the one that looks easiest.
* Have fun.

## Language

**Go.** See [environment-setup.md](environment-setup.md) for setup instructions.

## Structure

This year uses a **flat scoring** system: one mandatory base task + elective projects with independent point values. So pick what interests you.

---

## Required (5')

### SOCKS5 Proxy Server

Implement a simple SOCKS5 proxy server supporting `CMD CONNECT` (TCP). Authentication: `NO AUTH` (method `0x00`) only.

See [socks5.md](socks5.md) for full details.

**Deadline:** End of Week 1.

---

## Elective Projects

Choose any combination. Each project has its own point value.

---

### 1. Proxy Tool Configuration (1')

Configure a proxy tool of your choice (sing-box / xray / etc.) using a **configuration file** (not a GUI).

Requirements:
- Submit your config file with annotations explaining each field
- Must work in a real environment (demonstrate connectivity)

---

### 2. SOCKS5 UDP Support (4')

Extend your SOCKS5 server with `CMD UDP ASSOCIATE` (RFC 1928).

Requirements:
- Implement the full UDP relay flow
- NAT behaviour: Full Cone or Symmetric
- Testable with a standards-compliant SOCKS5 client

---

### 3. TLS Interception / MITM (6')

Capture and inspect HTTPS traffic, similar to `mitmproxy`.

Requirements:
- Generate trusted certificates on the fly (self-signed CA)
- Decrypt and display HTTPS request/response content
- Run as a proxy, transparently handling clients' HTTPS requests

**BONUS (+2'):** Do something interesting or useful with your MITM capability — ad filtering, request rewriting, security auditing, traffic analysis, etc. Be creative.

---

### 4. Simple frp (6')

Implement a simple [frp](https://github.com/fatedier/frp) (Fast Reverse Proxy): map a port on an intranet machine A to a port on a public machine B, allowing the internet to access A's service through B.

Requirements:
- Support both TCP and UDP forwarding
- Secure the control channel with mutual TLS (mTLS)
- Bandwidth and latency should be reasonable

**BONUS 1 (+2'):** Design a smart compression protocol tailored to the content being proxied.

**BONUS 2 (+2'):** QUIC transport layer support for the frp tunnel.

---

### 5. Network Diagnostic Toolkit

Build a progressive set of network diagnostic tools:

| Sub-project | Points | Core Learning |
|-------------|--------|---------------|
| **ping** | 2' | Raw sockets, ICMP packet construction/parsing, RTT measurement |
| **traceroute** | 3' | TTL manipulation, ICMP error messages, network path discovery |
| **recursive DNS resolver** | 5' | Binary protocol encoding/decoding, recursive resolution from root, caching |

Each sub-project is scored independently — you can complete just one or two.

**Key constraint:** You must hand-write ICMP packet construction and DNS wire format encoding/decoding. No existing DNS or ICMP libraries allowed. See [network-toolkit.md](network-toolkit.md). Starter files in [`network-toolkit/`](network-toolkit/).

---

### 6. QUIC Congestion Control (5')

Implement your own QUIC congestion-control algorithm (CUBIC / BBR / delay-based / custom) on the `apernet/quic-go` framework.

Requirements:
- Only modify `internal/cc/student.go`
- Achieve good throughput and fairness under emulated network conditions
- Submit a report analysing your algorithm's behaviour across scenarios

See [quic-cc.md](quic-cc.md). Starter code in [`quic-cc-lab/`](quic-cc-lab/).

---

### 7. Mini Caddy

Build a [Caddy](https://caddyserver.com/)-style web server from the TCP socket up.

| Component | Points | Content |
|-----------|--------|---------|
| **Core** | 6' | Hand-written HTTP/1.1 parsing & connection management, static file server, reverse proxy, virtual hosting, Caddyfile config |
| **Bonus 1: Automatic HTTPS** | +4' | ACME HTTP-01 client, tested against Pebble (the ACME test CA) |
| **Bonus 2: Middleware** | +3' | basicauth, rate limiting, gzip, access logging — composable and config-driven |
| **Bonus 3: HTTP/2** | +5' | HTTP/2 over TLS (HPACK, flow control, multiplexing) |

**Key constraint:** The server side of `net/http` and all third-party HTTP frameworks are banned. See [minicaddy.md](minicaddy.md). Testbed & sample config in [`minicaddy/`](minicaddy/).

---

### 8. Open Topic

Have another network-related project in mind? Propose it to a TA. We'll evaluate the workload and assign an appropriate point value.

---

## Scoring

- **Required (SOCKS5):** 5 points
- **Elective projects:** Points stack independently
- Code quality, commit hygiene, and real-world usability are considered during code review

## References

**Books:**

* [Beej's Guide to Network Programming](https://beej.us/guide/bgnet/)
* [High Performance Browser Networking](https://hpbn.co/) — read "Networking 101" and parts of the "HTTP" section
* [TCP/IP Tutorial and Technical Overview](https://www.redbooks.ibm.com/redbooks/pdfs/gg243376.pdf)

**RFCs:**

* [RFC 1928: SOCKS5](https://www.rfc-editor.org/rfc/rfc1928)
* [RFC 1035: DNS](https://www.rfc-editor.org/rfc/rfc1035)
* [RFC 9293: TCP](https://www.rfc-editor.org/rfc/rfc9293)
* [RFC 768: UDP](https://www.rfc-editor.org/rfc/rfc768)
* [RFC 9112: HTTP/1.1](https://www.rfc-editor.org/rfc/rfc9112.html)
* [RFC 9114: HTTP/3](https://www.rfc-editor.org/rfc/rfc9114.html)
* [RFC 8446: TLS 1.3](https://www.rfc-editor.org/rfc/rfc8446)
* [RFC 9000: QUIC](https://www.rfc-editor.org/rfc/rfc9000)
* [RFC 9002: QUIC Loss Detection and Congestion Control](https://www.rfc-editor.org/rfc/rfc9002)
* [RFC 8312: CUBIC](https://www.rfc-editor.org/rfc/rfc8312)
* [RFC 8555: ACME](https://www.rfc-editor.org/rfc/rfc8555)
* [HTTP on MDN](https://developer.mozilla.org/en-US/docs/Web/HTTP)

**Blogs & Articles:**

* Cloudflare Blog: [TLS Handshake](https://www.cloudflare.com/learning/ssl/what-happens-in-a-tls-handshake/), [TLS 1.3](https://blog.cloudflare.com/rfc-8446-aka-tls-1-3/)
* Cardwell et al., *BBR: Congestion-Based Congestion Control*, ACM Queue 2016

---

## Acknowledgments

Thanks to Alan Liang (ACM Class 2021) for laying the foundation for this project.
