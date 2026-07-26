#!/usr/bin/env bash
# shellcheck shell=bash
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=test/integration/multihost/lib.sh disable=SC1091
. "$HERE/lib.sh"
REPO_ROOT="$(cd "$HERE/../../.." && pwd)"
root=/var/lib/machines/rdda-eu

log "EU real flow: init (CF-enabled) + render eu + a tester client"
# Pin --client-sni so the harness is deterministic and does not exercise live
# REALITY-dest auto-discovery (which ranks external hosts by RTT). www.apple.com
# is the real site the client->RU handshake relays to over the bridge's egress.
nsrun eu rdda init --ru-host ru --eu-host eu \
  --client-sni www.apple.com --tunnel-sni www.apple.com \
  --cf-tunnel-host tunnel.rdda.test --cf-sub-host sub.rdda.test \
  --cf-tunnel-id testtunnel --cf-credentials-file /etc/cloudflared/test.json
nsrun eu bash -eus <<'INEU'
rdda client add tester >/dev/null
rdda render eu > /etc/rdda/singbox.json
chown -R rdda:rdda /etc/rdda
chmod 700 /etc/rdda
INEU

log "install the REAL rdda-singbox unit (reads /etc/rdda/singbox.json, loopback under CF)"
install -m0644 "$REPO_ROOT/deploy/systemd/rdda-singbox.service" "$root/etc/systemd/system/rdda-singbox.service"
install -m0644 "$REPO_ROOT/deploy/systemd/rdda-sub.service"  "$root/etc/systemd/system/rdda-sub.service"

log "cloudflared stand-in: chisel client reverse-forwards EU loopback to edge"
cat > "$root/etc/systemd/system/cloudflared.service" <<'UNIT'
[Unit]
Description=RDDA cloudflared stand-in (chisel reverse tunnel to edge)
After=network-online.target
[Service]
ExecStart=/usr/local/bin/chisel client edge:7000 R:127.0.0.1:8443:127.0.0.1:8443 R:127.0.0.1:8080:127.0.0.1:8080
Restart=on-failure
[Install]
WantedBy=multi-user.target
UNIT

# Pin the EU sing-box CF inbound to the loopback port the edge/chisel forwards
# (8443); init hardcodes EUPort=443. Bump log to debug for diagnosis.
nsrun eu bash -eus <<'INEU'
tmp="$(mktemp)"
jq '.inbounds[0].listen_port = 8443 | .log.level = "debug"' /etc/rdda/singbox.json > "$tmp"
mv "$tmp" /etc/rdda/singbox.json
chown rdda:rdda /etc/rdda/singbox.json
systemctl daemon-reload
systemctl enable --now rdda-singbox rdda-sub cloudflared
INEU
wait_active eu rdda-singbox
wait_active eu rdda-sub
wait_active eu cloudflared
log "eu provisioned"
