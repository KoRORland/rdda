#!/usr/bin/env bash
# Update smoke test: install the PREVIOUS published release's rdda binary, then
# run `rdda update` and assert it reaches the CURRENT (just-published) release.
#
# This exercises the REAL self-update path against REAL published assets — the
# GitHub download (with its flaky-CDN retries), checksum verification, the
# atomic swap, the `rdda version` self-check, and the "restart only a unit that
# exists on this node" logic. On a bare runner there is no rdda-sub unit, which
# is exactly the RU-node shape that regressed in v0.4.1 (it tried to restart the
# EU-only rdda-sub and rolled back every valid update).
#
# Intended for an EPHEMERAL host (CI runner): it overwrites /usr/local/bin/rdda.
# Do not run on a node you care about.
#
# Usage: sudo test/update-smoke.sh <current-tag> [arch]
#   e.g. sudo test/update-smoke.sh v0.4.2 amd64
set -euo pipefail

REPO="KoRORland/rdda"
CURRENT="${1:?usage: update-smoke.sh <current-tag> [arch]}"
ARCH="${2:-amd64}"
BIN="/usr/local/bin/rdda"

log() { printf '[update-smoke] %s\n' "$*"; }
die() { printf '[update-smoke][FAIL] %s\n' "$*" >&2; exit 1; }

api() { curl -fsSL --retry 4 --connect-timeout 20 --max-time 120 "https://api.github.com/repos/${REPO}/$1"; }

# previous = the newest published release tag that is not CURRENT.
PREV="$(api releases | jq -r --arg cur "$CURRENT" '[.[] | .tag_name] | map(select(. != $cur)) | .[0] // empty')"
[ -n "$PREV" ] || die "could not resolve a previous release (need at least one release older than ${CURRENT})"
log "previous=${PREV}  current=${CURRENT}  arch=${ARCH}"

# Install the previous release binary as the "installed" version.
log "installing previous release ${PREV}"
curl -fsSL --retry 4 --connect-timeout 20 --max-time 600 \
  -o "$BIN" "https://github.com/${REPO}/releases/download/${PREV}/rdda-linux-${ARCH}"
chmod 0755 "$BIN"
got="$("$BIN" version)"
[ "$got" = "$PREV" ] || die "installed prev but version reports ${got} (want ${PREV})"
log "OK: installed ${PREV}"

# The real update. On a bare host there is no long-running rdda service, so this
# must succeed on the binary self-check alone (the v0.4.1 RU regression).
log "running: rdda update"
"$BIN" update

got="$("$BIN" version)"
[ "$got" = "$CURRENT" ] || die "after update, version is ${got} (want ${CURRENT})"
log "OK: ${PREV} -> ${CURRENT} via rdda update"

# A second run must be a clean no-op (already current), not an error.
log "verifying re-run is a no-op"
"$BIN" update
[ "$("$BIN" version)" = "$CURRENT" ] || die "re-run changed the version unexpectedly"

# The rollback path must still work: restore the previous binary on demand.
log "verifying rollback restores ${PREV}"
"$BIN" update --rollback
got="$("$BIN" version)"
[ "$got" = "$PREV" ] || die "rollback did not restore ${PREV} (got ${got})"

log "ALL UPDATE-SMOKE ASSERTIONS PASSED (${PREV} -> ${CURRENT} -> rollback ${PREV})"
