#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck disable=SC1091
# shellcheck source=test/integration/multihost/lib.sh
. "$HERE/lib.sh"

log "1) EU has no inbound listener reachable from other hosts"
# From RU, a direct TCP connect to EU must fail (nft drops new inbound to EU).
if nsrun ru bash -lc 'curl -fsS --max-time 5 http://eu:8443/ >/dev/null 2>&1'; then
  die "EU is reachable directly from RU; expected zero inbound"
fi
log "OK: EU not directly reachable (zero inbound)"

log "2) RU cannot reach the internet target directly (only EU may)"
if nsrun ru bash -lc 'curl -fsS --max-time 5 http://target.rdda.test/ >/dev/null 2>&1'; then
  die "RU reached the target directly; exit-via-EU proof would be meaningless"
fi
log "OK: target not directly reachable from RU"

log "3) GREEN: client reaches the EU-only target THROUGH the tunnel"
body="$(nsrun client bash -lc 'curl -fsS --max-time 20 --socks5-hostname 127.0.0.1:1080 http://target.rdda.test/ 2>/dev/null' || true)"
[ "$body" = "RDDA_OK" ] || die "client did not reach target via tunnel (got: '${body}')"
log "OK: client -> RU -> edge -> EU -> target returned RDDA_OK (exit is EU)"

log "4) pull-sync: a NEW client added on EU lands in RU's config"
NEW_UUID="$(nsrun eu bash -lc 'rdda client add latecomer >/dev/null; jq -r .uuid /etc/rdda/clients/latecomer.json')"
nsrun ru systemctl start rdda-pull.service
ok=no
for _ in $(seq 1 20); do
  if nsrun ru bash -lc "jq -e --arg u '$NEW_UUID' '.inbounds[0].settings.clients[]|select(.id==\$u)' /etc/rdda/xray.json >/dev/null 2>&1"; then ok=yes; break; fi
  sleep 0.5
done
[ "$ok" = yes ] || die "pull-sync did not deliver the new client to RU"
log "OK: pull-sync delivered the new client to RU"

log "ALL ASSERTIONS PASSED"
