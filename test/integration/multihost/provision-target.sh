#!/usr/bin/env bash
# shellcheck shell=bash
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=test/integration/multihost/lib.sh disable=SC1091
. "$HERE/lib.sh"
root=/var/lib/machines/rdda-target
mkdir -p "$root/srv/www"
printf 'RDDA_OK\n' > "$root/srv/www/index.html"
cat > "$root/etc/nginx/conf.d/target.conf" <<'NGINX'
server { listen 80; root /srv/www; }
NGINX
rm -f "$root/etc/nginx/sites-enabled/default"
nsrun target systemctl enable --now nginx
wait_active target nginx
log "target provisioned (serves RDDA_OK on :80)"
