#!/usr/bin/env bash
#
# Build a rusted .deb for one architecture.
#
#   VERSION=1.2.3 ARCH=amd64 packaging/build-deb.sh   -> dist/rusted_1.2.3_amd64.deb
#
# The binary is static (CGO disabled), so the package needs no shared libraries -
# only git at runtime (to version backups in a repo) and ca-certificates. Installs
# a systemd service and, on first install, generates /etc/rusted/config.toml with a
# random API token + encryption secret (never regenerated, so encrypted device
# credentials stay readable across upgrades).
set -euo pipefail

VERSION="${VERSION:-0.0.0-dev}"
ARCH="${ARCH:-amd64}"

case "$ARCH" in
    amd64) GOARCH=amd64 ;;
    arm64) GOARCH=arm64 ;;
    *) echo "unsupported ARCH: $ARCH (use amd64 or arm64)" >&2; exit 1 ;;
esac

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

echo "==> Building static rusted ${VERSION} (${ARCH})"
mkdir -p "$STAGE/usr/bin" "$STAGE/lib/systemd/system" "$STAGE/DEBIAN"
CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "$STAGE/usr/bin/rusted" ./cmd/rusted

install -m 0644 packaging/rusted.service "$STAGE/lib/systemd/system/rusted.service"

INSTALLED_KB="$(du -ks "$STAGE/usr" "$STAGE/lib" | awk '{s+=$1} END {print s}')"

cat > "$STAGE/DEBIAN/control" <<EOF
Package: rusted
Version: ${VERSION}
Section: net
Priority: optional
Architecture: ${ARCH}
Depends: git, ca-certificates
Maintainer: Athena Networks <josh@athenanetworks.com.au>
Homepage: https://github.com/JoshFinlayAU/rusted
Installed-Size: ${INSTALLED_KB}
Description: rusted network configuration backup engine
 Backs up network device configurations over SSH (Cisco, MikroTik, Cambium and
 more), versions them in a git repository, and serves a small HTTP API. Runs as a
 systemd service; My Mate talks to it for config backups.
EOF

# Bootstrap on install: create the dirs, generate the system config once (random
# token + secret), initialise the db + backup repo, then enable the service.
cat > "$STAGE/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
case "$1" in
  configure)
    mkdir -p /etc/rusted /var/lib/rusted
    if [ ! -e /etc/rusted/config.toml ]; then
        rusted config init --global --data-dir /var/lib/rusted >/dev/null
    fi
    rusted --config /etc/rusted/config.toml init >/dev/null || true
    if [ -d /run/systemd/system ]; then
        systemctl daemon-reload || true
        systemctl enable rusted.service >/dev/null 2>&1 || true
        systemctl restart rusted.service || true
    fi
    ;;
esac
exit 0
EOF
chmod 0755 "$STAGE/DEBIAN/postinst"

# Stop + disable the service before the files go away. Config and backups under
# /etc/rusted and /var/lib/rusted are deliberately left in place.
cat > "$STAGE/DEBIAN/prerm" <<'EOF'
#!/bin/sh
set -e
if [ "$1" = "remove" ] || [ "$1" = "purge" ]; then
    if [ -d /run/systemd/system ]; then
        systemctl stop rusted.service || true
        systemctl disable rusted.service >/dev/null 2>&1 || true
    fi
fi
exit 0
EOF
chmod 0755 "$STAGE/DEBIAN/prerm"

mkdir -p dist
OUT="dist/rusted_${VERSION}_${ARCH}.deb"
dpkg-deb --build --root-owner-group "$STAGE" "$OUT"
echo "==> Built $OUT"
