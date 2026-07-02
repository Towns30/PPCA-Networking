# Environment Setup

## Language

**Go** (1.22+). We chose Go because:
- First-class concurrency (goroutines + channels)
- Excellent `net` package for low-level networking
- Simple deployment (single static binary)
- You probably haven't used it before — learning a new language is part of the point

## IDE

- **GoLand** (JetBrains) — dedicated Go IDE, free with student license
- **VS Code** + Go extension — install the `gopls` language server when prompted

## Installation

### macOS

```bash
brew install go
```

### Linux (Ubuntu/Debian)

```bash
# Option 1: apt (may be slightly old)
sudo apt install golang-go

# Option 2: official tarball (latest)
curl -LO "https://go.dev/dl/go1.22.5.linux-amd64.tar.gz"
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.22.5.linux-amd64.tar.gz
echo 'export PATH="$PATH:/usr/local/go/bin"' >> ~/.bashrc
source ~/.bashrc
```

### Windows

Download from [go.dev/dl](https://go.dev/dl/) or:
```powershell
scoop install go
```

### Verify

```bash
go version    # should print go1.22+
```

### Module proxy (recommended for faster downloads in China)

```bash
go env -w GO111MODULE=on
go env -w GOPROXY=https://goproxy.cn,direct
```

## Network Environment

For projects requiring raw sockets (ping, traceroute) or network namespaces
(QUIC CC testbed), you need a Linux environment. Options:

- **Native Linux** — ideal
- **WSL2** — works well for most projects
- **Docker** with `--cap-add=NET_RAW --cap-add=NET_ADMIN`
- **LXD/LXC containers** — good for multi-host setups

For the SOCKS5, frp, TLS MITM, and Mini Caddy projects, any OS works fine.

### Quick start with LXD

```bash
sudo snap install lxd
sudo lxd init --minimal
sudo lxc launch ubuntu:24.04 dev
sudo lxc exec dev -- bash
```

## Getting Started

1. Read the [Go Tour](https://go.dev/tour/) — focus on interfaces, goroutines, channels
2. Familiarise yourself with the `net` and `encoding/binary` packages
3. Understand basics: IPv4/IPv6, TCP/UDP, DNS, HTTP, TLS
4. Read source code of high-quality Go networking tools for idioms and patterns

## What is a client? What is a server?

**Client:** captures local network requests, wraps them in a protocol, sends to a specific server.

**Server:** accepts protocol-wrapped requests, unwraps them, makes the network request on behalf of the client.

Example: in SOCKS5, the server listens on an address:port. When a new connection arrives (connection 1), it performs the handshake, connects to the target (connection 2), then relays data between connection 1 and connection 2.
