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

# --- install sing-box (pinned) ---
SINGBOX_VERSION="1.13.14"  # keep in sync with VERSION
case "$ARCH" in
  amd64) SINGBOX_SHA256="f48703461a15476951ac4967cdad339d986f4b8096b4eb3ff0829a500502d697";;
  arm64) SINGBOX_SHA256="4742df6a4314e8ecc41736849fca6d73b8f9e91b6e8b06ee794ff17ba180579e";;
esac
SINGBOX_TARBALL="sing-box-${SINGBOX_VERSION}-linux-${ARCH}.tar.gz"
log "installing sing-box ${SINGBOX_VERSION}"
curl -fsSL "https://github.com/SagerNet/sing-box/releases/download/v${SINGBOX_VERSION}/${SINGBOX_TARBALL}" \
  -o "${TMP}/${SINGBOX_TARBALL}"
echo "${SINGBOX_SHA256}  ${TMP}/${SINGBOX_TARBALL}" | sha256sum -c - \
  || fail "sing-box checksum verification failed"
tar -xzf "${TMP}/${SINGBOX_TARBALL}" -C "$TMP"
install -m 0755 "${TMP}/sing-box-${SINGBOX_VERSION}-linux-${ARCH}/sing-box" /usr/local/bin/sing-box
log "installed sing-box ${SINGBOX_VERSION}"

# --- nfqws2 binary (RU role only) ---
if [ "$ROLE" = "ru" ]; then
  NFQWS2_VERSION="v72.12"  # keep in sync with VERSION
  case "$ARCH" in
    amd64) ZARCH="x86_64";;
    arm64) ZARCH="arm64";;
    *) fail "unsupported arch for nfqws2: ${ARCH}";;
  esac
  log "installing nfqws2 ${NFQWS2_VERSION}"
  curl -fsSL "https://github.com/bol-van/zapret/releases/download/${NFQWS2_VERSION}/zapret-${NFQWS2_VERSION}.tar.gz" \
    -o "${TMP}/zapret-${NFQWS2_VERSION}.tar.gz"
  curl -fsSL "https://github.com/bol-van/zapret/releases/download/${NFQWS2_VERSION}/sha256sum.txt" \
    -o "${TMP}/sha256sum.txt"
  NFQWS2_HASH="$(grep "zapret-${NFQWS2_VERSION}.tar.gz" "${TMP}/sha256sum.txt" | awk '{print $1}')"
  [ -n "${NFQWS2_HASH}" ] || fail "nfqws2 hash not found in sha256sum.txt"
  echo "${NFQWS2_HASH}  ${TMP}/zapret-${NFQWS2_VERSION}.tar.gz" | sha256sum -c - \
    || fail "nfqws2 checksum verification failed"
  TMP_NFQWS="${TMP}/nfqws_extract"
  mkdir -p "${TMP_NFQWS}"
  tar -xzf "${TMP}/zapret-${NFQWS2_VERSION}.tar.gz" \
    -C "${TMP_NFQWS}" \
    --strip-components=3 \
    "zapret-${NFQWS2_VERSION}/binaries/linux-${ZARCH}/nfqws"
  install -m0755 "${TMP_NFQWS}/nfqws" /usr/local/bin/nfqws2
  log "installed nfqws2 ${NFQWS2_VERSION}"
fi

# --- state dir + user ---
mkdir -p "$STATE_DIR"; chmod 0700 "$STATE_DIR"
id rdda >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin rdda
log "state dir $STATE_DIR ready; rdda user present"

# --- geoip-ru rule-set (RU role only): a LOCAL .srs so the RU sing-box never
# blocks startup on a remote download. Fetched here at install time (github is
# already required above for sing-box/nfqws); the rendered RU config points at it
# via geoip_path. Update it by re-running the installer.
if [ "$ROLE" = "ru" ]; then
  log "installing geoip-ru rule-set (local split-routing data)"
  curl -fsSL "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-ru.srs" \
    -o "${STATE_DIR}/geoip-ru.srs" || fail "could not download geoip-ru.srs"
  chown rdda:rdda "${STATE_DIR}/geoip-ru.srs"
  chmod 0644 "${STATE_DIR}/geoip-ru.srs"
fi

# --- systemd units (fetched at the resolved tag to match the binary) ---
RAW="https://raw.githubusercontent.com/${REPO}/${TAG}/deploy/systemd"
curl -fsSL "${RAW}/rdda-singbox.service" -o "${UNIT_DIR}/rdda-singbox.service"
if [ "$ROLE" = "eu" ]; then
  curl -fsSL "${RAW}/rdda-sub.service"   -o "${UNIT_DIR}/rdda-sub.service"
  curl -fsSL "${RAW}/rdda-alert.service" -o "${UNIT_DIR}/rdda-alert.service"
  curl -fsSL "${RAW}/rdda-alert.timer"   -o "${UNIT_DIR}/rdda-alert.timer"
fi
if [ "$ROLE" = "ru" ]; then
  curl -fsSL "${RAW}/rdda-nfqws.service"  -o "${UNIT_DIR}/rdda-nfqws.service"
  curl -fsSL "${RAW}/rdda-health.service" -o "${UNIT_DIR}/rdda-health.service"
  curl -fsSL "${RAW}/rdda-health.timer"   -o "${UNIT_DIR}/rdda-health.timer"
fi
systemctl daemon-reload
log "installed systemd units (rdda-sub stays dormant on eu until v0.2)"
if [ "$ROLE" = "ru" ]; then
  RAW_NFT="https://raw.githubusercontent.com/${REPO}/${TAG}/deploy/nftables"
  curl -fsSL "${RAW_NFT}/rdda-nfqws.nft" -o "${STATE_DIR}/rdda-nfqws.nft"
  systemctl enable --now rdda-nfqws
  systemctl enable --now rdda-health.timer
fi
if [ "$ROLE" = "eu" ]; then
  systemctl enable --now rdda-alert.timer
fi

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
  1. rdda init --ru-host <RU_IP> --eu-host <EU_HOST>
  2. rdda render eu > /etc/rdda/singbox.json
  3. chown -R rdda:rdda /etc/rdda
  4. systemctl enable --now rdda-singbox
  5. rdda client add <name>   # send the printed sing-box config privately
  (RU config: run `rdda render ru` and copy the output to the RU node.)
EOF
else
  cat <<'EOF'
  1. On the EU node, run: rdda render ru
  2. Copy that output to this RU node's /etc/rdda/singbox.json
  3. chown -R rdda:rdda /etc/rdda
  4. systemctl enable --now rdda-singbox
EOF
fi
