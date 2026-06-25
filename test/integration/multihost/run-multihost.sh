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

log "=== build image ==="; "$HERE/image.sh" "$REPO_ROOT"
log "=== bring up network ==="; "$HERE/net.sh"
log "=== provision ==="
"$HERE/provision-target.sh"
"$HERE/provision-edge.sh"
"$HERE/provision-eu.sh"
"$HERE/provision-ru.sh"
"$HERE/provision-client.sh"
log "=== assert ==="; "$HERE/assert.sh"
log "=== PASS ==="
