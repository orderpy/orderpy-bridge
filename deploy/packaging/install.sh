#!/bin/sh
# Idempotent install: user, directories, copy payload from this tarball dir to /opt/orderpy-bridge,
# install systemd units, enable bridge + updater path watcher.
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
INSTALL=/opt/orderpy-bridge
RUNTIME=/run/orderpy-bridge
STATE=/var/lib/orderpy-bridge
ENV_DIR=/etc/orderpy-bridge
ENV_FILE="${ENV_DIR}/orderpy-bridge.env"
USER_NAME=orderpy-bridge

if [ "$(id -u)" -ne 0 ]; then
	echo "Run as root: sudo $0" >&2
	exit 1
fi

if ! command -v minisign >/dev/null 2>&1; then
	echo "minisign is required for auto-updates (apt install minisign / apk add minisign)." >&2
	exit 1
fi

if ! getent passwd "$USER_NAME" >/dev/null 2>&1; then
	useradd --system --home "$STATE" --shell /usr/sbin/nologin "$USER_NAME"
fi

mkdir -p "$INSTALL" "$STATE" "$ENV_DIR" "$RUNTIME"
chown "$USER_NAME:$USER_NAME" "$STATE"
chmod 755 "$STATE"

install -d -m 755 "$INSTALL/updater" "$INSTALL/systemd"
install -m 755 "$SCRIPT_DIR/bridge" "$INSTALL/bridge"
install -m 755 "$SCRIPT_DIR/updater/orderpy-bridge-updater.sh" "$INSTALL/updater/orderpy-bridge-updater.sh"
install -m 644 "$SCRIPT_DIR/updater/minisign.pub" "$INSTALL/updater/minisign.pub"

for f in "$SCRIPT_DIR/systemd"/*.service "$SCRIPT_DIR/systemd"/*.path; do
	[ -f "$f" ] || continue
	base=$(basename "$f")
	install -m 644 "$f" "/etc/systemd/system/$base"
	install -m 644 "$f" "$INSTALL/systemd/$base"
done

if [ ! -f "$ENV_FILE" ]; then
	install -m 640 /dev/null "$ENV_FILE"
	chown root:root "$ENV_FILE"
	cat >>"$ENV_FILE" <<'EOF'
# OrderPy Bridge (daemon)
ORDERPY_BRIDGE_VARIANT=daemon
ORDERPY_KEY_PATH=/var/lib/orderpy-bridge/bridge_key.pem
ORDERPY_CLOUD_URL=https://api.example.com
ORDERPY_ALLOWED_ORIGINS=http://localhost:5173
# ORDERPY_BRIDGE_RUNTIME_DIR=/run/orderpy-bridge
EOF
	echo "Created $ENV_FILE — edit ORDERPY_CLOUD_URL and ORDERPY_ALLOWED_ORIGINS, then: systemctl restart orderpy-bridge" >&2
fi

chown root:root "$INSTALL/bridge"
chmod 755 "$INSTALL/bridge"

systemctl daemon-reload
systemctl enable orderpy-bridge.service orderpy-bridge-updater.path
systemctl restart orderpy-bridge.service
systemctl start orderpy-bridge-updater.path

echo "orderpy-bridge installed under $INSTALL. Runtime: $RUNTIME. State: $STATE."
