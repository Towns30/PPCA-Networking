# SOCKS5 Proxy Server

[中文版](socks5.zh.md)

## Requirements

Implement a simple SOCKS5 proxy server (RFC 1928).

**Must support:**
- Method negotiation: `NO AUTHENTICATION REQUIRED` (method `0x00`)
- `CMD CONNECT`: establish a TCP connection and relay data between client and target
- Address types: IPv4 (`0x01`), domain name (`0x03`), IPv6 (`0x04`)

**Not required:**
- `CMD BIND` or `CMD UDP ASSOCIATE` (UDP is a separate elective)
- Username/password authentication (method `0x02`)

## Testing

Use [Proxy SwitchyOmega](https://chrome.google.com/webstore/detail/proxy-switchyomega/padekgcemlokbadohgkifijomclgjgif) to set your browser's proxy, then browse normally.

```bash
./socks5-server -port 1080

curl -x socks5h://127.0.0.1:1080 http://example.com
curl -x socks5h://127.0.0.1:1080 https://www.google.com
curl -x socks5h://127.0.0.1:1080 http://ipv6.google.com
```

## Deadline

End of Week 1.

## Grading (5')

| Criterion | Points |
|-----------|--------|
| Correct protocol handshake (method negotiation + connect request/reply) | 1.5 |
| TCP CONNECT works (can proxy HTTP and HTTPS) | 2.0 |
| All address types supported (IPv4 / domain / IPv6) | 0.5 |
| Concurrent connections (goroutine per connection) | 0.5 |
| Error handling and code quality | 0.5 |

## Reference

- [RFC 1928: SOCKS Protocol Version 5](https://www.rfc-editor.org/rfc/rfc1928)
