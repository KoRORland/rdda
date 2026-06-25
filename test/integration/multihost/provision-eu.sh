#!/usr/bin/env bash
# shellcheck shell=bash
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=test/integration/multihost/lib.sh disable=SC1091
. "$HERE/lib.sh"
REPO_ROOT="$(cd "$HERE/../../.." && pwd)"
root=/var/lib/machines/rdda-eu

log "EU real flow: init (CF-enabled) + render eu + a tester client"
nsrun eu rdda init --ru-host ru --eu-host eu \
  --cf-tunnel-host tunnel.rdda.test --cf-sub-host sub.rdda.test \
  --cf-tunnel-id testtunnel --cf-credentials-file /etc/cloudflared/test.json
nsrun eu bash -eus <<'INEU'
rdda client add tester >/dev/null
rdda render eu > /etc/rdda/xray.json
chown -R rdda:rdda /etc/rdda
chmod 700 /etc/rdda
INEU

log "install the REAL rdda-xray unit (reads /etc/rdda/xray.json, loopback under CF)"
install -m0644 "$REPO_ROOT/deploy/systemd/rdda-xray.service" "$root/etc/systemd/system/rdda-xray.service"
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

# Pin EU xray + sub to the loopback ports the edge forwards (8443/8080).
nsrun eu bash -eus <<'INEU'
sed -i 's#"port": 443#"port": 8443#' /etc/rdda/xray.json
sed -i 's#"loglevel": "warning"#"loglevel": "debug"#' /etc/rdda/xray.json
systemctl daemon-reload
systemctl enable --now rdda-xray rdda-sub cloudflared
INEU
wait_active eu rdda-xray
wait_active eu rdda-sub
wait_active eu cloudflared
log "eu provisioned"
