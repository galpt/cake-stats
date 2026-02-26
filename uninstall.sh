#!/bin/sh
# cake-stats uninstallation script
# Cleanly removes cake-stats from OpenWrt, systemd, and bare Linux systems.
# Usage: sh uninstall.sh [--force]

set -e

BINARY_NAME="cake-stats"
SERVICE_NAME="cake-stats"
BINARY_PATH="/usr/bin/${BINARY_NAME}"

FORCE="${FORCE_UNINSTALL:-}"
while [ "$#" -gt 0 ]; do
    case "$1" in
        --force) FORCE="true"; shift ;;
        --help|-h)
            echo "Usage: $0 [--force]"
            echo "  --force   Skip confirmation prompts"
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

log_info()  { printf "${BLUE}[INFO]${NC} %s\n" "$1"; }
log_ok()    { printf "${GREEN}[OK]${NC}   %s\n" "$1"; }
log_warn()  { printf "${YELLOW}[WARN]${NC} %s\n" "$1"; }
log_error() { printf "${RED}[ERROR]${NC} %s\n" "$1"; }

# ---- Pre-flight checks ----------------------------------------------------

check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        log_error "Please run as root: sudo $0"
        exit 1
    fi
}

confirm() {
    local msg="$1"
    if [ "$FORCE" = "true" ]; then
        return 0
    fi
    printf "%s [y/N]: " "$msg"
    read -r answer
    [ "$answer" = "y" ] || [ "$answer" = "Y" ]
}

# ---- Service stop & removal -----------------------------------------------

stop_openwrt_service() {
    if [ -f "/etc/init.d/${SERVICE_NAME}" ]; then
        log_info "Stopping OpenWrt service..."
        /etc/init.d/${SERVICE_NAME} stop    2>/dev/null || true
        /etc/init.d/${SERVICE_NAME} disable 2>/dev/null || true
        return 0
    fi
    return 1
}

stop_systemd_service() {
    if command -v systemctl > /dev/null 2>&1 && \
       [ -f "/etc/systemd/system/${SERVICE_NAME}.service" ]; then
        log_info "Stopping systemd service..."
        systemctl stop    "${SERVICE_NAME}" 2>/dev/null || true
        systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
        systemctl daemon-reload             2>/dev/null || true
        return 0
    fi
    return 1
}

stop_service() {
    stop_openwrt_service || stop_systemd_service || log_warn "Service not found; skipping stop."
}

# ---- File removal ---------------------------------------------------------

remove_files() {
    local removed=0

    remove_one() {
        if [ -f "$1" ]; then
            log_info "Removing $1"
            rm -f "$1"
            removed=$((removed + 1))
        fi
    }

    remove_one "$BINARY_PATH"
    remove_one "/etc/init.d/${SERVICE_NAME}"
    remove_one "/etc/hotplug.d/iface/99-${SERVICE_NAME}"
    remove_one "/etc/systemd/system/${SERVICE_NAME}.service"
    remove_one "/var/run/${SERVICE_NAME}.pid"

    if [ "$removed" -eq 0 ]; then
        log_warn "No files found to remove."
    fi
}

# ---- Main -----------------------------------------------------------------

main() {
    log_info "cake-stats uninstaller"
    check_root

    if ! confirm "This will stop and remove cake-stats. Continue?"; then
        log_info "Aborted."
        exit 0
    fi

    stop_service
    remove_files

    # Reload systemd if applicable
    if command -v systemctl > /dev/null 2>&1; then
        systemctl daemon-reload 2>/dev/null || true
    fi

    echo ""
    log_ok "cake-stats has been removed."
}

main "$@"
