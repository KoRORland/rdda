#!/usr/bin/env bash
# shellcheck shell=bash
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=test/integration/multihost/lib.sh disable=SC1091
. "$HERE/lib.sh"
root=/var/lib/machines/rdda-client

log "render the client config on EU (operator runs this on EU), copy to client"
UUID="$(nsrun eu bash -lc "jq -r .uuid /etc/rdda/clients/tester.json")"
nsrun eu rdda render client --uuid "$UUID" --socks-port 1080 > "$root/etc/client.json"

cat > "$root/etc/systemd/system/rdda-client.service" <<'UNIT'
[Unit]
Description=RDDA client xray (SOCKS -> RU)
After=network-online.target
[Service]
ExecStart=/usr/local/bin/xray run -c /etc/client.json
Restart=on-failure
[Install]
WantedBy=multi-user.target
UNIT
nsrun client systemctl enable --now rdda-client
wait_active client rdda-client
log "client provisioned"
