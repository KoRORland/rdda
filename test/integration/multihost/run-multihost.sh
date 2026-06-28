#!/usr/bin/env bash
# Top-level orchestrator for the multi-host nspawn integration harness.
# Must run as root on a host with systemd-nspawn, debootstrap, nft, and Go.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../../.." && pwd)"
# shellcheck source=test/integration/multihost/lib.sh
. "$HERE/lib.sh"

HOSTS=(eu edge ru client target)

[ "$(id -u)" -eq 0 ] || die "must run as root"
for bin in systemd-nspawn machinectl debootstrap nft systemd-run go; do
  command -v "$bin" >/dev/null || die "$bin not installed"
done

PASSED=0
diag() {
  log "===== DIAG (harness did not pass) ====="
  nft list ruleset 2>&1 | sed 's/^/[nft] /' || true
  for h in edge eu ru; do
    log "--- $h: listeners + addrs ---"
    systemd-run --machine="rdda-$h" --wait --pipe --quiet ss -ltnp 2>&1 | sed "s/^/[$h] /" || true
    systemd-run --machine="rdda-$h" --wait --pipe --quiet ip -4 addr show host0 2>&1 | sed "s/^/[$h] /" || true
  done
  log "--- edge: nginx + chisel-server status ---"
  systemd-run --machine=rdda-edge --wait --pipe --quiet systemctl status nginx rdda-edge-chisel --no-pager -l 2>&1 | tail -25 | sed 's/^/[edge] /' || true
  log "--- eu: cloudflared(chisel client) journal ---"
  systemd-run --machine=rdda-eu --wait --pipe --quiet journalctl -u cloudflared --no-pager 2>&1 | tail -20 | sed 's/^/[eu] /' || true
  log "--- ru -> edge reachability ---"
  systemd-run --machine=rdda-ru --wait --pipe --quiet curl -vk --max-time 8 https://sub.rdda.test/ru/config 2>&1 | tail -20 | sed 's/^/[ru] /' || true
  for h in client ru eu; do
    log "--- $h: sing-box journal ---"
    systemd-run --machine="rdda-$h" --wait --pipe --quiet journalctl -u rdda-singbox -u rdda-client -u rdda-nfqws -u rdda-pull --no-pager 2>&1 | tail -30 | sed "s/^/[$h] /" || true
  done
  log "--- client: verbose curl through the tunnel ---"
  systemd-run --machine=rdda-client --wait --pipe --quiet curl -v --max-time 15 --socks5-hostname 127.0.0.1:1080 http://target.rdda.test/ 2>&1 | tail -25 | sed 's/^/[client] /' || true
}
teardown() {
  [ "$PASSED" = 1 ] || diag
  log "teardown"
  for h in "${HOSTS[@]}"; do machinectl terminate "rdda-$h" 2>/dev/null || true; done
  iptables -D FORWARD -i br-rdda -j ACCEPT 2>/dev/null || true
  iptables -D FORWARD -o br-rdda -j ACCEPT 2>/dev/null || true
  iptables -t nat -D POSTROUTING -s 203.0.113.0/24 ! -d 203.0.113.0/24 -j MASQUERADE 2>/dev/null || true
  ip link del br-rdda 2>/dev/null || true
  nft delete table inet rdda 2>/dev/null || true
}
trap teardown EXIT

log "=== build image ==="; bash "$HERE/image.sh" "$REPO_ROOT"
log "=== bring up network ==="; bash "$HERE/net.sh"
log "=== provision ==="
bash "$HERE/provision-target.sh"
bash "$HERE/provision-edge.sh"
bash "$HERE/provision-eu.sh"
bash "$HERE/provision-ru.sh"
bash "$HERE/provision-client.sh"
log "=== assert ==="; bash "$HERE/assert.sh"
PASSED=1
log "=== PASS ==="
