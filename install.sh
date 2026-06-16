#!/usr/bin/env bash
#
# rusted installer.
#
# Usage:
#   ./install.sh            # install for the current user (default)
#   ./install.sh --user     # same as above
#   ./install.sh --global   # install system-wide (uses sudo as needed)
#   ./install.sh --global --service   # also install+enable a systemd service
#
# User install:
#   binary  -> ~/.local/bin/rusted
#   config  -> ${XDG_CONFIG_HOME:-~/.config}/rusted/config.toml
#   data    -> ${XDG_DATA_HOME:-~/.local/share}/rusted   (db + backups)
#
# Global install:
#   binary  -> /usr/local/bin/rusted
#   config  -> /etc/rusted/config.toml
#   data    -> /var/lib/rusted                           (db + backups)
#
# The config file is created with a randomly generated API token and encryption
# secret on first install, and is never overwritten (so encrypted credentials
# stay readable across upgrades).

set -euo pipefail

MODE="user"
INSTALL_SERVICE=0

usage() {
    sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
    exit "${1:-0}"
}

for arg in "$@"; do
    case "$arg" in
        --user)    MODE="user" ;;
        --global)  MODE="global" ;;
        --service) INSTALL_SERVICE=1 ;;
        -h|--help) usage 0 ;;
        *) echo "unknown option: $arg" >&2; usage 1 ;;
    esac
done

# --- locate repo and tooling -------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

if ! command -v go >/dev/null 2>&1; then
    echo "error: Go toolchain not found on PATH (needed to build rusted)." >&2
    exit 1
fi
if ! command -v git >/dev/null 2>&1; then
    echo "warning: 'git' not found; rusted needs it at runtime to store backups." >&2
fi

# sudo helper: only used for global writes, and only if not already root.
SUDO=""
if [ "$MODE" = "global" ] && [ "$(id -u)" -ne 0 ]; then
    if command -v sudo >/dev/null 2>&1; then
        SUDO="sudo"
    else
        echo "error: --global requires root privileges and 'sudo' was not found." >&2
        exit 1
    fi
fi

# --- resolve target paths ----------------------------------------------------
if [ "$MODE" = "global" ]; then
    BIN_DIR="/usr/local/bin"
    CONFIG_FILE="/etc/rusted/config.toml"
    DATA_DIR="/var/lib/rusted"
else
    BIN_DIR="${XDG_BIN_HOME:-$HOME/.local/bin}"
    CONFIG_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/rusted/config.toml"
    DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/rusted"
fi
BIN_PATH="$BIN_DIR/rusted"

echo "==> Installing rusted (${MODE})"
echo "    binary:  $BIN_PATH"
echo "    config:  $CONFIG_FILE"
echo "    data:    $DATA_DIR"

# --- build -------------------------------------------------------------------
echo "==> Building..."
TMP_BIN="$(mktemp -t rusted.XXXXXX)"
trap 'rm -f "$TMP_BIN"' EXIT
CGO_ENABLED=0 go build -o "$TMP_BIN" ./cmd/rusted
echo "    built $(go version | awk '{print $3}')"

# --- install binary ----------------------------------------------------------
$SUDO mkdir -p "$BIN_DIR"
$SUDO install -m 0755 "$TMP_BIN" "$BIN_PATH"
echo "==> Installed binary."

# --- create config (only if missing) ----------------------------------------
if [ -e "$CONFIG_FILE" ]; then
    echo "==> Config already exists at $CONFIG_FILE (leaving it untouched)."
else
    echo "==> Generating config with a random API token and encryption secret..."
    if [ "$MODE" = "global" ]; then
        $SUDO "$BIN_PATH" config init --global --data-dir "$DATA_DIR"
    else
        "$BIN_PATH" config init --data-dir "$DATA_DIR"
    fi
fi

# --- initialise db + backup repo --------------------------------------------
echo "==> Initialising database and backup repository..."
$SUDO "$BIN_PATH" --config "$CONFIG_FILE" init || \
    "$BIN_PATH" --config "$CONFIG_FILE" init

# --- optional systemd service (global) --------------------------------------
if [ "$INSTALL_SERVICE" -eq 1 ]; then
    if [ "$MODE" != "global" ]; then
        echo "warning: --service is only supported with --global; skipping." >&2
    elif ! command -v systemctl >/dev/null 2>&1; then
        echo "warning: systemctl not found; skipping service install." >&2
    else
        UNIT="/etc/systemd/system/rusted.service"
        echo "==> Installing systemd service at $UNIT"
        $SUDO tee "$UNIT" >/dev/null <<EOF
[Unit]
Description=rusted network configuration backup API
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$BIN_PATH --config $CONFIG_FILE serve
Restart=on-failure
RestartSec=5
WorkingDirectory=$DATA_DIR

[Install]
WantedBy=multi-user.target
EOF
        $SUDO systemctl daemon-reload
        $SUDO systemctl enable --now rusted.service
        echo "    service enabled and started (systemctl status rusted)."
    fi
fi

# --- post-install hints ------------------------------------------------------
echo
echo "==> Done."
case ":$PATH:" in
    *":$BIN_DIR:"*) ;;
    *) echo "Note: $BIN_DIR is not on your PATH. Add it, e.g.:"
       echo "      export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac
echo "Next steps:"
echo "  rusted cred add lab -u admin -p '<password>'"
echo "  rusted device add r1 -H 10.0.0.1 -d cisco_nxos -c lab"
echo "  rusted backup run --all"
if [ "$MODE" = "global" ] && [ "$INSTALL_SERVICE" -ne 1 ]; then
    echo "  rusted serve            # or re-run with --service to run it under systemd"
fi
