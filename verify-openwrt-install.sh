#!/bin/sh
# Verify that cake-stats is correctly installed and enabled on OpenWrt.

set -u

SERVICE_NAME="cake-stats"
INIT_SCRIPT="/etc/init.d/${SERVICE_NAME}"
BINARY_PATH="/usr/bin/${SERVICE_NAME}"
LOG_LINES=20

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { printf "${BLUE}[INFO]${NC} %s\n" "$1"; }
log_ok() { printf "${GREEN}[OK]${NC}   %s\n" "$1"; }
log_warn() { printf "${YELLOW}[WARN]${NC} %s\n" "$1"; }
log_error() { printf "${RED}[ERR]${NC}  %s\n" "$1"; }

while [ "$#" -gt 0 ]; do
	case "$1" in
		--log-lines)
			LOG_LINES="$2"
			shift 2
			;;
		--help|-h)
			echo "Usage: sh $0 [--log-lines N]"
			echo "  --log-lines N   Number of recent cake-stats log lines to display (default: 20)"
			exit 0
			;;
		*)
			echo "Unknown option: $1" >&2
			exit 1
			;;
	esac
done

failures=0

check_ok() {
	if "$@"; then
		return 0
	fi
	failures=$((failures + 1))
	return 1
}

if [ ! -f /etc/openwrt_release ]; then
	log_error "This verification helper must be run on an OpenWrt router"
	exit 1
fi

log_info "Verifying ${SERVICE_NAME} installation on OpenWrt"

if check_ok test -x "$BINARY_PATH"; then
	log_ok "Binary present: $BINARY_PATH"
else
	log_error "Binary missing or not executable: $BINARY_PATH"
fi

if check_ok test -x "$INIT_SCRIPT"; then
	log_ok "Init script present: $INIT_SCRIPT"
else
	log_error "Init script missing or not executable: $INIT_SCRIPT"
fi

START_VALUE=""
if [ -r "$INIT_SCRIPT" ]; then
	START_VALUE=$(awk -F= '/^START=/{gsub(/[[:space:]]/, "", $2); print $2; exit}' "$INIT_SCRIPT")
fi

if [ -n "$START_VALUE" ]; then
	RC_LINK="/etc/rc.d/S${START_VALUE}${SERVICE_NAME}"
	if check_ok test -L "$RC_LINK"; then
		TARGET=$(readlink "$RC_LINK" 2>/dev/null || true)
		log_ok "Autostart symlink present: $RC_LINK -> ${TARGET:-unknown}"
	else
		log_error "Autostart symlink missing: $RC_LINK"
	fi
else
	failures=$((failures + 1))
	log_error "Could not determine START value from $INIT_SCRIPT"
fi

if "$INIT_SCRIPT" enabled >/dev/null 2>&1; then
	log_ok "Service is enabled for reboot"
else
	failures=$((failures + 1))
	log_error "Service is not enabled for reboot"
fi

STATUS_OUTPUT=$($INIT_SCRIPT status 2>&1 || true)
if "$INIT_SCRIPT" running >/dev/null 2>&1; then
	log_ok "Service is currently running"
else
	failures=$((failures + 1))
	log_error "Service is not currently running"
	if [ -n "$STATUS_OUTPUT" ]; then
		printf "%s\n" "$STATUS_OUTPUT"
	fi
fi

if command -v logread >/dev/null 2>&1; then
	log_info "Recent ${SERVICE_NAME} log lines"
	LOG_OUTPUT=$(logread 2>/dev/null | grep "$SERVICE_NAME" | tail -n "$LOG_LINES" || true)
	if [ -n "$LOG_OUTPUT" ]; then
		printf "%s\n" "$LOG_OUTPUT"
	else
		log_warn "No recent logread entries found for ${SERVICE_NAME}"
	fi
else
	log_warn "logread not available; skipping log inspection"
fi

if [ "$failures" -eq 0 ]; then
	log_ok "Verification passed"
	exit 0
fi

log_error "Verification failed with ${failures} problem(s)"
	exit 1