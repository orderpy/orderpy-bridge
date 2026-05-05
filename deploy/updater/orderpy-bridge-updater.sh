#!/bin/sh
# Apply cloud-triggered bridge update: download tarball, verify sha256 + minisign,
# atomic swap under /opt/orderpy-bridge, restart service, write update-result.json.
# Intended to run as root from systemd (orderpy-bridge-updater.service).
set -eu

RUNTIME="${ORDERPY_BRIDGE_RUNTIME_DIR:-/run/orderpy-bridge}"
CACHE=/var/cache/orderpy-bridge
INSTALL=/opt/orderpy-bridge
PUB="${INSTALL}/updater/minisign.pub"
TRIGGER="${RUNTIME}/update-requested.json"
RESULT="${RUNTIME}/update-result.json"

log() { printf '%s\n' "$*" >&2; }

write_result() {
	U_STATUS="$1"
	U_TARGET="$2"
	U_ERROR="${3:-}"
	export U_STATUS U_TARGET U_ERROR
	python3 <<'PY'
import json, os
path = os.environ.get("RESULT_PATH", "/run/orderpy-bridge/update-result.json")
status = os.environ["U_STATUS"]
target = os.environ.get("U_TARGET", "")
err = os.environ.get("U_ERROR", "")
errv = None if err == "" else err
with open(path, "w") as f:
    json.dump({"status": status, "target_version": target, "error": errv}, f)
PY
}

RESULT_PATH="$RESULT"
export RESULT_PATH

validate_url() {
	_url="$1"
	printf '%s' "$_url" | grep -qE '^https://github\.com/orderpy/orderpy-bridge-go/releases/download/v[0-9]+\.[0-9]+\.[0-9]+/[A-Za-z0-9._-]+$'
}

validate_sha() {
	_sha="$1"
	printf '%s' "$_sha" | grep -qE '^[A-Fa-f0-9]{64}$'
}

if ! command -v python3 >/dev/null 2>&1; then
	log "orderpy-bridge-updater: python3 required"
	exit 1
fi
if ! command -v curl >/dev/null 2>&1; then
	log "orderpy-bridge-updater: curl required"
	exit 1
fi
if ! command -v minisign >/dev/null 2>&1; then
	log "orderpy-bridge-updater: minisign required (apt install minisign)"
	exit 1
fi
if ! command -v sha256sum >/dev/null 2>&1; then
	log "orderpy-bridge-updater: sha256sum required"
	exit 1
fi

mkdir -p "$RUNTIME" "$CACHE"

if [ ! -f "$TRIGGER" ]; then
	exit 0
fi

TMPJ="$(mktemp)"
cp "$TRIGGER" "$TMPJ"
rm -f "$TRIGGER"

BODY_FILE="$TMPJ"
export BODY_FILE
TARGET=$(python3 -c "import json, os; print(json.load(open(os.environ['BODY_FILE'])).get('target_version',''))")
TARBALL_URL=$(python3 -c "import json, os; print(json.load(open(os.environ['BODY_FILE'])).get('tarball_url',''))")
MINISIG_URL=$(python3 -c "import json, os; print(json.load(open(os.environ['BODY_FILE'])).get('minisig_url',''))")
SHA256_HEX=$(python3 -c "import json, os; print(json.load(open(os.environ['BODY_FILE'])).get('tarball_sha256',''))")
rm -f "$TMPJ"

if [ -z "$TARGET" ] || [ -z "$TARBALL_URL" ] || [ -z "$MINISIG_URL" ] || [ -z "$SHA256_HEX" ]; then
	write_result failed "" "invalid_trigger_json"
	exit 1
fi

SHA256_HEX=$(printf '%s' "$SHA256_HEX" | tr 'A-F' 'a-f')

if ! validate_url "$TARBALL_URL" || ! validate_url "$MINISIG_URL"; then
	write_result failed "$TARGET" "url_not_allowed"
	exit 1
fi
if ! validate_sha "$SHA256_HEX"; then
	write_result failed "$TARGET" "invalid_sha256"
	exit 1
fi
if [ ! -r "$PUB" ]; then
	write_result failed "$TARGET" "missing_pubkey"
	exit 1
fi

TARBALL="${CACHE}/orderpy-bridge-${TARGET}.tar.gz"
MINISIG="${CACHE}/orderpy-bridge-${TARGET}.tar.gz.minisig"
rm -f "$TARBALL" "$MINISIG"

if ! curl -fsSL --max-time 120 -o "$TARBALL" "$TARBALL_URL"; then
	write_result failed "$TARGET" "download_tarball_failed"
	exit 1
fi
if ! curl -fsSL --max-time 120 -o "$MINISIG" "$MINISIG_URL"; then
	write_result failed "$TARGET" "download_minisig_failed"
	exit 1
fi

printf '%s  %s\n' "$SHA256_HEX" "$TARBALL" | sha256sum -c - >/dev/null || {
	write_result failed "$TARGET" "sha256_mismatch"
	exit 1
}

if ! minisign -V -p "$PUB" -m "$TARBALL" -x "$MINISIG" >/dev/null 2>&1; then
	write_result failed "$TARGET" "minisign_verify_failed"
	exit 1
fi

NEWROOT="$(mktemp -d)"
cleanup() {
	rm -rf "$NEWROOT"
}
trap cleanup EXIT

if ! tar -xzf "$TARBALL" -C "$NEWROOT"; then
	write_result failed "$TARGET" "extract_failed"
	exit 1
fi

EXTRACTED="${NEWROOT}/orderpy-bridge-${TARGET}"
if [ ! -f "${EXTRACTED}/bridge" ]; then
	write_result failed "$TARGET" "missing_bridge_binary"
	exit 1
fi

OLD="${INSTALL}.old"
rm -rf "$OLD"

rollback() {
	if [ -d "$OLD" ]; then
		rm -rf "$INSTALL"
		mv "$OLD" "$INSTALL" || true
	fi
	systemctl start orderpy-bridge.service 2>/dev/null || true
}

systemctl stop orderpy-bridge.service 2>/dev/null || true

if [ -d "$INSTALL" ]; then
	if ! mv "$INSTALL" "$OLD"; then
		write_result failed "$TARGET" "backup_failed"
		systemctl start orderpy-bridge.service 2>/dev/null || true
		exit 1
	fi
fi

if ! mv "$EXTRACTED" "$INSTALL"; then
	rollback
	write_result failed "$TARGET" "install_move_failed"
	exit 1
fi

trap - EXIT
rm -rf "$NEWROOT"

chmod 755 "$INSTALL/bridge" 2>/dev/null || true
chmod +x "$INSTALL/updater/orderpy-bridge-updater.sh" 2>/dev/null || true

if [ -d "$INSTALL/systemd" ]; then
	for f in "$INSTALL/systemd"/*.service "$INSTALL/systemd"/*.path; do
		[ -f "$f" ] || continue
		install -m 644 "$f" /etc/systemd/system/
	done
	systemctl daemon-reload
	systemctl enable orderpy-bridge-updater.path 2>/dev/null || true
fi

if ! systemctl start orderpy-bridge.service; then
	rm -rf "$INSTALL"
	mv "$OLD" "$INSTALL" 2>/dev/null || true
	write_result failed "$TARGET" "service_start_failed"
	exit 1
fi

rm -rf "$OLD"

write_result success "$TARGET" ""
exit 0
