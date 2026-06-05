#!/bin/sh
# cake-stats installation script
# Supports OpenWrt (procd/init.d), systemd Linux distros, and bare Linux.
# Usage: sh install.sh [--port PORT] [--interval INTERVAL]
#
# Requirements: tc (iproute2), wget or curl

set -e

VERSION="1.0.0"
BINARY_NAME="cake-stats"
SERVICE_NAME="cake-stats"
BINARY_PATH="/usr/bin/${BINARY_NAME}"
REPO_URL="https://github.com/galpt/cake-stats"
DEFAULT_PORT="11112"
DEFAULT_INTERVAL="1s"

# Allow environment overrides
PORT="${CAKE_STATS_PORT:-$DEFAULT_PORT}"
INTERVAL="${CAKE_STATS_INTERVAL:-$DEFAULT_INTERVAL}"

# Parse CLI flags
while [ "$#" -gt 0 ]; do
    case "$1" in
        --port) PORT="$2"; shift 2 ;;
        --interval) INTERVAL="$2"; shift 2 ;;
        --help|-h)
            echo "Usage: $0 [--port PORT] [--interval INTERVAL]"
            echo "  --port PORT          Web UI port (default: $DEFAULT_PORT)"
            echo "  --interval INTERVAL  Poll interval e.g. 1s, 500ms (default: $DEFAULT_INTERVAL)"
            exit 0
            ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

# ---- Colour helpers -------------------------------------------------------

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info()    { printf "${BLUE}[INFO]${NC} %s\n" "$1"; }
log_ok()      { printf "${GREEN}[OK]${NC}   %s\n" "$1"; }
log_warn()    { printf "${YELLOW}[WARN]${NC} %s\n" "$1"; }
log_error()   { printf "${RED}[ERROR]${NC} %s\n" "$1"; }

# ---- Pre-flight checks ----------------------------------------------------

check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        log_error "Please run as root: sudo $0"
        exit 1
    fi
}

check_deps() {
    if ! command -v tc > /dev/null 2>&1 && \
       ! [ -x /sbin/tc ] && \
       ! [ -x /usr/sbin/tc ]; then
        log_error "'tc' (iproute2) is required but not found."
        exit 1
    fi
    if ! command -v wget        > /dev/null 2>&1 && \
       ! command -v curl        > /dev/null 2>&1 && \
       ! command -v uclient-fetch > /dev/null 2>&1; then
        log_error "A download tool is required (wget, curl, or uclient-fetch) but none were found."
        exit 1
    fi
    log_ok "Dependencies OK"
}

# ---- Architecture detection -----------------------------------------------

# Detect whether a "mips" or "mips64" system is little-endian.
# Linux's uname -m always reports "mips" regardless of endianness, so we
# need alternative signals.  Prints "le" for little-endian, or nothing (empty
# string) for big-endian, matching the cake-stats release asset naming
# convention (linux-mips vs linux-mipsle).
detect_mips_endianness() {
    # Layer 1: OpenWrt release file (covers >99% of MIPS router installations)
    if [ -f /etc/openwrt_release ]; then
        # DISTRIB_ARCH encodes endianness: mipsel_*, mips64el_* = little-endian
        local dist_arch
        dist_arch=$(grep '^DISTRIB_ARCH=' /etc/openwrt_release 2>/dev/null | head -1 | cut -d"'" -f2)
        case "$dist_arch" in
            mipsel*|mips64el*) echo "le"; return ;;
            mips*|mips64*)     return ;;   # big-endian; print nothing
        esac
    fi

    # Layer 2: /proc/cpuinfo — some MIPS kernels expose a "byte order" field
    if [ -r /proc/cpuinfo ]; then
        local order
        order=$(grep -i 'byte order' /proc/cpuinfo 2>/dev/null | head -1 | tr '[:upper:]' '[:lower:]')
        case "$order" in
            *little*) echo "le"; return ;;
            *big*)    return ;;
        esac
    fi

    # Layer 3: readelf on a known ELF binary — authoritative when available
    if command -v readelf >/dev/null 2>&1 && [ -r /bin/sh ]; then
        if readelf -h /bin/sh 2>/dev/null | grep -qi 'little endian'; then
            echo "le"; return
        elif readelf -h /bin/sh 2>/dev/null | grep -qi 'big endian'; then
            return
        fi
    fi

    # Layer 4: fallback — default to big-endian (historical convention).
    # Prints nothing, so the caller gets "linux-mips" / "linux-mips64".
}

detect_arch() {
    local arch suffix
    arch=$(uname -m)
    case "$arch" in
        x86_64)           echo "linux-amd64" ;;
        aarch64|arm64)    echo "linux-arm64" ;;
        armv7l)           echo "linux-armv7" ;;
        armv6l)           echo "linux-armv6" ;;
        i386|i686)        echo "linux-386" ;;
        mips)
            suffix=$(detect_mips_endianness)
            echo "linux-mips${suffix}" ;;
        mipsel|mipsle)    echo "linux-mipsle" ;;
        mips64)
            suffix=$(detect_mips_endianness)
            echo "linux-mips64${suffix}" ;;
        mips64el|mips64le)echo "linux-mips64le" ;;
        *)
            log_error "Unsupported architecture: $arch"
            exit 1
            ;;
    esac
}

# ---- OS detection ---------------------------------------------------------

detect_os() {
    if [ -f /etc/openwrt_release ]; then
        echo "openwrt"
    elif command -v systemctl > /dev/null 2>&1; then
        echo "systemd"
    else
        echo "linux"
    fi
}

# ---- Download & install binary --------------------------------------------

download_binary() {
    local arch name url tmp_dir
    arch=$(detect_arch)
    name="${BINARY_NAME}-${arch}"
    url="${REPO_URL}/releases/latest/download/${name}.tar.gz"

    log_info "Downloading ${name} from GitHub Releases..."

    tmp_dir=$(mktemp -d)
    # shellcheck disable=SC2064
    trap "rm -rf '$tmp_dir'" EXIT INT TERM

    if command -v wget > /dev/null 2>&1; then
        wget -q --show-progress "$url" -O "${tmp_dir}/${name}.tar.gz" || {
            log_error "Download failed: $url"
            exit 1
        }
    elif command -v curl > /dev/null 2>&1; then
        curl -fSL "$url" -o "${tmp_dir}/${name}.tar.gz" || {
            log_error "Download failed: $url"
            exit 1
        }
    else
        # uclient-fetch is the built-in downloader on OpenWrt builds that ship
        # without wget or curl (e.g. apk-based OpenWrt 25.x on the N5105).
        uclient-fetch -O "${tmp_dir}/${name}.tar.gz" "$url" || {
            log_error "Download failed: $url"
            exit 1
        }
    fi

    tar -xzf "${tmp_dir}/${name}.tar.gz" -C "$tmp_dir" || {
        log_error "Failed to extract archive."
        exit 1
    }

    cp "${tmp_dir}/${name}" "$BINARY_PATH" && chmod 755 "$BINARY_PATH" || {
        log_error "Failed to install binary to $BINARY_PATH"
        exit 1
    }

    log_ok "Binary installed: $BINARY_PATH"
}

# ---- OpenWrt service setup -----------------------------------------------

install_openwrt_service() {
    log_info "Installing OpenWrt init.d service..."

    cat > /etc/init.d/${SERVICE_NAME} << INITEOF
#!/bin/sh /etc/rc.common
# ${SERVICE_NAME} - real-time CAKE SQM statistics web dashboard

START=99
STOP=10
USE_PROCD=1

PROG="${BINARY_PATH}"
HOST="\${CAKE_STATS_HOST:-0.0.0.0}"
PORT="\${CAKE_STATS_PORT:-${PORT}}"
INTERVAL="\${CAKE_STATS_INTERVAL:-${INTERVAL}}"

start_service() {
    logger -t ${SERVICE_NAME} "Registering procd instance on \$HOST:\$PORT (interval \$INTERVAL)"

	procd_open_instance
    procd_set_param command /bin/sh -c '
        if [ ! -x "\$1" ]; then
            logger -t ${SERVICE_NAME} "binary \$1 not found at startup, waiting"
            until [ -x "\$1" ]; do
                sleep 1
            done
        fi
        logger -t ${SERVICE_NAME} "Launching on \$2:\$3 (interval \$4)"
        exec "\$1" -host "\$2" -port "\$3" -interval "\$4"
    ' sh "\$PROG" "\$HOST" "\$PORT" "\$INTERVAL"
	procd_set_param pidfile /var/run/${SERVICE_NAME}.pid
	procd_set_param stdout 1
	procd_set_param stderr 1
	procd_set_param respawn 3600 5 5
	procd_close_instance
}

stop_service() {
	logger -t ${SERVICE_NAME} "Stopping"
}

reload_service() {
	stop
	start
}

service_triggers() {
	procd_add_reload_trigger "${SERVICE_NAME}"
}
INITEOF

    chmod 755 /etc/init.d/${SERVICE_NAME}

    # Install hotplug script to start after boot and restart on CAKE changes
    mkdir -p /etc/hotplug.d/iface
    cat > /etc/hotplug.d/iface/99-${SERVICE_NAME} << HOTPLUGEOF
#!/bin/sh
# Start ${SERVICE_NAME} after boot if netifd finishes after rc.d boot, and
# restart it when a CAKE interface changes.
[ "\$ACTION" = "ifup" ] || [ "\$ACTION" = "ifupdate" ] || [ "\$ACTION" = "ifdown" ] || exit 0
SERVICE="/etc/init.d/${SERVICE_NAME}"

service_enabled() {
    "\$SERVICE" enabled >/dev/null 2>&1
}

service_running() {
    "\$SERVICE" running >/dev/null 2>&1
}

if [ "\$ACTION" = "ifup" ] || [ "\$ACTION" = "ifupdate" ]; then
    if service_enabled && ! service_running; then
        logger -t "${SERVICE_NAME}" "Interface \$INTERFACE \$ACTION detected, starting fallback service launch"
        "\$SERVICE" start >/dev/null 2>&1 || true
        exit 0
    fi
fi

if tc qdisc show dev "\$INTERFACE" 2>/dev/null | grep -q "qdisc cake"; then
    logger -t "${SERVICE_NAME}" "Interface \$INTERFACE \$ACTION, restarting"
    "\$SERVICE" restart >/dev/null 2>&1 || true
fi
HOTPLUGEOF

    chmod 755 /etc/hotplug.d/iface/99-${SERVICE_NAME}

    if ! /etc/init.d/${SERVICE_NAME} enable; then
        log_error "Failed to enable OpenWrt service"
        exit 1
    fi

    if ! /etc/init.d/${SERVICE_NAME} start; then
        log_error "Failed to start OpenWrt service"
        exit 1
    fi

    if ! /etc/init.d/${SERVICE_NAME} enabled >/dev/null 2>&1; then
        log_error "OpenWrt service was started but is not enabled for reboot"
        exit 1
    fi

    log_ok "OpenWrt service enabled and started"
}

# ---- systemd service setup -----------------------------------------------

install_systemd_service() {
    log_info "Installing systemd service..."

    cat > /etc/systemd/system/${SERVICE_NAME}.service << SVCEOF
[Unit]
Description=cake-stats - real-time CAKE SQM statistics dashboard
After=network.target
Wants=network.target

[Service]
Type=simple
ExecStart=${BINARY_PATH} -port ${PORT} -interval ${INTERVAL}
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${SERVICE_NAME}

[Install]
WantedBy=multi-user.target
SVCEOF

    systemctl daemon-reload
    systemctl enable "${SERVICE_NAME}" 2>/dev/null || true
    systemctl start  "${SERVICE_NAME}"

    log_ok "systemd service enabled and started"
}

# ---- Bare Linux (fallback) -----------------------------------------------

install_bare_service() {
    log_warn "No service manager detected. The binary is installed at ${BINARY_PATH}."
    log_info "Run manually: ${BINARY_PATH} -port ${PORT} -interval ${INTERVAL}"
}

# ---- Main -----------------------------------------------------------------

main() {
    log_info "cake-stats installer v${VERSION}"
    check_root
    check_deps

    download_binary

    local os
    os=$(detect_os)
    log_info "Detected OS type: ${os}"

    case "$os" in
        openwrt)  install_openwrt_service ;;
        systemd)  install_systemd_service ;;
        linux)    install_bare_service ;;
    esac

    echo ""
    log_ok "Installation complete!"
    log_info "Web interface: http://<device-ip>:${PORT}"
    log_info "API endpoint:  http://<device-ip>:${PORT}/api/stats"
    echo ""
}

main "$@"
