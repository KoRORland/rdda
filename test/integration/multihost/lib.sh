# test/integration/multihost/lib.sh
# Shared helpers for the multi-host nspawn integration harness.
# shellcheck shell=bash

log() { printf '[multihost] %s\n' "$*" >&2; }
die() { printf '[multihost][FAIL] %s\n' "$*" >&2; exit 1; }

# Run a command inside a booted container as root, wait, and stream output.
nsrun() {
  local host="$1"; shift
  systemd-run --machine="rdda-${host}" --wait --pipe --quiet "$@"
}

# Poll until <unit> is active inside <host>, or fail after ~20s. Also rejects a
# crash-looping unit: a service with Restart=on-failure reads "active" transiently
# between restarts, so require it to have NOT restarted (NRestarts=0).
wait_active() {
  local host="$1" unit="$2" state restarts
  for _ in $(seq 1 40); do
    state="$(nsrun "$host" systemctl is-active "$unit" 2>/dev/null || true)"
    if [ "$state" = active ]; then
      restarts="$(nsrun "$host" systemctl show -p NRestarts --value "$unit" 2>/dev/null || echo 0)"
      [ "${restarts:-0}" = 0 ] && { log "$host:$unit active"; return 0; }
      break
    fi
    [ "$state" = failed ] && break
    sleep 0.5
  done
  nsrun "$host" systemctl status "$unit" --no-pager -l 2>&1 | head -30 || true
  nsrun "$host" journalctl -u "$unit" --no-pager 2>&1 | tail -40 || true
  die "$host:$unit not stably active (state=$state, restarts=${restarts:-?})"
}
