#!/usr/bin/env bash
# Bridge, boot the five containers with static IPs, apply isolation.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=test/integration/multihost/lib.sh
. "$HERE/lib.sh"

declare -A IP=( [eu]=203.0.113.10 [edge]=203.0.113.20 [ru]=203.0.113.30 [client]=203.0.113.40 [target]=203.0.113.50 )

log "create bridge"
ip link add br-rdda type bridge 2>/dev/null || true
ip addr add 203.0.113.1/24 dev br-rdda 2>/dev/null || true
ip link set br-rdda up

# The CI runner has Docker, whose FORWARD chain has policy drop and only permits
# docker0. Once br_netfilter routes our bridge traffic through FORWARD, Docker
# drops it. Whitelist br-rdda in FORWARD; our inet rdda table still enforces the
# target/EU isolation (a drop in any chain is final; an accept here is not).
iptables -I FORWARD -i br-rdda -j ACCEPT 2>/dev/null || true
iptables -I FORWARD -o br-rdda -j ACCEPT 2>/dev/null || true

# Internet egress for the containers: masquerade bridge traffic leaving for the
# real internet (anything not destined back into the bridge subnet). This makes
# the nodes behave like production — the client->RU REALITY handshake relays to a
# real site (www.apple.com) and sing-box downloads its geoip-ru rule-set. The
# test's container-to-container isolation is enforced by the inet rdda nft rules
# below, NOT by withholding egress.
sysctl -qw net.ipv4.ip_forward=1
iptables -t nat -C POSTROUTING -s 203.0.113.0/24 ! -d 203.0.113.0/24 -j MASQUERADE 2>/dev/null \
  || iptables -t nat -A POSTROUTING -s 203.0.113.0/24 ! -d 203.0.113.0/24 -j MASQUERADE

HOSTS_BLOCK=$'203.0.113.10 eu\n203.0.113.20 edge tunnel.rdda.test sub.rdda.test\n203.0.113.30 ru\n203.0.113.40 client\n203.0.113.50 target target.rdda.test'

for h in eu edge ru client target; do
  root="/var/lib/machines/rdda-$h"
  # Static address on the nspawn veth (host0) via networkd.
  install -D -m0644 /dev/stdin "$root/etc/systemd/network/10-host0.network" <<NET
[Match]
Name=host0
[Network]
Address=${IP[$h]}/24
Gateway=203.0.113.1
DNS=8.8.8.8
NET
  printf '%s\n' "$HOSTS_BLOCK" > "$root/etc/hosts"
  # Give the container a working resolver. nspawn would otherwise copy the
  # runner's /etc/resolv.conf (a 127.0.0.53 systemd-resolved stub that does not
  # run inside the container -> all DNS refused). With --resolv-conf=off below,
  # nspawn leaves this file alone. /etc/hosts still wins for the in-network names.
  rm -f "$root/etc/resolv.conf"
  printf 'nameserver 8.8.8.8\nnameserver 1.1.1.1\n' > "$root/etc/resolv.conf"
  log "boot rdda-$h"
  systemd-nspawn -D "$root" --machine="rdda-$h" \
    --network-bridge=br-rdda --resolv-conf=off --boot --quiet &
done

log "wait for containers to boot"
for h in eu edge ru client target; do
  for _ in $(seq 1 40); do
    systemd-run --machine="rdda-$h" --wait --pipe --quiet true 2>/dev/null && break
    sleep 0.5
  done
done

log "apply isolation (EU zero-inbound; target only from EU)"
# Same-bridge (L2) IPv4 traffic only traverses the inet FORWARD hook when
# br_netfilter is loaded and bridge-nf-call-iptables is on. Without this the
# nft rules below never see container<->container traffic.
modprobe br_netfilter 2>/dev/null || true
sysctl -qw net.bridge.bridge-nf-call-iptables=1
nft -f - <<'NFT'
table inet rdda {
  chain forward {
    type filter hook forward priority 0; policy accept;
    # Established/related always ok (lets EU's outbound chisel + freedom return).
    ct state established,related accept
    # Target reachable ONLY from EU.
    ip daddr 203.0.113.50 ip saddr != 203.0.113.10 drop
    # EU accepts NO new inbound (it only ever dials out).
    ip daddr 203.0.113.10 ct state new drop
  }
}
NFT
log "network up"
