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

teardown() {
  log "teardown"
  for h in "${HOSTS[@]}"; do machinectl terminate "rdda-$h" 2>/dev/null || true; done
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
log "=== PASS ==="
