#!/usr/bin/env bash
# Real-deployment integration harness. Provisions EU and RU exactly like a real
# deploy: the ACTUAL deploy/systemd/rdda-xray.service unit, running as the `rdda`
# user, reading configs from 0700 /etc/rdda{,-ru} owned by rdda. This means a
# unit/user/permission regression (e.g. a service that can't read its config)
# makes this harness fail — unlike a synthetic loopback check.
#
# Also asserts:
#   - EU inbound is loopback-only (not publicly bound) under CF fronting.
#   - rdda pull lands a new client into the RU config with no manual render.
#
# Must run as root. Usage: run.sh <EU_PORT> <RU_PORT>
set -euo pipefail
EU_PORT="$1"
RU_PORT="$2"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UNIT_SRC="$REPO_ROOT/deploy/systemd/rdda-xray.service"

[ "$(id -u)" -eq 0 ] || { echo "must run as root"; exit 2; }
for bin in xray rdda jq systemctl nginx openssl ss curl python3; do
  command -v "$bin" >/dev/null || { echo "$bin not installed"; exit 2; }
done

# Clean any prior run.
systemctl stop rdda-xray.service rdda-xray-ru.service 2>/dev/null || true
rm -f /etc/systemd/system/rdda-xray.service /etc/systemd/system/rdda-xray-ru.service
rm -rf /etc/rdda /etc/rdda-ru
systemctl daemon-reload 2>/dev/null || true

# rdda user, exactly as the installer creates it.
id rdda >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin rdda

# EU node = source of truth, CF-fronted (nginx stands in for cloudflared).
CF_HOST=tunnel.local
rdda --dir /etc/rdda init \
  --ru-host 127.0.0.1 --eu-host 127.0.0.1 \
  --client-sni www.cloudflare.com --tunnel-sni www.cloudflare.com \
  --cf-tunnel-host "$CF_HOST" --cf-sub-host sub.local \
  --cf-tunnel-id testtunnel --cf-credentials-file /etc/cloudflared/test.json >/dev/null
rdda --dir /etc/rdda client add tester >/dev/null

# EU inbound is loopback + security:none under CF. Pin its port for the test.
rdda --dir /etc/rdda render eu \
  | jq ".inbounds[0].port=$EU_PORT" \
  > /etc/rdda/xray.json

# --- cloudflared stand-in: TLS terminate on :443 -> loopback EU origin ---
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -keyout /etc/ssl/cf.key -out /etc/ssl/cf.crt -subj "/CN=$CF_HOST" >/dev/null 2>&1
cat >/etc/nginx/conf.d/cf.conf <<NGINX
server {
    listen 443 ssl;
    server_name $CF_HOST sub.local;
    ssl_certificate     /etc/ssl/cf.crt;
    ssl_certificate_key /etc/ssl/cf.key;
    location / { proxy_pass http://127.0.0.1:$EU_PORT; proxy_http_version 1.1; }
    location /sub/ { proxy_pass http://127.0.0.1:8080; }
    location /ru/  { proxy_pass http://127.0.0.1:8080; }
}
NGINX
echo "127.0.0.1 $CF_HOST sub.local" >> /etc/hosts
nginx -t && systemctl restart nginx

# Render the RU node to dial the CF front (:443, allowInsecure for self-signed cert).
mkdir -p /etc/rdda-ru
rdda --dir /etc/rdda render ru \
  | jq ".inbounds[0].port=$RU_PORT | .inbounds[0].listen=\"127.0.0.1\" \
        | .outbounds[0].settings.vnext[0].port=443 \
        | .outbounds[0].streamSettings.tlsSettings.allowInsecure=true" \
  > /etc/rdda-ru/xray.json

# Real deploy ownership/permissions (this is what the DynamicUser bug trips on).
chown -R rdda:rdda /etc/rdda /etc/rdda-ru
chmod 700 /etc/rdda /etc/rdda-ru

# Install the REAL unit for EU; derive the RU unit from the SAME file (only the
# config path/description differ) so any unit regression is exercised on both.
install -m 0644 "$UNIT_SRC" /etc/systemd/system/rdda-xray.service
sed -e 's#/etc/rdda/xray.json#/etc/rdda-ru/xray.json#' \
    -e 's#Description=.*#Description=RDDA xray-core (ru test instance)#' \
    "$UNIT_SRC" > /etc/systemd/system/rdda-xray-ru.service
systemctl daemon-reload
systemctl restart rdda-xray.service rdda-xray-ru.service

# Both units must reach active(running); fail loudly with logs otherwise.
for unit in rdda-xray rdda-xray-ru; do
  ok=no
  for _ in $(seq 1 20); do
    state="$(systemctl is-active "$unit" 2>/dev/null || true)"
    if [ "$state" = active ]; then ok=yes; break; fi
    if [ "$state" = failed ]; then break; fi
    sleep 0.5
  done
  if [ "$ok" != yes ]; then
    echo "=== $unit did not reach active (state=$(systemctl is-active "$unit" 2>/dev/null || true)) ==="
    systemctl status "$unit" --no-pager -l 2>&1 | head -30 || true
    journalctl -u "$unit" --no-pager 2>&1 | tail -40 || true
    exit 3
  fi
done
echo "rdda-xray + rdda-xray-ru active"

# EU inbound must NOT be listening on a public address.
if ss -ltn | awk '{print $4}' | grep -qE "0\.0\.0\.0:$EU_PORT|\[::\]:$EU_PORT"; then
  echo "FAIL: EU inbound is publicly bound; expected loopback-only under CF" >&2
  exit 1
fi
echo "OK: EU inbound is loopback-only"

# Two-hop traffic assertion: client SOCKS → RU(:RU_PORT) → nginx(:443) → EU(:EU_PORT) → internet.
# The client config dials RU via REALITY; the tunnel hop now flows through nginx (CF stand-in).
CLIENT_SOCKS_PORT=19080

XRAY_CLIENT_CFG="$(mktemp /tmp/rdda-client-XXXXXX.json)"
trap 'rm -f "$XRAY_CLIENT_CFG"' EXIT

# Extract client reality fields from config.yaml via python3 (avoids fragile grep on nested YAML).
python3 - /etc/rdda/config.yaml "$RU_PORT" "$CLIENT_SOCKS_PORT" "$XRAY_CLIENT_CFG" <<'PYEOF'
import sys, yaml, json
cfg_path, ru_port, socks_port, out_path = sys.argv[1:]
with open(cfg_path) as f:
    cfg = yaml.safe_load(f)
cr = cfg["client_reality"]
clients_path = cfg_path.replace("config.yaml", "clients")
import os, glob
client_files = glob.glob(os.path.join(clients_path, "*.json"))
client_file = next((c for c in client_files if "tester" in os.path.basename(c)), client_files[0])
with open(client_file) as f:
    client = json.load(f)
doc = {
    "log": {"loglevel": "warning"},
    "inbounds": [{
        "listen": "127.0.0.1", "port": int(socks_port),
        "protocol": "socks", "tag": "socks-in",
        "settings": {"auth": "noauth", "udp": False}
    }],
    "outbounds": [{
        "protocol": "vless", "tag": "proxy",
        "settings": {"vnext": [{
            "address": "127.0.0.1", "port": int(ru_port),
            "users": [{"id": client["uuid"], "encryption": "none", "flow": ""}]
        }]},
        "streamSettings": {
            "network": "xhttp",
            "xhttpSettings": {"path": cfg["client_path"]},
            "security": "reality",
            "realitySettings": {
                "serverName": cr["server_name"],
                "publicKey": cr["public_key"],
                "shortId": cr["short_ids"][0],
                "fingerprint": "chrome"
            }
        }
    }]
}
with open(out_path, "w") as f:
    json.dump(doc, f, indent=2)
PYEOF

xray run -c "$XRAY_CLIENT_CFG" &
XRAY_CLIENT_PID=$!
sleep 2

# Probe: send a request through the tunnel.
if ! curl --socks5 "127.0.0.1:$CLIENT_SOCKS_PORT" --max-time 10 -fsS https://www.example.com >/dev/null 2>&1; then
  echo "FAIL: two-hop tunnel probe failed (client→RU→nginx→EU→internet)" >&2
  kill "$XRAY_CLIENT_PID" 2>/dev/null || true
  exit 1
fi
kill "$XRAY_CLIENT_PID" 2>/dev/null || true
echo "OK: two-hop tunnel works (client SOCKS → RU → nginx(:443) → EU → internet)"

# Pull-sync assertion: add a NEW client on EU after the RU config was rendered,
# then pull and verify the new UUID lands in /etc/rdda-ru/xray.json.
rdda --dir /etc/rdda client add latecomer >/dev/null
NEW_UUID=$(jq -r '.uuid' /etc/rdda/clients/latecomer.json)

# Start the sub server on loopback (EU).
rdda --dir /etc/rdda serve --addr 127.0.0.1:8080 &
SUB_PID=$!
sleep 1

PULL_TOKEN=$(grep '^pull_token:' /etc/rdda/config.yaml | awk '{print $2}')

rdda --dir /etc/rdda-ru pull \
  --from "http://127.0.0.1:8080/ru/config" --token "$PULL_TOKEN" \
  --dest /etc/rdda-ru/xray.json --reload-cmd "true"

# Re-apply the test port surgery the renderer can't know about, then verify.
jq ".inbounds[0].port=$RU_PORT | .inbounds[0].listen=\"127.0.0.1\" \
    | .outbounds[0].settings.vnext[0].port=443 \
    | .outbounds[0].streamSettings.tlsSettings.allowInsecure=true" \
   /etc/rdda-ru/xray.json > /etc/rdda-ru/xray.json.tmp && mv /etc/rdda-ru/xray.json.tmp /etc/rdda-ru/xray.json

if ! jq -e --arg u "$NEW_UUID" '.inbounds[0].settings.clients[] | select(.id==$u)' /etc/rdda-ru/xray.json >/dev/null; then
  echo "FAIL: pulled RU config does not contain the new client" >&2
  kill $SUB_PID 2>/dev/null || true
  exit 1
fi
echo "OK: pull-sync delivered the new client to RU"
kill $SUB_PID 2>/dev/null || true
