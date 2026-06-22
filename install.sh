#!/usr/bin/env bash
# RDDA node installer. Usage:
#   curl -fsSL https://raw.githubusercontent.com/KoRORland/rdda/main/install.sh | sudo bash -s -- <eu|ru> [--version vX.Y.Z] [--keep-ssh]
set -euo pipefail

REPO="KoRORland/rdda"
BIN_DST="/usr/local/bin/rdda"
STATE_DIR="/etc/rdda"
UNIT_DIR="/etc/systemd/system"

log()  { printf '\033[1;34m[rdda]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[rdda]\033[0m %s\n' "$*" >&2; }
fail() { printf '\033[1;31m[rdda] ERROR:\033[0m %s\n' "$*" >&2; exit 1; }

ROLE=""
VERSION="latest"
KEEP_SSH="no"

# --- parse args ---
[ "$#" -ge 1 ] || fail "role required: eu or ru"
ROLE="$1"; shift
case "$ROLE" in eu|ru) ;; *) fail "role must be 'eu' or 'ru', got '$ROLE'";; esac
while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      VERSION="${2:-}"; shift 2 || fail "--version needs a value"
      [ -n "$VERSION" ] || fail "--version needs a non-empty value"
      ;;
    --keep-ssh) KEEP_SSH="yes"; shift;;
    *) fail "unknown argument: $1";;
  esac
done

# --- preconditions ---
[ "$(id -u)" -eq 0 ] || fail "must run as root (use sudo)"
command -v curl >/dev/null || fail "curl is required"

case "$(uname -m)" in
  x86_64)  ARCH="amd64";;
  aarch64) ARCH="arm64";;
  *) fail "unsupported architecture: $(uname -m)";;
esac
log "role=$ROLE arch=$ARCH version=$VERSION"

# --- resolve release tag ---
if [ "$VERSION" = "latest" ]; then
  TAG="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -m1 '"tag_name"' | cut -d'"' -f4 || true)"
  [ -n "$TAG" ] || fail "could not resolve latest release tag (has a release been published?)"
else
  TAG="$VERSION"
fi
log "installing tag $TAG"

# --- download + verify + install rdda binary ---
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
BASE="https://github.com/${REPO}/releases/download/${TAG}"
curl -fsSL "${BASE}/rdda-linux-${ARCH}" -o "${TMP}/rdda-linux-${ARCH}"
curl -fsSL "${BASE}/SHA256SUMS"         -o "${TMP}/SHA256SUMS"
( cd "$TMP" && grep "rdda-linux-${ARCH}\$" SHA256SUMS | sha256sum -c - ) \
  || fail "checksum verification failed for rdda-linux-${ARCH}"
install -m 0755 "${TMP}/rdda-linux-${ARCH}" "$BIN_DST"
log "installed $($BIN_DST version) to $BIN_DST"

# --- install xray-core, disable its stock unit ---
log "installing xray-core"
bash -c "$(curl -fsSL https://github.com/XTLS/Xray-install/raw/main/install-release.sh)" @ install
systemctl disable --now xray.service 2>/dev/null || true

# --- state dir + user ---
mkdir -p "$STATE_DIR"; chmod 0700 "$STATE_DIR"
id rdda >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin rdda
log "state dir $STATE_DIR ready; rdda user present"

# --- systemd units (fetched at the resolved tag to match the binary) ---
RAW="https://raw.githubusercontent.com/${REPO}/${TAG}/deploy/systemd"
curl -fsSL "${RAW}/rdda-xray.service" -o "${UNIT_DIR}/rdda-xray.service"
if [ "$ROLE" = "eu" ]; then
  curl -fsSL "${RAW}/rdda-sub.service" -o "${UNIT_DIR}/rdda-sub.service"
fi
systemctl daemon-reload
log "installed systemd units (rdda-sub stays dormant on eu until v0.2)"

# --- host hardening (both roles) ---
log "hardening host: time sync + unattended upgrades + firewall"
systemctl enable --now systemd-timesyncd 2>/dev/null || true
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y unattended-upgrades ufw
systemctl enable --now unattended-upgrades 2>/dev/null || true

# --- firewall: FINAL step (so a mid-run failure never locks us out) ---
ufw default deny incoming
ufw default allow outgoing
ufw allow 443/tcp
if [ "$ROLE" = "eu" ]; then
  ufw allow 22/tcp
else
  if [ "$KEEP_SSH" = "yes" ]; then
    ufw allow 22/tcp
    warn "RU node: --keep-ssh set, leaving SSH (22) OPEN"
  else
    warn "RU node: closing SSH (22). Use your VPS provider console from now on."
  fi
fi
ufw --force enable
log "firewall active: $(ufw status | tr '\n' ' ')"

# --- next steps ---
cat <<EOF

[rdda] $ROLE node provisioned. Next steps:
EOF
if [ "$ROLE" = "eu" ]; then
  cat <<'EOF'
  1. rdda --dir /etc/rdda init --ru-host <RU_IP> --eu-host <EU_HOST>
  2. rdda --dir /etc/rdda render eu > /etc/rdda/xray.json
  3. chown -R rdda:rdda /etc/rdda
  4. systemctl enable --now rdda-xray
  5. rdda --dir /etc/rdda client add <name>   # send the printed vless:// link privately
  (RU config: run `rdda --dir /etc/rdda render ru` and copy the output to the RU node.)
EOF
else
  cat <<'EOF'
  1. On the EU node, run: rdda --dir /etc/rdda render ru
  2. Copy that output to this RU node's /etc/rdda/xray.json
  3. chown -R rdda:rdda /etc/rdda
  4. systemctl enable --now rdda-xray
EOF
fi
