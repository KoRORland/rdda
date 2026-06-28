#!/usr/bin/env bash
# shellcheck shell=bash
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=test/integration/multihost/lib.sh disable=SC1091
. "$HERE/lib.sh"
CA_DIR=/run/rdda-ca
root=/var/lib/machines/rdda-edge

log "sign edge cert with the test CA (SAN tunnel.rdda.test, sub.rdda.test)"
openssl req -newkey rsa:2048 -nodes -keyout "$CA_DIR/edge.key" -out "$CA_DIR/edge.csr" \
  -subj "/CN=tunnel.rdda.test" >/dev/null 2>&1
openssl x509 -req -in "$CA_DIR/edge.csr" -CA "$CA_DIR/ca.crt" -CAkey "$CA_DIR/ca.key" \
  -CAcreateserial -days 2 -out "$CA_DIR/edge.crt" \
  -extfile <(printf 'subjectAltName=DNS:tunnel.rdda.test,DNS:sub.rdda.test') >/dev/null 2>&1
install -D -m0644 "$CA_DIR/edge.crt" "$root/etc/ssl/edge.crt"
install -D -m0600 "$CA_DIR/edge.key" "$root/etc/ssl/edge.key"

log "nginx: TLS terminate :443 -> chisel-forwarded EU loopback"
cat > "$root/etc/nginx/conf.d/edge.conf" <<'NGINX'
server {
    # Lane B: the RU->EU CF hop is sing-box VLESS over WebSocket (httpupgrade does
    # not survive real Cloudflare; ws does — proven in the P0 de-risk). nginx must
    # therefore forward the HTTP/1.1 Upgrade: as the real Cloudflare edge does.
    # No http2 here: the WS handshake is HTTP/1.1.
    listen 443 ssl;
    server_name tunnel.rdda.test sub.rdda.test;
    ssl_certificate     /etc/ssl/edge.crt;
    ssl_certificate_key /etc/ssl/edge.key;
    location / {
        proxy_pass http://127.0.0.1:8443;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 300s;
        client_max_body_size 0;
    }
    location /sub/ { proxy_pass http://127.0.0.1:8080; }
    location /ru/  { proxy_pass http://127.0.0.1:8080; }
}
NGINX
rm -f "$root/etc/nginx/sites-enabled/default"

log "chisel server unit on :7000 (reverse tunnels allowed)"
cat > "$root/etc/systemd/system/rdda-edge-chisel.service" <<'UNIT'
[Unit]
Description=RDDA edge chisel server (Cloudflare stand-in)
After=network-online.target
[Service]
ExecStart=/usr/local/bin/chisel server --reverse --host 0.0.0.0 --port 7000
Restart=on-failure
[Install]
WantedBy=multi-user.target
UNIT

nsrun edge systemctl enable --now nginx rdda-edge-chisel
nsrun edge systemctl restart nginx   # ensure our :443 config is the loaded one
wait_active edge nginx
wait_active edge rdda-edge-chisel
log "edge provisioned"
