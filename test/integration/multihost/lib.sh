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

# Poll until <unit> is active inside <host>, or fail after ~20s.
wait_active() {
  local host="$1" unit="$2" i state
  for i in $(seq 1 40); do
    state="$(nsrun "$host" systemctl is-active "$unit" 2>/dev/null || true)"
    [ "$state" = active ] && { log "$host:$unit active"; return 0; }
    [ "$state" = failed ] && break
    sleep 0.5
  done
  nsrun "$host" systemctl status "$unit" --no-pager -l 2>&1 | head -30 || true
  nsrun "$host" journalctl -u "$unit" --no-pager 2>&1 | tail -40 || true
  die "$host:$unit did not reach active (state=$state)"
}
