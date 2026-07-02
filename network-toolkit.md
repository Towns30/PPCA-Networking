# Network Diagnostic Toolkit

[中文版](network-toolkit.zh.md)

> Elective project — Networking track

## Overview

Implement a progressive set of network diagnostic tools, each building on skills from the previous:

| Tool | Points | What you learn |
|------|--------|----------------|
| **ping** | 2' | Raw sockets, ICMP packet construction, checksum, RTT |
| **traceroute** | 3' | TTL manipulation, ICMP error parsing, path discovery |
| **recursive DNS resolver** | 5' | Binary protocol (RFC 1035), recursive delegation, caching |

Each sub-project is scored independently. You may complete any subset.

---

## Library Restrictions

### Forbidden

- Any DNS library (`miekg/dns`, `golang.org/x/net/dns/dnsmessage`, etc.)
- `net.Resolver`, `net.LookupHost`, `net.LookupIP`, and all `net.Lookup*`
- `golang.org/x/net/icmp` (the packet construction helpers)
- Third-party ping/traceroute/DNS libraries

### Allowed

- `net.ListenPacket`, `net.DialUDP`, `net.UDPConn`, `net.IPConn` — raw socket ops
- `golang.org/x/net/ipv4` — **only** for `SetTTL()` / `SetControlMessage()`
- `encoding/binary`, `bytes`, `strings`, `fmt`, `os`, `time`, `sync`, `context`
- CLI libraries (`flag`, `cobra`, etc.)
- Display libraries (`tablewriter`, `bubbletea`, etc.) for bonus TUI

**The point is to construct and parse packets by hand. If a library does the interesting work, you haven't learned anything.**

---

## Tier 1: Ping (2')

### What to build

```
$ sudo ./ping -c 4 google.com
PING google.com (142.250.80.46): 56 data bytes
64 bytes from 142.250.80.46: icmp_seq=0 ttl=116 time=3.42 ms
64 bytes from 142.250.80.46: icmp_seq=1 ttl=116 time=3.51 ms
64 bytes from 142.250.80.46: icmp_seq=2 ttl=116 time=3.38 ms
64 bytes from 142.250.80.46: icmp_seq=3 ttl=116 time=4.12 ms

--- google.com ping statistics ---
4 packets transmitted, 4 packets received, 0.0% packet loss
round-trip min/avg/max/stddev = 3.38/3.61/4.12/0.30 ms
```

### Must hand-write

- ICMP Echo Request construction (Type=8, Code=0, Checksum, ID, Seq, Payload)
- ICMP Echo Reply parsing
- Checksum calculation (RFC 1071 — one's complement sum)
- RTT measurement and statistics (min/avg/max/stddev, packet loss %)

### CLI

```
ping [options] <host>
  -c <count>      Packets to send (default: unlimited)
  -i <interval>   Interval in seconds (default: 1.0)
  -s <size>       Payload size in bytes (default: 56)
  -t <timeout>    Per-packet timeout in seconds (default: 2.0)
```

### Notes

- Requires root/sudo (or `setcap cap_net_raw+ep` on Linux)
- Must handle: timeout, SIGINT (print stats before exit), ICMP errors
- Use a unique ICMP Identifier (e.g. PID) to distinguish from other ping processes

---

## Tier 2: Traceroute (3')

### What to build

```
$ sudo ./traceroute google.com
traceroute to google.com (142.250.80.46), 30 hops max, 60 byte packets
 1  192.168.1.1 (192.168.1.1)  1.234 ms  1.123 ms  1.345 ms
 2  10.0.0.1 (10.0.0.1)  5.678 ms  5.432 ms  5.891 ms
 3  * * *
 ...
14  142.250.80.46 (142.250.80.46)  12.345 ms  12.123 ms  12.456 ms
```

### Must hand-write

- **UDP mode**: send UDP packets with incrementing TTL to high ports (33434+); receive ICMP Time Exceeded or Destination Unreachable
- **ICMP mode**: send ICMP Echo with incrementing TTL; receive Time Exceeded or Echo Reply
- Parse ICMP error messages and extract the **original packet header** (inner IP + 8 bytes of transport) to match responses to probes
- Concurrent probe sending (don't block sequentially per probe)

### CLI

```
traceroute [options] <host>
  -m <max_hops>    Maximum TTL (default: 30)
  -q <nqueries>    Probes per hop (default: 3)
  -w <timeout>     Timeout in seconds (default: 3.0)
  -I               Use ICMP Echo mode (default: UDP)
  -f <first_ttl>   Starting TTL (default: 1)
```

Matching ICMP error responses to outgoing probes. In UDP mode, use different destination ports per probe. The ICMP error payload contains the first 8 bytes of the original UDP header (src/dst port) — use these to correlate.

---

## Tier 3: Recursive DNS Resolver (5')

### What to build

A DNS server that resolves queries **recursively from the root** (no forwarding to 8.8.8.8).

```bash
# Terminal 1
$ ./dnsresolver -port 5353

# Terminal 2
$ dig @127.0.0.1 -p 5353 example.com A
;; ANSWER SECTION:
example.com.        86400   IN  A   93.184.216.34

$ dig @127.0.0.1 -p 5353 gmail.com MX
;; ANSWER SECTION:
gmail.com.          3600    IN  MX  5 gmail-smtp-in.l.google.com.
```

### Must hand-write

**1. DNS Wire Format (RFC 1035)**

- Header encoding/decoding (12 bytes, all flags)
- Question section (QNAME as length-prefixed labels, QTYPE, QCLASS)
- Resource Record parsing: NAME, TYPE, CLASS, TTL, RDLENGTH, RDATA
- **Name compression** — labels starting with `0xC0` are pointers; must follow pointers (and detect loops)
- RDATA parsing per type: A (4B IPv4), AAAA (16B IPv6), CNAME/NS (domain name), MX (preference + domain), SOA (all fields)

**2. Recursive Resolution**

```
resolve(name, qtype):
    nameservers = root_servers
    loop:
        query a nameserver
        if answer → return
        if NXDOMAIN → return error
        if NS referral:
            get new NS names from Authority section
            get glue IPs from Additional section
            if no glue → recursively resolve the NS name
            nameservers = new set
            continue
        if CNAME → follow the chain
```

You must handle:
- Referrals **with** glue records (Additional section has IP)
- Referrals **without** glue (must resolve the NS name recursively)
- CNAME chains (follow → resolve target, max depth guard)
- Loop detection, timeout, retry with alternate nameservers

**3. Caching**

- Cache RRs keyed by (name, type, class), respect TTL
- Negative caching (NXDOMAIN) per RFC 2308
- Concurrent-safe (`sync.RWMutex` or `sync.Map`)

**4. Server**

- Listen on configurable UDP port (default 5353)
- Handle concurrent queries (one goroutine each)
- Respond with proper DNS format (QR=1, copy ID, include question)

### CLI

```
dnsresolver [options]
  -port <port>     Listen port (default: 5353)
  -root <file>     Root hints file (default: root.hints)
  -verbose         Print resolution trace
```

### Provided

- `root.hints` file (download from internic.net or use the one in the starter repo)
- Test DNS packets (hex-encoded) for unit testing your parser
- A list of test domains covering edge cases

---

## Bonus (up to +2')

| Bonus | Points | Description |
|-------|--------|-------------|
| MTR mode | +0.5 | Continuous traceroute with live-updating display (loss%, RTT stats) |
| AS/GeoIP annotation | +0.5 | Query `origin.asn.cymru.com` via DNS TXT, show ASN per hop |
| TCP fallback (DNS) | +0.5 | Retry over TCP when response has TC=1 |
| Query coalescing | +0.5 | Deduplicate concurrent identical queries |
| EDNS0 | +0.5 | Support OPT pseudo-RR for larger UDP payload |
| Paris-traceroute | +0.5 | Fixed flow ID to avoid ECMP path changes |

Max bonus: **+2'** (choose any combination).

---

## Environment

- **Linux**: `sudo` or `setcap cap_net_raw+ep ./binary` for ping/traceroute
- **macOS**: requires `sudo` for raw ICMP sockets
- **DNS resolver**: no elevated privileges needed (just UDP on a high port)

Recommended: develop on Linux (VM, container with `--cap-add=NET_RAW`, or WSL2).
