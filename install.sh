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

detect_arch() {
    local arch
    arch=$(uname -m)
    case "$arch" in
        x86_64)           echo "linux-amd64" ;;
        aarch64|arm64)    echo "linux-arm64" ;;
        armv7l)           echo "linux-armv7" ;;
        armv6l)           echo "linux-armv6" ;;
        i386|i686)        echo "linux-386" ;;
        mips)             echo "linux-mips" ;;
        mipsel|mipsle)    echo "linux-mipsle" ;;
        mips64)           echo "linux-mips64" ;;
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

START=95
STOP=10
USE_PROCD=1

PROG="${BINARY_PATH}"

start_service() {
    [ -x "\$PROG" ] || { echo "Binary \$PROG not found"; return 1; }
    procd_open_instance
    procd_set_param command "\$PROG" -port ${PORT} -interval ${INTERVAL}
    procd_set_param pidfile /var/run/${SERVICE_NAME}.pid
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_set_param respawn
    procd_close_instance
    logger -t ${SERVICE_NAME} "Service started on port ${PORT}"
}

stop_service() {
    logger -t ${SERVICE_NAME} "Service stopped"
}

reload_service() {
    stop
    start
}
INITEOF

    chmod 755 /etc/init.d/${SERVICE_NAME}

    # Install hotplug script to restart on interface changes
    mkdir -p /etc/hotplug.d/iface
    cat > /etc/hotplug.d/iface/99-${SERVICE_NAME} << HOTPLUGEOF
#!/bin/sh
# Restart ${SERVICE_NAME} when a CAKE interface goes up or down
[ "\$ACTION" = "ifup" ] || [ "\$ACTION" = "ifdown" ] || exit 0
if tc qdisc show dev "\$INTERFACE" 2>/dev/null | grep -q "qdisc cake"; then
    logger -t "${SERVICE_NAME}" "Interface \$INTERFACE \$ACTION, restarting"
    /etc/init.d/${SERVICE_NAME} restart
fi
HOTPLUGEOF

    chmod 755 /etc/hotplug.d/iface/99-${SERVICE_NAME}

    /etc/init.d/${SERVICE_NAME} enable 2>/dev/null || true
    /etc/init.d/${SERVICE_NAME} start  2>/dev/null || true

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
