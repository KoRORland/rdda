#!/usr/bin/env bash
# shellcheck shell=bash
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=test/integration/multihost/lib.sh disable=SC1091
. "$HERE/lib.sh"
REPO_ROOT="$(cd "$HERE/../../.." && pwd)"
root=/var/lib/machines/rdda-ru

log "install real rdda-singbox + pull + nfqws units on RU"
install -m0644 "$REPO_ROOT/deploy/systemd/rdda-singbox.service" "$root/etc/systemd/system/rdda-singbox.service"
install -m0644 "$REPO_ROOT/deploy/systemd/rdda-pull.service"   "$root/etc/systemd/system/rdda-pull.service"
install -m0644 "$REPO_ROOT/deploy/systemd/rdda-pull.timer"     "$root/etc/systemd/system/rdda-pull.timer"
install -m0644 "$REPO_ROOT/deploy/systemd/rdda-nfqws.service"  "$root/etc/systemd/system/rdda-nfqws.service"
install -D -m0644 "$REPO_ROOT/deploy/nftables/rdda-nfqws.nft"  "$root/etc/rdda/rdda-nfqws.nft"

log "fetch the pull token from EU (operator looks it up on EU)"
TOKEN="$(nsrun eu bash -lc "grep '^pull_token:' /etc/rdda/config.yaml | awk '{print \$2}'")"

log "RU real flow: pull.env + initial pull + start sing-box + enable timer"
nsrun ru bash -eus <<INRU
install -d -m0700 -o rdda -g rdda /etc/rdda
cat > /etc/rdda/pull.env <<ENV
RDDA_PULL_FROM=https://sub.rdda.test/ru/config
RDDA_PULL_TOKEN=${TOKEN}
ENV
chmod 600 /etc/rdda/pull.env
# sudoers so the rdda user can reload sing-box after a pull
echo 'rdda ALL=(root) NOPASSWD: /usr/bin/systemctl reload-or-restart rdda-singbox' > /etc/sudoers.d/rdda-reload
chmod 440 /etc/sudoers.d/rdda-reload
# initial pull (no --dir, no --dest: defaults to /etc/rdda/singbox.json)
rdda pull --from https://sub.rdda.test/ru/config --token ${TOKEN} --reload-cmd true
jq '.log.level = "debug"' /etc/rdda/singbox.json > /etc/rdda/singbox.json.new
mv /etc/rdda/singbox.json.new /etc/rdda/singbox.json
chown -R rdda:rdda /etc/rdda
systemctl daemon-reload
systemctl enable --now rdda-singbox rdda-pull.timer
# nfqws unit + nft are INSTALLED (deploy surface exercised) but intentionally NOT
# enabled here. Confirmed in CI: nfqws2 runs fine under nspawn (NFQUEUE binds), but
# the fake,split2 desync corrupts the RU->edge TLS handshake on this single-hop
# bridge (the "fake" decoy packet, which in production dies via TTL before the real
# dest, reaches the edge directly -> bad record mac). The desync is a real-internet
# DPI-evasion feature; a 1-hop test topology cannot validate its runtime faithfully.
INRU
wait_active ru rdda-singbox
log "ru provisioned"
