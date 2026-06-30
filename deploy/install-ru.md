# RU node setup (Ubuntu 24.04)

The RU node is the in-country entry point. It exposes **only port 443** and runs
no management service. Its sing-box config is produced on the EU node and copied
here.

## 1. Provision with the installer — from the VPS provider console

> Run this from your VPS provider's web/serial **console**, NOT over SSH: the
> installer closes port 22 as its final step, so an SSH session would be cut.

    curl -fsSL https://raw.githubusercontent.com/KoRORland/rdda/main/install.sh | sudo bash -s -- ru

This installs `rdda` + sing-box + the `rdda-singbox` unit, **plus the RU-only
egress DPI-desync** (`nfqws2` + the `rdda-nfqws` unit, **enabled automatically**)
and a **local `geoip-ru.srs`** rule-set at `/etc/rdda/geoip-ru.srs`. It hardens the
host (time sync, automatic security updates) and locks the firewall to **443/tcp
only** (SSH closed). For ongoing maintenance, use the provider console — the node
is designed to run hands-off (auto security updates + systemd restart-on-failure).
(`--keep-ssh` leaves 22 open if you really need it during debugging.)

See section 6 for what `rdda-nfqws` and the local geoip rule-set do.

## 2. Install the config from the EU node

On the **EU** node:

    rdda render ru

Copy that output into this RU node's `/etc/rdda/singbox.json` (paste it via the
console, or use a one-time secure copy — there is no exposed management channel
on the RU node, and automatic pull-sync is a v0.2 feature). Then:

    chown -R rdda:rdda /etc/rdda
    systemctl enable --now rdda-singbox

### REALITY dest check (automatic — can fail the install on purpose)

`rdda-singbox` runs `rdda check-dest` as an `ExecStartPre`, so the
`systemctl enable --now` above **fails if this RU node cannot reach the REALITY
handshake dest over TLS 1.3.** That dest is the `--client-sni` chosen at `rdda init`
on EU (default **`addons.mozilla.org`**); it is both the SNI the inspected
client→RU hop carries *and* the site this node relays the handshake to — if it is
blocked or throttled from inside Russia, no client can connect, so we refuse to
start rather than ship a dead node.

If the start fails, check why:

    rdda check-dest -c /etc/rdda/singbox.json   # prints the dest and the failure

Then pick an SNI that is **reachable and unblocked from this RU node** (verify by
hand with `openssl s_client -connect <host>:443 -servername <host> -tls1_3 </dev/null`),
re-run `rdda init --client-sni <host> …` and `rdda render ru` on EU, re-copy the
config here, and start again. The dest must support TLS 1.3 (the REALITY
requirement) and ideally be a high-collateral site RKN is unlikely to block.

**Soft mode (advisory, non-blocking).** To warn but start anyway — e.g. to tolerate
a transient dest outage — add `--warn` to the check via a systemd drop-in:

    systemctl edit rdda-singbox      # add the two lines below, then save
    # [Service]
    # ExecStartPre=
    # ExecStartPre=/usr/local/bin/rdda check-dest -c /etc/rdda/singbox.json --warn

The first empty `ExecStartPre=` clears the strict check; the second re-adds it in
`--warn` mode (logs a warning to the journal and lets sing-box start).

## 3. After client changes

Whenever you add/remove clients on EU, re-run `rdda render ru` there and
re-copy the output here, then `systemctl restart rdda-singbox`.

In v0.2, client changes are propagated automatically via the pull-sync timer
(see section 4 below) — no manual copy is needed.

## 4. Pull-sync (v0.2)

The pull-sync timer runs `rdda pull` every ~5 minutes, fetching the latest
sing-box config from the EU subscription endpoint and reloading sing-box if the config
changed. This replaces the manual `render ru` + copy workflow.

### 4.1 Write the pull environment file

On the **EU** node, look up the pull token:

    grep pull_token /etc/rdda/config.yaml

Then on the **RU** node (via the provider console):

    sudo tee /etc/rdda/pull.env > /dev/null <<'EOF'
    RDDA_PULL_FROM=https://<cf-sub-host>/ru/config
    RDDA_PULL_TOKEN=<pull_token from EU config.yaml>
    RDDA_HEALTH_TO=https://<cf-sub-host>/ru/health
    EOF
    sudo chmod 600 /etc/rdda/pull.env
    sudo chown rdda:rdda /etc/rdda/pull.env

Replace `<cf-sub-host>` with the Cloudflare sub hostname configured on the EU
node (e.g. `sub.example.com`).

### 4.2 Grant rdda permission to reload sing-box

The pull service runs as the `rdda` user but needs to call
`systemctl reload-or-restart rdda-singbox` after a config change. Create a
sudoers drop-in:

    sudo tee /etc/sudoers.d/rdda-reload > /dev/null <<'EOF'
    rdda ALL=(root) NOPASSWD: /usr/bin/systemctl reload-or-restart rdda-singbox
    EOF
    sudo chmod 440 /etc/sudoers.d/rdda-reload
    sudo visudo -c   # validate syntax

### 4.3 Install and enable the timer

    sudo cp deploy/systemd/rdda-pull.service /etc/systemd/system/
    sudo cp deploy/systemd/rdda-pull.timer   /etc/systemd/system/
    sudo systemctl daemon-reload
    sudo systemctl enable --now rdda-pull.timer

### 4.4 Verify

Trigger the first pull immediately and check its status:

    sudo systemctl start rdda-pull.service
    sudo systemctl status rdda-pull.service

A successful run exits 0 and logs the fetch URL. The RU node no longer needs
a manual `render ru` copy — the timer keeps `/etc/rdda/singbox.json` in sync.

## 5. Health beat (`rdda-health.timer`) — enabled by the installer

The installer enables `rdda-health.timer` on the RU node automatically. The timer
fires at a **randomized interval (1–10 minutes, with random padding)** to avoid a
predictable beacon signal: each beat sends a short POST to `RDDA_HEALTH_TO` (set
in `/etc/rdda/pull.env`), which is the EU node's `/ru/health` endpoint behind the
Cloudflare tunnel. This anti-beacon jitter makes periodic health traffic
indistinguishable from ordinary HTTPS noise.

On the EU node, the beats appear in `rdda status` — showing the RU node's last
beat and its age (e.g. `✓ last beat 90s ago`). If the node goes silent for
>20 min, `rdda status` marks it `STALE`.

To inspect locally: `systemctl status rdda-health.timer` and
`journalctl -u rdda-health.service`.

## 6. Health & diagnostics

Run `rdda doctor` any time to actively check this node: services, the REALITY
dest, the Cloudflare control channel, and (on RU) a real fetch through the
RU→EU tunnel. It exits non-zero if a check fails, so it works in monitoring/cron.

## 7. Egress DPI-desync (`rdda-nfqws`) and local geoip split-routing

The installer sets both of these up on the RU node automatically; this section
explains what they are and how to tune/verify them.

### 7.1 nfqws2 egress desync — **enabled by the installer**

The RU node runs `nfqws2` (zapret2) to DPI-desync RDDA's **own** outbound TLS
handshakes on port 443 (the RU→EU tunnel), so the entry node's egress is harder
to fingerprint. The installer downloads `nfqws2`, installs `rdda-nfqws.service`
+ the nftables hook `/etc/rdda/rdda-nfqws.nft`, and runs `systemctl enable --now
rdda-nfqws`.

- **Fail-open by design.** The nft rule uses `queue ... bypass`: if `nfqws2` is
  down, packets pass through unmodified, so a desync failure never breaks the
  tunnel. (A *misconfigured* desync profile that the path rejects is a different
  matter — pick a profile compatible with your network.)
- **Profile.** The default desync strategy is `fake,split2`. The configured
  strategy is shown by `rdda render nfqws` (it reads the `desync:` block of
  `config.yaml`).
- **Verify:** `systemctl status rdda-nfqws` and `journalctl -u rdda-nfqws`.
- **Disable** (if it interferes on your route): `systemctl disable --now rdda-nfqws`.
  The tunnel keeps working without it.

### 7.2 Local geoip-ru rule-set

To split-route **domestic (RU) traffic direct** and tunnel only the rest, the RU
sing-box uses a geoip-ru rule-set. It is a **local file** at
`/etc/rdda/geoip-ru.srs` (the rendered config's `geoip_path`), fetched by the
installer at install time — **not** a remote rule-set. This matters: sing-box
downloads a *remote* rule-set (blocking) at startup and fails to start if it
cannot reach it, a poor dependency for a censored entry node. A local file means
the data plane always starts offline.

- **Update it:** re-run the installer, or refresh the file:
  `curl -fsSL https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-ru.srs -o /etc/rdda/geoip-ru.srs && systemctl reload-or-restart rdda-singbox`.
- **Disable split-routing** (tunnel everything — safe, less efficient): on the EU
  node, `rdda init … --geoip-path ""` and re-render; the RU config then carries no
  rule-set and needs no `.srs` file.
