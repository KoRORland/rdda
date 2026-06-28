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
debootstrap --include=systemd,systemd-sysv,dbus,nginx,curl,jq,ca-certificates,openssl,iproute2,sudo \
  stable "$BASE" http://deb.debian.org/debian

log "build rdda and install sing-box + nfqws2 + chisel into base"
( cd "$REPO_ROOT" && go build -o "$BASE/usr/local/bin/rdda" ./cmd/rdda )
# sing-box (pinned; matches VERSION/install.sh). The base host build has egress.
SB_VER=1.13.14
curl -fsSL "https://github.com/SagerNet/sing-box/releases/download/v${SB_VER}/sing-box-${SB_VER}-linux-amd64.tar.gz" -o /tmp/sb.tgz
tar -xzf /tmp/sb.tgz -C /tmp
install -m0755 "/tmp/sing-box-${SB_VER}-linux-amd64/sing-box" "$BASE/usr/local/bin/sing-box"
# nfqws2 (zapret prebuilt; matches VERSION/install.sh). RU desync egress.
NFQWS2_VER=v72.12
curl -fsSL "https://github.com/bol-van/zapret/releases/download/${NFQWS2_VER}/zapret-${NFQWS2_VER}.tar.gz" -o /tmp/zapret.tgz
tar -xzf /tmp/zapret.tgz -C /tmp "zapret-${NFQWS2_VER}/binaries/linux-x86_64/nfqws"
install -m0755 "/tmp/zapret-${NFQWS2_VER}/binaries/linux-x86_64/nfqws" "$BASE/usr/local/bin/nfqws2"
# geoip-ru rule-set: shipped as a LOCAL .srs (the prod installer does the same),
# so the RU sing-box starts from a local file with no remote rule_set download.
# provision-ru copies it into /etc/rdda/geoip-ru.srs (the rendered RU config path).
curl -fsSL "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-ru.srs" -o "$BASE/usr/local/share/geoip-ru.srs"
# chisel (reverse-tunnel stand-in for cloudflared). Pin the version: the asset
# name is versioned, and resolving via api.github.com hits the unauthenticated
# rate limit (403) on shared CI runner IPs. Direct release-CDN download instead.
CHISEL_VER=1.11.5
curl -fsSL "https://github.com/jpillora/chisel/releases/download/v${CHISEL_VER}/chisel_${CHISEL_VER}_linux_amd64.gz" -o /tmp/chisel.gz
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
# Debian auto-enables nginx at install; disable it so it does NOT boot with the
# default :80 site. Provisioners that need nginx start it fresh with their own
# config (otherwise `enable --now` is a no-op on the already-running default).
systemctl disable nginx || true
useradd --system --no-create-home --shell /usr/sbin/nologin rdda || true
useradd --system --no-create-home --shell /usr/sbin/nologin cloudflared || true
INROOT

log "clone base to per-host rootfs"
for h in eu edge ru client target; do
  rm -rf "/var/lib/machines/rdda-$h"
  cp -a "$BASE" "/var/lib/machines/rdda-$h"
done
log "image build done"
