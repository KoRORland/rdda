#!/usr/bin/env bash
# Boots EU and RU xray instances on loopback for the integration test.
# The client xray instance is started by the Go test (it generates the config).
# Requires `xray` and `rdda` on PATH. Writes PIDs to $DIR/pids.
set -euo pipefail
DIR="$1"
EU_PORT="$2"
RU_PORT="$3"

rdda --dir "$DIR/state" init \
  --ru-host 127.0.0.1 \
  --eu-host 127.0.0.1 \
  --client-sni www.cloudflare.com \
  --tunnel-sni www.cloudflare.com >/dev/null

rdda --dir "$DIR/state" client add tester >/dev/null

rdda --dir "$DIR/state" render eu \
  | jq ".inbounds[0].port=$EU_PORT | .inbounds[0].listen=\"127.0.0.1\"" \
  > "$DIR/eu.json"

rdda --dir "$DIR/state" render ru \
  | jq ".inbounds[0].port=$RU_PORT | .inbounds[0].listen=\"127.0.0.1\" | .outbounds[0].settings.vnext[0].port=$EU_PORT" \
  > "$DIR/ru.json"

xray run -c "$DIR/eu.json" >"$DIR/eu.log" 2>&1 & echo $! >  "$DIR/pids"
xray run -c "$DIR/ru.json" >"$DIR/ru.log" 2>&1 & echo $! >> "$DIR/pids"
sleep 1
echo "started"
