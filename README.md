# cake-stats

Real-time web UI for monitoring CAKE SQM (Smart Queue Management) statistics on Linux/OpenWrt routers.

---

<a name="table-of-contents"></a>
## Table of Contents
- [How it looks like](#how-it-looks-like)
  - [Desktop](#desktop)
  - [Mobile](#mobile)
- [Status](#status)
- [Features](#features)
- [Requirements](#requirements)
- [Design Notes](#design-notes)
- [Build](#build)
- [Usage](#usage)
- [Limitations & Next Steps](#limitations--next-steps)
- [Contributing](#contributing)
- [License](#license)

## How it looks like

### Desktop

<p align="center">
	<img src="https://github.com/galpt/cake-stats/blob/main/img/how-it-looks-like-desktop-001.png" alt="Web UI preview" style="max-width:100%;height:auto;" />
	<br/>
	<em>UI for desktop screens</em>
</p>

<p align="center">
	<img src="https://github.com/galpt/cake-stats/blob/main/img/how-it-looks-like-desktop-002.png" alt="Web UI preview" style="max-width:100%;height:auto;" />
	<br/>
	<em>Graphs for desktop screens</em>
</p>

### Mobile

<p align="center">
	<img src="https://github.com/galpt/cake-stats/blob/main/img/how-it-looks-like-mobile-001.png" alt="Web UI preview" style="max-width:100%;height:auto;" />
	<br/>
	<em>UI for mobile screens</em>
</p>

<p align="center">
	<img src="https://github.com/galpt/cake-stats/blob/main/img/how-it-looks-like-mobile-002.png" alt="Web UI preview" style="max-width:100%;height:auto;" />
	<br/>
	<em>Graphs for mobile screens</em>
</p>

[&#8593; Back to Table of Contents](#table-of-contents)

## Status

- Stable — all parser unit tests passing, binary builds clean for 14 target platforms.

[&#8593; Back to Table of Contents](#table-of-contents)

## Features

- Automatically discovers all CAKE qdiscs via `tc -s qdisc`
- Parses every CAKE field: `thresh`, `target`, `interval`, `pk_delay`, `av_delay`, `sp_delay`, `backlog`, `pkts`, `bytes`, `way_inds`, `way_miss`, `way_cols`, `drops`, `marks`, `ack_drop`, `sp_flows`, `bk_flows`, `un_flows`, `max_len`, `quantum`
- Correctly handles diffserv modes: `diffserv3`, `diffserv4`, `diffserv8`, `besteffort`, `precedence`; also parses the separate `fwmark MASK` tin-override parameter
- Two-word tier names are joined correctly (e.g. `"Best Effort"`)
- Real-time push via **Server-Sent Events** — no WebSocket, no polling jitter
- Built on Fiber v3 with zerolog for structured logs and easyjson pre-generated serializers
- Default poll interval 100ms for near-instant UI updates (adjustable via `-interval`)
- Single static binary — no runtime dependencies
- Web UI: dark TUI aesthetic (`#2D3C59` bg, JetBrains Mono, zero hover animations)
- Responsive for desktop and mobile (sticky first column, horizontal scroll on small screens)
- Per-interface **live sparklines** (TX throughput, avg latency, drops/s) with current-value labels
- Tap/click any sparkline bar to open a **full-screen history modal** with three uPlot time-series charts
- Server-side ring buffer retains history across page reloads (configurable via `-history` flag)

[&#8593; Back to Table of Contents](#table-of-contents)

## Requirements

- Linux kernel with `tc` + `sch_cake` module loaded, **or** OpenWrt with `kmod-sched-cake`
- Go 1.25+ (build only; not needed at runtime)
- Third-party libraries used during build/services:
  - [Fiber v3](https://gofiber.io/) – HTTP framework
  - [zerolog](https://github.com/rs/zerolog) – structured logging
  - [easyjson](https://github.com/mailru/easyjson) – JSON code generation
  - [rtnetlink](https://github.com/jsimonetti/rtnetlink) – optional netlink client (not currently used; included in go.mod for future event‑based polling)

[&#8593; Back to Table of Contents](#table-of-contents)

## Design Notes

- **Modular architecture**: code is split into `pkg/parser`, `pkg/history`, `pkg/server`, `pkg/log`, and `pkg/types`, with the CLI entrypoint under `cmd/cake-stats`.  This keeps the core logic reusable and simplifies testing.
- **Zero-allocation philosophy**: hot paths avoid heap allocations by using `sync.Pool` for temporary buffers, `easyjson`-generated marshalers, and pre‑computed byte slices.  Benchmark-driven optimisations ensure the 100 ms poll loop runs with minimal GC pressure.
- **Ring buffer history**: a thread-safe circular buffer stores past snapshots; clients receive both current data and historical samples after reconnects or page loads.
- **Polling strategy**: defaults to 100 ms for near-instant updates; interval is command-line configurable.  The codebase contains scaffolding and a placeholder comment for an optional rtnetlink-based watcher, but the current release still relies on regular `tc` invocations.
- **Server-Sent Events**: statistics are broadcast over SSE.  A pool of reusable message buffers reduces allocations when many clients connect.
- **Fiber & zerolog**: Fiber v3 provides a lightweight HTTP server with built‑in recovery middleware; zerolog supplies compact, structured log output.
- **Single static binary**: the project builds to one statically-linked executable, suitable for OpenWrt.
- **Testing and documentation**: parser and history packages include unit tests and benchmarks.  Dependencies are kept to a minimum to ease audits.

[&#8593; Back to Table of Contents](#table-of-contents)

## Build

```bash
git clone https://github.com/galpt/cake-stats
cd cake-stats
go test ./...          # should print PASS
# regenerate any easyjson helpers (optional)
go generate ./...
go build -o cake-stats .
```

Cross-compile for a MIPS OpenWrt router:
```bash
CGO_ENABLED=0 GOOS=linux GOARCH=mips GOMIPS=softfloat \
  go build -ldflags "-s -w" -o cake-stats-linux-mips .
```

Pre-built binaries for all common platforms are attached to every [GitHub Release](https://github.com/galpt/cake-stats/releases).

[&#8593; Back to Table of Contents](#table-of-contents)

## Usage

### Quick start
```bash
./cake-stats                 # serves on http://0.0.0.0:11112
./cake-stats -port 8080      # custom port
./cake-stats -interval 2s    # poll tc every 2 seconds (default 100ms)
./cake-stats -history 3600   # retain 1 hour of history (default 300 = 5 min)
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
| `GET /api/history` | Full ring-buffer history per interface (JSON), used to seed sparklines on page load |
| `GET /events` | SSE stream — emits updated JSON on every poll interval |

[&#8593; Back to Table of Contents](#table-of-contents)

## Limitations & Next Steps

- Still polls using `tc`; a kernel‑level rtnetlink watcher is included as an option but not yet the default.
- No built‑in authentication or HTTPS; expose only on trusted networks or pair with a reverse proxy.
- UI is intentionally minimal – theme support, additional charts, and accessibility tweaks are on the roadmap.
- RAM footprint may vary; the few‑megabyte figure above is RSS (resident set size) measured with tools such as `top`/`ps`.  Depending on kernel malloc behaviour, architecture and how many clients are connected the value can be anywhere from about 4 MB up to a dozen megabytes.
- Cross‑platform builds are produced, but CAKE itself is Linux‑only; Windows/FreeBSD binaries do not collect real data.
- Future enhancements: persistent history storage, per‑flow details, OpenWrt package, and support for other qdiscs.

[&#8593; Back to Table of Contents](#table-of-contents)

## Contributing

```bash
go test ./...    # run unit tests
go vet ./...     # static analysis
```

PRs welcome. Please keep external dependencies at zero.

[&#8593; Back to Table of Contents](#table-of-contents)

## License

[MIT](LICENSE)

[&#8593; Back to Table of Contents](#table-of-contents)
