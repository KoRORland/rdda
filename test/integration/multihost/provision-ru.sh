#!/usr/bin/env bash
# shellcheck shell=bash
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=test/integration/multihost/lib.sh disable=SC1091
. "$HERE/lib.sh"
REPO_ROOT="$(cd "$HERE/../../.." && pwd)"
root=/var/lib/machines/rdda-ru

log "install real rdda-xray + pull units on RU"
install -m0644 "$REPO_ROOT/deploy/systemd/rdda-xray.service"   "$root/etc/systemd/system/rdda-xray.service"
install -m0644 "$REPO_ROOT/deploy/systemd/rdda-pull.service"   "$root/etc/systemd/system/rdda-pull.service"
install -m0644 "$REPO_ROOT/deploy/systemd/rdda-pull.timer"     "$root/etc/systemd/system/rdda-pull.timer"

log "fetch the pull token from EU (operator looks it up on EU)"
TOKEN="$(nsrun eu bash -lc "grep '^pull_token:' /etc/rdda/config.yaml | awk '{print \$2}'")"

log "RU real flow: pull.env + initial pull + start xray + enable timer"
nsrun ru bash -eus <<INRU
install -d -m0700 -o rdda -g rdda /etc/rdda
cat > /etc/rdda/pull.env <<ENV
RDDA_PULL_FROM=https://sub.rdda.test/ru/config
RDDA_PULL_TOKEN=${TOKEN}
ENV
chmod 600 /etc/rdda/pull.env
# sudoers so the rdda user can reload xray after a pull
echo 'rdda ALL=(root) NOPASSWD: /usr/bin/systemctl reload-or-restart rdda-xray' > /etc/sudoers.d/rdda-reload
chmod 440 /etc/sudoers.d/rdda-reload
# initial pull (no --dir, no --dest: defaults to /etc/rdda/xray.json)
rdda pull --from https://sub.rdda.test/ru/config --token ${TOKEN} --reload-cmd true
chown -R rdda:rdda /etc/rdda
systemctl daemon-reload
systemctl enable --now rdda-xray rdda-pull.timer
INRU
wait_active ru rdda-xray
log "ru provisioned"
