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

log "4) multiplex negotiated on client->RU (the inspected hop; Signal-3 guard)"
# sing-box logs an accepted muxed inbound as "inbound multiplex connection ...".
if nsrun ru bash -lc "journalctl -u rdda-singbox --no-pager 2>&1 | grep -qiE 'multiplex|mux'"; then
  log "OK: client->RU connection is multiplexed"
else
  die "multiplex was NOT negotiated on the client->RU hop (Signal-3 regression)"
fi

log "5) pull-sync: a NEW client added on EU lands in RU's config"
# `rdda client add` runs as root and writes clients/* root-owned 0600; the EU sub
# server runs as the rdda user, so re-chown (the documented post-add prod step)
# before it must serve the new client at /ru/config.
NEW_UUID="$(nsrun eu bash -lc 'rdda client add latecomer >/dev/null; chown -R rdda:rdda /etc/rdda; jq -r .uuid /etc/rdda/clients/latecomer.json')"
nsrun ru systemctl start rdda-pull.service
ok=no
for _ in $(seq 1 20); do
  if nsrun ru bash -lc "jq -e --arg u '$NEW_UUID' '.inbounds[0].users[]|select(.uuid==\$u)' /etc/rdda/singbox.json >/dev/null 2>&1"; then ok=yes; break; fi
  sleep 0.5
done
[ "$ok" = yes ] || die "pull-sync did not deliver the new client to RU"
log "OK: pull-sync delivered the new client to RU"

log "6) health beat: RU -> EU, visible in 'rdda status' on EU"
# Fire one beat now (deterministic; the timer's first beat is ~2min out).
nsrun ru systemctl start rdda-health.service
ok=no
for _ in $(seq 1 10); do
  if nsrun eu bash -lc 'rdda status | grep -q "last beat"'; then ok=yes; break; fi
  sleep 0.5
done
[ "$ok" = yes ] || { nsrun eu rdda status 2>&1 | sed 's/^/[eu-status] /' || true; die "EU rdda status did not show the RU health beat"; }
log "OK: EU rdda status shows the RU node's health beat"

log "ALL ASSERTIONS PASSED"
