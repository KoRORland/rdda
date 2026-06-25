#!/usr/bin/env bash
# Build the shared base rootfs once, then clone it per host.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=test/integration/multihost/lib.sh
. "$HERE/lib.sh"
REPO_ROOT="${1:?repo root required}"
BASE=/var/lib/machines/rdda-base
CA_DIR=/run/rdda-ca

log "debootstrap base rootfs"
rm -rf "$BASE"; mkdir -p "$BASE"
debootstrap --include=systemd,systemd-sysv,dbus,nginx,curl,jq,ca-certificates,openssl,iproute2 \
  stable "$BASE" http://deb.debian.org/debian

log "build rdda and install xray + chisel into base"
( cd "$REPO_ROOT" && go build -o "$BASE/usr/local/bin/rdda" ./cmd/rdda )
# xray-core
curl -fsSL https://github.com/XTLS/Xray-core/releases/latest/download/Xray-linux-64.zip -o /tmp/xray.zip
( cd /tmp && rm -rf xray && mkdir xray && cd xray && { jar xf /tmp/xray.zip 2>/dev/null || unzip -o /tmp/xray.zip; } )
install -m0755 /tmp/xray/xray "$BASE/usr/local/bin/xray"
# chisel (reverse-tunnel stand-in for cloudflared)
curl -fsSL https://github.com/jpillora/chisel/releases/latest/download/chisel_linux_amd64.gz -o /tmp/chisel.gz
gunzip -f /tmp/chisel.gz
install -m0755 /tmp/chisel "$BASE/usr/local/bin/chisel"

log "generate test CA"
mkdir -p "$CA_DIR"
openssl req -x509 -newkey rsa:2048 -nodes -days 2 \
  -keyout "$CA_DIR/ca.key" -out "$CA_DIR/ca.crt" -subj "/CN=RDDA Test CA" >/dev/null 2>&1
install -D -m0644 "$CA_DIR/ca.crt" "$BASE/usr/local/share/ca-certificates/rdda-test-ca.crt"

log "enable networkd + ssh-free boot; create rdda user"
systemd-nspawn -D "$BASE" --pipe /bin/bash -eus <<'INROOT'
systemctl enable systemd-networkd
update-ca-certificates
useradd --system --no-create-home --shell /usr/sbin/nologin rdda || true
useradd --system --no-create-home --shell /usr/sbin/nologin cloudflared || true
INROOT

log "clone base to per-host rootfs"
for h in eu edge ru client target; do
  rm -rf "/var/lib/machines/rdda-$h"
  cp -a "$BASE" "/var/lib/machines/rdda-$h"
done
log "image build done"
