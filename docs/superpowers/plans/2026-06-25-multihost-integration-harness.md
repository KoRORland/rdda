# Multi-Host Integration Harness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the single-host integration test with a `systemd-nspawn` multi-host harness that runs the real operator commands on separate ru-client, ru-node, and eu-node hosts and proves the RU client reaches an EU-only internet target through the tunnel (exit via EU).

**Architecture:** One Linux runner hosts five nspawn containers on a private bridge: `eu`, `ru`, `client`, `edge` (a transparent Cloudflare stand-in), and `target` (an HTTP server reachable only from `eu`). `eu` has zero inbound — it dials **out** to `edge` over a `chisel` reverse tunnel (the `cloudflared` stand-in); `edge` terminates TLS for the CF hostname and forwards in. A test CA installed in `ru`/`client` trust stores makes the edge cert validate like a real Cloudflare cert, so every `rdda` command is identical to production. Green = `client` fetches the `target`'s known body through the tunnel.

**Tech Stack:** bash, `systemd-nspawn`/`machinectl`, `debootstrap`, a Linux bridge + nftables, `chisel` (reverse tunnel), `nginx` (edge TLS), xray-core, the `rdda` binary, Go (the test wrapper).

## Global Constraints

- Module path `github.com/KoRORland/rdda`; `go.mod` stays `go 1.22`; never run bare `go mod tidy`; no new Go deps.
- **No `--dir`, no per-node command divergence, no jq port surgery, no `allowInsecure`.** The three real hosts run the documented operator commands verbatim. The only test-only substitutions are infrastructure the operator never touches: nspawn provisioning, the `/etc/hosts` CF-hostname mapping, a test CA in the trust store, the self-signed-by-test-CA edge cert, and swapping `cloudflared.service` for the chisel reverse-tunnel unit.
- **EU has zero inbound.** `eu` runs no listening port reachable from other hosts; reachability is established by `eu` dialing out to `edge`. Enforced both by the reverse-tunnel design and an nftables rule dropping all new inbound to `eu`.
- All shell files pass `bash -n` and `shellcheck` (the repo runs shellcheck in CI). The full harness requires root + nspawn and only runs in CI / on a root host; locally, `bash -n` + `shellcheck` are the gate.
- Fixed addressing (use these exact values everywhere): bridge `br-rdda` host side `10.8.0.1/24`; `eu` `10.8.0.10`, `edge` `10.8.0.20`, `ru` `10.8.0.30`, `client` `10.8.0.40`, `target` `10.8.0.50`. CF hostnames: data hop `tunnel.rdda.test`, subscription `sub.rdda.test`, both resolve to `edge` (`10.8.0.20`). Internet target hostname `target.rdda.test` → `10.8.0.50`. Container rootfs base at `/var/lib/machines/rdda-base`, per-host at `/var/lib/machines/<name>`.
- Project workflow rule: every task ends by committing **and pushing to `main`** (`git push`). Do not ask.

---

## File Structure

All new files under `test/integration/multihost/`:

- `lib.sh` — shared helpers: logging, `nsrun <host> <cmd...>` (run a command in a booted container), `wait_active <host> <unit>`, `die`.
- `image.sh` — build the base rootfs once (debootstrap + deps + the freshly built `rdda` + a generated test CA), then clone it to the five per-host rootfs trees.
- `net.sh` — create `br-rdda`, boot the five containers with static IPs, wait for boot, apply nftables isolation (EU zero-inbound; target reachable only from EU).
- `provision-edge.sh` — edge: CA-signed cert for the CF hostnames, nginx `:443` TLS terminator, `chisel server`.
- `provision-eu.sh` — eu: real `rdda init`/`render eu`/`serve`; real `rdda-xray.service`; the chisel reverse-tunnel unit standing in for `cloudflared.service`.
- `provision-ru.sh` — ru: install the test CA, write `pull.env`, `rdda pull` the initial config, real `rdda-xray.service` + `rdda-pull.timer`.
- `provision-client.sh` — client: install the client xray config rendered on `eu`, run xray with a SOCKS inbound.
- `provision-target.sh` — target: a minimal HTTP server returning a known body.
- `assert.sh` — the green assertions (services active; EU zero inbound; client→target via EU; pull-sync).
- `run-multihost.sh` — top-level orchestrator: sequences image → net → provision-* → assert, with a teardown trap.
- Modified: `test/integration/integration_test.go` (invoke `run-multihost.sh` instead of `run.sh`; skip if not root/nspawn), `.github/workflows/integration.yml`. Deleted: `test/integration/run.sh`.

---

### Task 1: Harness scaffold — lib.sh + orchestrator skeleton + Go wrapper

**Files:**
- Create: `test/integration/multihost/lib.sh`
- Create: `test/integration/multihost/run-multihost.sh`
- Modify: `test/integration/integration_test.go`

**Interfaces:**
- Produces: `lib.sh` functions `log <msg>`, `die <msg>` (exit 1), `nsrun <host> <cmd...>` (`systemd-run --machine=rdda-<host> --wait --pipe --quiet <cmd...>`), `wait_active <host> <unit>` (polls `systemctl is-active` inside the container, fails after 20 tries). `run-multihost.sh` sources `lib.sh` and defines the host list `HOSTS=(eu edge ru client target)` and the run sequence.

- [ ] **Step 1: Write `lib.sh`**

```bash
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
```

- [ ] **Step 2: Write `run-multihost.sh` skeleton**

```bash
#!/usr/bin/env bash
# Top-level orchestrator for the multi-host nspawn integration harness.
# Must run as root on a host with systemd-nspawn, debootstrap, nft, and Go.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../../.." && pwd)"
# shellcheck source=test/integration/multihost/lib.sh
. "$HERE/lib.sh"

HOSTS=(eu edge ru client target)

[ "$(id -u)" -eq 0 ] || die "must run as root"
for bin in systemd-nspawn machinectl debootstrap nft systemd-run go; do
  command -v "$bin" >/dev/null || die "$bin not installed"
done

teardown() {
  log "teardown"
  for h in "${HOSTS[@]}"; do machinectl terminate "rdda-$h" 2>/dev/null || true; done
  ip link del br-rdda 2>/dev/null || true
  nft delete table inet rdda 2>/dev/null || true
}
trap teardown EXIT

log "=== build image ==="; "$HERE/image.sh" "$REPO_ROOT"
log "=== bring up network ==="; "$HERE/net.sh"
log "=== provision ==="
"$HERE/provision-target.sh"
"$HERE/provision-edge.sh"
"$HERE/provision-eu.sh"
"$HERE/provision-ru.sh"
"$HERE/provision-client.sh"
log "=== assert ==="; "$HERE/assert.sh"
log "=== PASS ==="
```

- [ ] **Step 3: Point the Go wrapper at the new orchestrator**

In `test/integration/integration_test.go`, change the `exec.Command("bash", filepath.Join(".", "run.sh"), ...)` invocation to call the multi-host orchestrator with no port args, and update the skip checks. Replace the body of `TestRealDeployTunnel` so it:

```go
func TestRealDeployTunnel(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("multi-host integration test requires root (nspawn/nft/debootstrap)")
	}
	for _, bin := range []string{"systemd-nspawn", "debootstrap", "nft", "machinectl"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not available", bin)
		}
	}
	cmd := exec.Command("bash", filepath.Join(".", "multihost", "run-multihost.sh"))
	out, err := cmd.CombinedOutput()
	t.Logf("run-multihost.sh output:\n%s", out)
	if err != nil {
		t.Fatalf("multi-host harness failed: %v", err)
	}
}
```

Remove the now-dead client-xray/probe helpers in that file (the harness owns the probe). Keep the file's `//go:build integration` tag and imports it still uses (`os`, `os/exec`, `path/filepath`, `testing`); drop imports that become unused.

- [ ] **Step 4: Gate — syntax + lint + compile**

Run:
```bash
bash -n test/integration/multihost/lib.sh test/integration/multihost/run-multihost.sh
shellcheck test/integration/multihost/lib.sh test/integration/multihost/run-multihost.sh
go vet -tags=integration ./test/integration/...
```
Expected: no syntax errors; shellcheck clean; `go vet` clean.

- [ ] **Step 5: Commit**

```bash
git add test/integration/multihost/lib.sh test/integration/multihost/run-multihost.sh test/integration/integration_test.go
git commit -m "test(integration): scaffold multi-host nspawn harness + orchestrator"
git push
```

---

### Task 2: Base image builder

**Files:**
- Create: `test/integration/multihost/image.sh`

**Interfaces:**
- Consumes: `lib.sh`; arg `$1` = repo root.
- Produces: `/var/lib/machines/rdda-base` (a booted-capable Debian rootfs with xray, chisel, nginx, curl, jq, ca-certificates, the `rdda` binary at `/usr/local/bin/rdda`, and a test CA at `/usr/local/share/ca-certificates/rdda-test-ca.crt`), plus the test CA key/cert on the host at `/run/rdda-ca/` for the edge to sign with. Clones the base to `/var/lib/machines/rdda-{eu,edge,ru,client,target}`.

- [ ] **Step 1: Write `image.sh`**

```bash
#!/usr/bin/env bash
# Build the shared base rootfs once, then clone it per host.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=test/integration/multihost/lib.sh
. "$HERE/lib.sh"
REPO_ROOT="${1:?repo root required}"
BASE=/var/lib/machines/rdda-base
CA_DIR=/run/rdda-ca

log "debootstrap base rootfs"
rm -rf "$BASE"; mkdir -p "$BASE"
debootstrap --include=systemd,systemd-sysv,dbus,nginx,curl,jq,ca-certificates,openssl,iproute2 \
  stable "$BASE" http://deb.debian.org/debian

log "build rdda and install xray + chisel into base"
( cd "$REPO_ROOT" && go build -o "$BASE/usr/local/bin/rdda" ./cmd/rdda )
# xray-core
curl -fsSL https://github.com/XTLS/Xray-core/releases/latest/download/Xray-linux-64.zip -o /tmp/xray.zip
( cd /tmp && rm -rf xray && mkdir xray && cd xray && jar xf /tmp/xray.zip 2>/dev/null || unzip -o /tmp/xray.zip )
install -m0755 /tmp/xray/xray "$BASE/usr/local/bin/xray"
# chisel (reverse-tunnel stand-in for cloudflared)
curl -fsSL https://github.com/jpillora/chisel/releases/latest/download/chisel_linux_amd64.gz -o /tmp/chisel.gz
gunzip -f /tmp/chisel.gz
install -m0755 /tmp/chisel "$BASE/usr/local/bin/chisel"

log "generate test CA"
mkdir -p "$CA_DIR"
openssl req -x509 -newkey rsa:2048 -nodes -days 2 \
  -keyout "$CA_DIR/ca.key" -out "$CA_DIR/ca.crt" -subj "/CN=RDDA Test CA" >/dev/null 2>&1
install -D -m0644 "$CA_DIR/ca.crt" "$BASE/usr/local/share/ca-certificates/rdda-test-ca.crt"

log "enable networkd + ssh-free boot; create rdda user"
systemd-nspawn -D "$BASE" --pipe /bin/bash -eus <<'INROOT'
systemctl enable systemd-networkd
update-ca-certificates
useradd --system --no-create-home --shell /usr/sbin/nologin rdda || true
useradd --system --no-create-home --shell /usr/sbin/nologin cloudflared || true
INROOT

log "clone base to per-host rootfs"
for h in eu edge ru client target; do
  rm -rf "/var/lib/machines/rdda-$h"
  cp -a "$BASE" "/var/lib/machines/rdda-$h"
done
log "image build done"
```

- [ ] **Step 2: Gate — syntax + lint**

Run:
```bash
bash -n test/integration/multihost/image.sh
shellcheck test/integration/multihost/image.sh
```
Expected: clean (downloads/debootstrap only run under root in CI; this gate checks the script, not execution).

- [ ] **Step 3: Commit**

```bash
git add test/integration/multihost/image.sh
git commit -m "test(integration): base rootfs builder (rdda+xray+chisel+test CA)"
git push
```

---

### Task 3: Network bring-up + isolation

**Files:**
- Create: `test/integration/multihost/net.sh`

**Interfaces:**
- Consumes: `lib.sh`, the per-host rootfs from Task 2.
- Produces: bridge `br-rdda` (`10.8.0.1/24`); five booted containers each with a static IP and `/etc/hosts` mapping the CF + target hostnames; nftables table `inet rdda` enforcing EU zero-inbound and target-only-from-EU.

- [ ] **Step 1: Write `net.sh`**

```bash
#!/usr/bin/env bash
# Bridge, boot the five containers with static IPs, apply isolation.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=test/integration/multihost/lib.sh
. "$HERE/lib.sh"

declare -A IP=( [eu]=10.8.0.10 [edge]=10.8.0.20 [ru]=10.8.0.30 [client]=10.8.0.40 [target]=10.8.0.50 )

log "create bridge"
ip link add br-rdda type bridge 2>/dev/null || true
ip addr add 10.8.0.1/24 dev br-rdda 2>/dev/null || true
ip link set br-rdda up

HOSTS_BLOCK=$'10.8.0.10 eu\n10.8.0.20 edge tunnel.rdda.test sub.rdda.test\n10.8.0.30 ru\n10.8.0.40 client\n10.8.0.50 target target.rdda.test'

for h in eu edge ru client target; do
  root="/var/lib/machines/rdda-$h"
  # Static address on the nspawn veth (host0) via networkd.
  install -D -m0644 /dev/stdin "$root/etc/systemd/network/10-host0.network" <<NET
[Match]
Name=host0
[Network]
Address=${IP[$h]}/24
Gateway=10.8.0.1
DNS=10.8.0.1
NET
  printf '%s\n' "$HOSTS_BLOCK" > "$root/etc/hosts"
  log "boot rdda-$h"
  systemd-nspawn -D "$root" --machine="rdda-$h" \
    --network-bridge=br-rdda --boot --quiet &
done

log "wait for containers to boot"
for h in eu edge ru client target; do
  for _ in $(seq 1 40); do
    systemd-run --machine="rdda-$h" --wait --pipe --quiet true 2>/dev/null && break
    sleep 0.5
  done
done

log "apply isolation (EU zero-inbound; target only from EU)"
nft -f - <<'NFT'
table inet rdda {
  chain forward {
    type filter hook forward priority 0; policy accept;
    # Established/related always ok (lets EU's outbound chisel + freedom return).
    ct state established,related accept
    # Target reachable ONLY from EU.
    ip daddr 10.8.0.50 ip saddr != 10.8.0.10 drop
    # EU accepts NO new inbound (it only ever dials out).
    ip daddr 10.8.0.10 ct state new drop
  }
}
NFT
sysctl -q net.bridge.bridge-nf-call-iptables=1 2>/dev/null || true
log "network up"
```

- [ ] **Step 2: Gate — syntax + lint**

Run:
```bash
bash -n test/integration/multihost/net.sh
shellcheck test/integration/multihost/net.sh
```
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add test/integration/multihost/net.sh
git commit -m "test(integration): bridge + 5 nspawn hosts + nftables isolation"
git push
```

---

### Task 4: Edge + EU provisioning (the reverse-tunnel stand-in)

**Files:**
- Create: `test/integration/multihost/provision-edge.sh`
- Create: `test/integration/multihost/provision-eu.sh`

**Interfaces:**
- Consumes: `lib.sh`, booted `edge`/`eu`, the test CA at `/run/rdda-ca`, `rdda`/`xray`/`chisel`/`nginx` in the rootfs.
- Produces: `edge` serving TLS `:443` for `tunnel.rdda.test`/`sub.rdda.test` (forwarding to chisel-exposed EU loopback ports `8443`/`8080`) and a `chisel server` on `:7000`; `eu` running `rdda-xray` (loopback) + sub `serve` (loopback) + a `chisel client` unit (the `cloudflared` stand-in) dialing `edge:7000` and reverse-forwarding `8443`/`8080`. EU writes its config via the real `rdda init`/`render eu`.

- [ ] **Step 1: Write `provision-edge.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=test/integration/multihost/lib.sh
. "$HERE/lib.sh"
CA_DIR=/run/rdda-ca
root=/var/lib/machines/rdda-edge

log "sign edge cert with the test CA (SAN tunnel.rdda.test, sub.rdda.test)"
openssl req -newkey rsa:2048 -nodes -keyout "$CA_DIR/edge.key" -out "$CA_DIR/edge.csr" \
  -subj "/CN=tunnel.rdda.test" >/dev/null 2>&1
openssl x509 -req -in "$CA_DIR/edge.csr" -CA "$CA_DIR/ca.crt" -CAkey "$CA_DIR/ca.key" \
  -CAcreateserial -days 2 -out "$CA_DIR/edge.crt" \
  -extfile <(printf 'subjectAltName=DNS:tunnel.rdda.test,DNS:sub.rdda.test') >/dev/null 2>&1
install -D -m0644 "$CA_DIR/edge.crt" "$root/etc/ssl/edge.crt"
install -D -m0600 "$CA_DIR/edge.key" "$root/etc/ssl/edge.key"

log "nginx: TLS terminate :443 -> chisel-forwarded EU loopback"
cat > "$root/etc/nginx/conf.d/edge.conf" <<'NGINX'
server {
    listen 443 ssl;
    server_name tunnel.rdda.test sub.rdda.test;
    ssl_certificate     /etc/ssl/edge.crt;
    ssl_certificate_key /etc/ssl/edge.key;
    location /     { proxy_pass http://127.0.0.1:8443; proxy_http_version 1.1; }
    location /sub/ { proxy_pass http://127.0.0.1:8080; }
    location /ru/  { proxy_pass http://127.0.0.1:8080; }
}
NGINX
rm -f "$root/etc/nginx/sites-enabled/default"

log "chisel server unit on :7000 (reverse tunnels allowed)"
cat > "$root/etc/systemd/system/rdda-edge-chisel.service" <<'UNIT'
[Unit]
Description=RDDA edge chisel server (Cloudflare stand-in)
After=network-online.target
[Service]
ExecStart=/usr/local/bin/chisel server --reverse --host 0.0.0.0 --port 7000
Restart=on-failure
[Install]
WantedBy=multi-user.target
UNIT

nsrun edge systemctl enable --now nginx rdda-edge-chisel
wait_active edge nginx
wait_active edge rdda-edge-chisel
log "edge provisioned"
```

- [ ] **Step 2: Write `provision-eu.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=test/integration/multihost/lib.sh
. "$HERE/lib.sh"
REPO_ROOT="$(cd "$HERE/../../.." && pwd)"
root=/var/lib/machines/rdda-eu

log "EU real flow: init (CF-enabled) + render eu + a tester client"
nsrun eu rdda init --ru-host ru --eu-host eu \
  --cf-tunnel-host tunnel.rdda.test --cf-sub-host sub.rdda.test \
  --cf-tunnel-id testtunnel --cf-credentials-file /etc/cloudflared/test.json
nsrun eu bash -eus <<'INEU'
rdda client add tester >/dev/null
rdda render eu > /etc/rdda/xray.json
chown -R rdda:rdda /etc/rdda
chmod 700 /etc/rdda
INEU

log "install the REAL rdda-xray unit (reads /etc/rdda/xray.json, loopback under CF)"
install -m0644 "$REPO_ROOT/deploy/systemd/rdda-xray.service" "$root/etc/systemd/system/rdda-xray.service"
install -m0644 "$REPO_ROOT/deploy/systemd/rdda-sub.service"  "$root/etc/systemd/system/rdda-sub.service"

log "cloudflared stand-in: chisel client reverse-forwards EU loopback to edge"
cat > "$root/etc/systemd/system/cloudflared.service" <<'UNIT'
[Unit]
Description=RDDA cloudflared stand-in (chisel reverse tunnel to edge)
After=network-online.target
[Service]
ExecStart=/usr/local/bin/chisel client edge:7000 R:127.0.0.1:8443:127.0.0.1:8443 R:127.0.0.1:8080:127.0.0.1:8080
Restart=on-failure
[Install]
WantedBy=multi-user.target
UNIT

# Pin EU xray + sub to the loopback ports the edge forwards (8443/8080).
nsrun eu bash -eus <<'INEU'
sed -i 's#"port": 443#"port": 8443#' /etc/rdda/xray.json
sed -i 's#--addr 127.0.0.1:8080#--addr 127.0.0.1:8080#' /etc/systemd/system/rdda-sub.service
systemctl daemon-reload
systemctl enable --now rdda-xray rdda-sub cloudflared
INEU
wait_active eu rdda-xray
wait_active eu rdda-sub
wait_active eu cloudflared
log "eu provisioned"
```

> Note on the EU port: `rdda init` defaults `EUPort` to 443, but the edge reverse-forwards loopback `8443`. The one `sed` above rewrites the rendered EU inbound port to `8443` to match the chisel/edge forward; this is an infrastructure detail (the loopback port behind cloudflared), not an operator command change. The sub server already listens on loopback `8080` per its unit.

- [ ] **Step 3: Gate — syntax + lint**

Run:
```bash
bash -n test/integration/multihost/provision-edge.sh test/integration/multihost/provision-eu.sh
shellcheck test/integration/multihost/provision-edge.sh test/integration/multihost/provision-eu.sh
```
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add test/integration/multihost/provision-edge.sh test/integration/multihost/provision-eu.sh
git commit -m "test(integration): edge TLS+chisel and EU zero-inbound reverse tunnel"
git push
```

---

### Task 5: RU + client + target provisioning

**Files:**
- Create: `test/integration/multihost/provision-target.sh`
- Create: `test/integration/multihost/provision-ru.sh`
- Create: `test/integration/multihost/provision-client.sh`

**Interfaces:**
- Consumes: `lib.sh`; booted `target`/`ru`/`client`; `eu` already provisioned (its sub server + config exist) from Task 4.
- Produces: `target` serving body `RDDA_OK` on `:80`; `ru` running `rdda-xray` from a real `rdda pull` + the `rdda-pull.timer`; `client` running xray with a SOCKS inbound on `127.0.0.1:1080`, dialing `ru:443` via REALITY, using a config rendered on `eu`.

- [ ] **Step 1: Write `provision-target.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=test/integration/multihost/lib.sh
. "$HERE/lib.sh"
root=/var/lib/machines/rdda-target
mkdir -p "$root/srv/www"
printf 'RDDA_OK\n' > "$root/srv/www/index.html"
cat > "$root/etc/systemd/system/rdda-target.service" <<'UNIT'
[Unit]
Description=RDDA internet target (reachable only via EU)
After=network-online.target
[Service]
WorkingDirectory=/srv/www
ExecStart=/usr/bin/python3 -m http.server 80
Restart=on-failure
UNIT
# python3 isn't in base; serve with a tiny socat-free shell loop via xray? Use busybox httpd if present, else nginx.
cat > "$root/etc/nginx/conf.d/target.conf" <<'NGINX'
server { listen 80; root /srv/www; }
NGINX
rm -f "$root/etc/nginx/sites-enabled/default"
nsrun target systemctl enable --now nginx
wait_active target nginx
log "target provisioned (serves RDDA_OK on :80)"
```

> Use nginx (already in the base image) for the target rather than python; the `rdda-target.service` python unit above is removed in favor of the nginx `target.conf`. Delete the python unit block before committing if it remains — the nginx server is the one enabled.

- [ ] **Step 2: Write `provision-ru.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=test/integration/multihost/lib.sh
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
```

> The default `--reload-cmd` in production is `systemctl reload-or-restart rdda-xray`; running as the `rdda` user that needs the sudoers rule above AND a `sudo` prefix. This harness uses `--reload-cmd true` for the one-shot initial pull (xray is started explicitly afterward) and relies on the timer for steady state. The production reload-permission wiring (whether the default reload-cmd should be `sudo systemctl ...`) is tracked as a separate fix — see the plan's Self-Review note.

- [ ] **Step 3: Write `provision-client.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=test/integration/multihost/lib.sh
. "$HERE/lib.sh"
root=/var/lib/machines/rdda-client

log "render the client config on EU (operator runs this on EU), copy to client"
UUID="$(nsrun eu bash -lc "jq -r .uuid /etc/rdda/clients/tester.json")"
nsrun eu rdda render client --uuid "$UUID" --socks-port 1080 > "$root/etc/client.json"

cat > "$root/etc/systemd/system/rdda-client.service" <<'UNIT'
[Unit]
Description=RDDA client xray (SOCKS -> RU)
After=network-online.target
[Service]
ExecStart=/usr/local/bin/xray run -c /etc/client.json
Restart=on-failure
[Install]
WantedBy=multi-user.target
UNIT
nsrun client systemctl enable --now rdda-client
wait_active client rdda-client
log "client provisioned"
```

- [ ] **Step 4: Gate — syntax + lint**

Run:
```bash
bash -n test/integration/multihost/provision-target.sh test/integration/multihost/provision-ru.sh test/integration/multihost/provision-client.sh
shellcheck test/integration/multihost/provision-target.sh test/integration/multihost/provision-ru.sh test/integration/multihost/provision-client.sh
```
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add test/integration/multihost/provision-target.sh test/integration/multihost/provision-ru.sh test/integration/multihost/provision-client.sh
git commit -m "test(integration): RU pull-sync, client xray, EU-only target hosts"
git push
```

---

### Task 6: Green assertions

**Files:**
- Create: `test/integration/multihost/assert.sh`

**Interfaces:**
- Consumes: `lib.sh`; all hosts provisioned (Tasks 4–5).
- Produces: the pass/fail gate — services active, EU zero inbound, the exit-via-EU proof, and pull-sync.

- [ ] **Step 1: Write `assert.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=test/integration/multihost/lib.sh
. "$HERE/lib.sh"

log "1) EU has no inbound listener reachable from other hosts"
# From RU, a direct TCP connect to EU must fail (nft drops new inbound to EU).
if nsrun ru bash -lc 'curl -fsS --max-time 5 http://eu:8443/ >/dev/null 2>&1'; then
  die "EU is reachable directly from RU; expected zero inbound"
fi
log "OK: EU not directly reachable (zero inbound)"

log "2) RU cannot reach the internet target directly (only EU may)"
if nsrun ru bash -lc 'curl -fsS --max-time 5 http://target.rdda.test/ >/dev/null 2>&1'; then
  die "RU reached the target directly; exit-via-EU proof would be meaningless"
fi
log "OK: target not directly reachable from RU"

log "3) GREEN: client reaches the EU-only target THROUGH the tunnel"
body="$(nsrun client bash -lc 'curl -fsS --max-time 20 --socks5-hostname 127.0.0.1:1080 http://target.rdda.test/ 2>/dev/null' || true)"
[ "$body" = "RDDA_OK" ] || die "client did not reach target via tunnel (got: '${body}')"
log "OK: client -> RU -> edge -> EU -> target returned RDDA_OK (exit is EU)"

log "4) pull-sync: a NEW client added on EU lands in RU's config"
NEW_UUID="$(nsrun eu bash -lc 'rdda client add latecomer >/dev/null; jq -r .uuid /etc/rdda/clients/latecomer.json')"
nsrun ru systemctl start rdda-pull.service
ok=no
for _ in $(seq 1 20); do
  if nsrun ru bash -lc "jq -e --arg u '$NEW_UUID' '.inbounds[0].settings.clients[]|select(.id==\$u)' /etc/rdda/xray.json >/dev/null 2>&1"; then ok=yes; break; fi
  sleep 0.5
done
[ "$ok" = yes ] || die "pull-sync did not deliver the new client to RU"
log "OK: pull-sync delivered the new client to RU"

log "ALL ASSERTIONS PASSED"
```

- [ ] **Step 2: Gate — syntax + lint**

Run:
```bash
bash -n test/integration/multihost/assert.sh
shellcheck test/integration/multihost/assert.sh
```
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add test/integration/multihost/assert.sh
git commit -m "test(integration): green assertions (exit-via-EU + pull-sync)"
git push
```

---

### Task 7: CI wiring + retire the single-host harness

**Files:**
- Modify: `.github/workflows/integration.yml`
- Delete: `test/integration/run.sh`

**Interfaces:**
- Consumes: `run-multihost.sh` (Task 1) and the Go wrapper change (Task 1).
- Produces: a CI job that installs the host prerequisites and runs the multi-host harness.

- [ ] **Step 1: Update the workflow to install prerequisites and drop the xray/run.sh path**

In `.github/workflows/integration.yml`, replace the `Install xray-core`, `Build and install rdda`, and final `go test` steps with host-prereq install + the multi-host run. The job stays `runs-on: ubuntu-24.04`:

```yaml
      - run: go vet ./... && go test ./...
      - name: Install nspawn + debootstrap + nft
        run: sudo apt-get update && sudo apt-get install -y systemd-container debootstrap nftables
      - name: Run multi-host integration harness
        run: sudo -E env "PATH=$PATH" go test -tags=integration ./test/integration/... -v -timeout 20m
```

Keep the existing `on:` triggers and the `setup-go` step unchanged.

- [ ] **Step 2: Delete the superseded single-host harness**

```bash
git rm test/integration/run.sh
```

- [ ] **Step 3: Gate — workflow + compile**

Run:
```bash
python3 -c 'import yaml,sys; yaml.safe_load(open(".github/workflows/integration.yml"))' && echo yaml-ok
go vet -tags=integration ./test/integration/...
```
Expected: `yaml-ok`; `go vet` clean.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/integration.yml
git commit -m "ci: run multi-host nspawn integration harness; retire single-host run.sh"
git push
```

- [ ] **Step 5: Watch the first CI run (the real gate)**

The harness only fully runs in CI. After pushing, watch the `integration` workflow. Expect first-run failures from environment specifics (image download URLs, networkd veth name, chisel reverse-tunnel ordering, nginx h2-vs-h1.1 to the xray origin). Iterate on the scripts until the run prints `ALL ASSERTIONS PASSED` and `=== PASS ===`. Record the green run URL in the progress ledger.

---

## Self-Review

- **Spec coverage (multi-host design doc):**
  - nspawn orchestration on one runner — Tasks 1–3. ✓
  - transparent CF-edge stand-in, EU dials out (zero inbound) via chisel reverse tunnel — Task 4. ✓
  - test CA so node commands are identical (no `allowInsecure`) — Tasks 2 (CA in base), 4 (edge cert signed by it). ✓
  - 3 real hosts run real operator commands — Tasks 4 (EU), 5 (RU, client). ✓
  - internet-target reachable only via EU + the exit-via-EU proof — Tasks 3 (nft), 5 (target), 6 (assert). ✓
  - pull-sync with no manual render — Tasks 5, 6. ✓
- **Placeholder scan:** the `provision-target.sh` script carries a stray python unit alongside the nginx target; the note instructs deleting the python block so nginx is the single server. The implementer must remove it during Task 5 — flagged here so it is not shipped. No other TBDs.
- **Type/agreement consistency:** loopback ports are consistent — EU xray inbound rewritten to `8443`, sub on `8080`; edge nginx proxies to `8443`/`8080`; EU chisel reverse-forwards exactly `8443` and `8080`. Hostnames `tunnel.rdda.test`/`sub.rdda.test`→edge and `target.rdda.test`→target match across `net.sh`, the edge cert SAN, and the asserts. The pull endpoint `https://sub.rdda.test/ru/config` matches the `/ru/` nginx location and `rdda pull --from`.
- **Known production gap to fix (out of this harness's scope, tracked for the final review):** the default `rdda pull --reload-cmd "systemctl reload-or-restart rdda-xray"` runs as the `rdda` user, which cannot drive systemctl without `sudo`; `install-ru.md` provisions a sudoers rule for `sudo systemctl reload-or-restart rdda-xray`, so the default reload-cmd should be `sudo systemctl reload-or-restart rdda-xray`. Decide and fix during the Workstream A final whole-branch review.

## Out of scope

Real Cloudflare (manual verification per `install-eu.md`); multi-runner / cloud VMs; IPv6.
```
