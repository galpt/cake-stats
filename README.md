# cake-stats

Real-time web UI for monitoring CAKE SQM (Smart Queue Management) statistics on Linux/OpenWrt routers.

---

## Table of Contents
- [Status](#status)
- [Features](#features)
- [Requirements](#requirements)
- [Build](#build)
- [Usage](#usage)
- [Design Notes](#design-notes)
- [Limitations & Next Steps](#limitations--next-steps)
- [Contributing](#contributing)
- [License](#license)

## Status

- Stable — all parser unit tests passing, binary builds clean for 14 target platforms.

## Features

- Automatically discovers all CAKE qdiscs via `tc -s qdisc`
- Parses every CAKE field: thresh, target, interval, pk\_delay, av\_delay, sp\_delay, backlog, pkts, bytes, way\_inds, way\_miss, way\_cols, drops, marks, ack\_drop, sp\_flows, bk\_flows, un\_flows, max\_len, quantum
- Correctly handles diffserv modes: `diffserv3`, `diffserv4`, `diffserv8`, `besteffort`, `precedence`
- Two-word tier names are joined correctly (e.g. "Best Effort", "CS1 Best" etc.)
- Real-time push via **Server-Sent Events** — no WebSocket, no polling jitter
- Single static binary — no runtime dependencies; runs on OpenWrt with ≈4 MB of RAM overhead
- Web UI: dark TUI aesthetic (`#2D3C59` bg, JetBrains Mono, zero hover animations)
- Responsive for desktop and mobile (sticky first column, horizontal scroll on small screens)

## Requirements

- Linux kernel with `tc` + `sch_cake` module loaded, **or** OpenWrt with `kmod-sched-cake`
- Go 1.22+ (build only; not needed at runtime)

## Build

```bash
git clone https://github.com/galpt/cake-stats
cd cake-stats
go test ./...          # should print PASS
go build -o cake-stats .
```

Cross-compile for a MIPS OpenWrt router:
```bash
CGO_ENABLED=0 GOOS=linux GOARCH=mips GOMIPS=softfloat \
  go build -ldflags "-s -w" -o cake-stats-linux-mips .
```

Pre-built binaries for all common platforms are attached to every [GitHub Release](https://github.com/galpt/cake-stats/releases).

## Usage

### Quick start
```bash
./cake-stats                 # serves on http://0.0.0.0:11112
./cake-stats -port 8080      # custom port
./cake-stats -interval 2s    # poll tc every 2 seconds (default 1s)
./cake-stats -host 127.0.0.1 # listen only on loopback
./cake-stats -version        # print version and exit
```

Open `http://<router-ip>:11112` in a browser.

### Install on OpenWrt
```bash
sh install.sh                # auto-detects arch, downloads latest binary
sh install.sh --port 11112 --interval 1s
```

### Install on systemd Linux
```bash
sudo sh install.sh
```

### Uninstall
```bash
sh uninstall.sh              # prompts for confirmation
sh uninstall.sh --force      # no prompts
```

### API

| Endpoint | Description |
|----------|-------------|
| `GET /` | Web UI (HTML) |
| `GET /api/stats` | Current stats snapshot (JSON) |
| `GET /events` | SSE stream — emits updated JSON on every poll interval |

## Design Notes

- **Pure stdlib Go**: no external dependencies; the binary embeds `index.html` via `//go:embed`.
- **SSE over WebSocket**: server-to-client only push makes SSE sufficient and simpler; browsers handle automatic reconnection.
- **`uint64` for all counters**: avoids overflow for high-throughput links; max ~18.4 EB.
- **`sync.RWMutex`**: a single reader/writer lock separates the poller goroutine from concurrent HTTP handlers.
- **Delay fields as strings**: `pk_delay`, `av_delay`, `sp_delay` are kept as raw strings (e.g., `"6.73ms"`) so the unit suffix is preserved.

## Limitations & Next Steps

- Historical graphing (sparklines) is not yet implemented — only current snapshot is shown.
- No authentication; if exposing to the internet, put behind a reverse proxy with basic auth.
- Windows/FreeBSD builds are provided but CAKE is a Linux-only qdisc.

## Contributing

```bash
go test ./...    # run unit tests
go vet ./...     # static analysis
```

PRs welcome. Please keep external dependencies at zero.

## License

[MIT](LICENSE)
