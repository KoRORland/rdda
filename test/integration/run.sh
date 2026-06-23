#!/usr/bin/env bash
# Real-deployment integration harness. Provisions EU and RU exactly like a real
# deploy: the ACTUAL deploy/systemd/rdda-xray.service unit, running as the `rdda`
# user, reading configs from 0700 /etc/rdda{,-ru} owned by rdda. This means a
# unit/user/permission regression (e.g. a service that can't read its config)
# makes this harness fail — unlike a synthetic loopback check.
#
# Must run as root. Usage: run.sh <EU_PORT> <RU_PORT>
set -euo pipefail
EU_PORT="$1"
RU_PORT="$2"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UNIT_SRC="$REPO_ROOT/deploy/systemd/rdda-xray.service"

[ "$(id -u)" -eq 0 ] || { echo "must run as root"; exit 2; }
for bin in xray rdda jq systemctl; do
  command -v "$bin" >/dev/null || { echo "$bin not installed"; exit 2; }
done

# Clean any prior run.
systemctl stop rdda-xray.service rdda-xray-ru.service 2>/dev/null || true
rm -f /etc/systemd/system/rdda-xray.service /etc/systemd/system/rdda-xray-ru.service
rm -rf /etc/rdda /etc/rdda-ru
systemctl daemon-reload 2>/dev/null || true

# rdda user, exactly as the installer creates it.
id rdda >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin rdda

# EU node = source of truth.
rdda --dir /etc/rdda init \
  --ru-host 127.0.0.1 --eu-host 127.0.0.1 \
  --client-sni www.cloudflare.com --tunnel-sni www.cloudflare.com >/dev/null
rdda --dir /etc/rdda client add tester >/dev/null

rdda --dir /etc/rdda render eu \
  | jq ".inbounds[0].port=$EU_PORT | .inbounds[0].listen=\"127.0.0.1\"" \
  > /etc/rdda/xray.json

mkdir -p /etc/rdda-ru
rdda --dir /etc/rdda render ru \
  | jq ".inbounds[0].port=$RU_PORT | .inbounds[0].listen=\"127.0.0.1\" | .outbounds[0].settings.vnext[0].port=$EU_PORT" \
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
