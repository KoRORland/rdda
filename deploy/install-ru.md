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

### 4.1 Write the control-channel environment file

The pull/health units, the `/etc/sudoers.d/rdda-reload` grant, and the sudoers
validation are all handled by the installer. Only the token-bearing `pull.env`
is left, because it needs the EU pull token — and `rdda control-channel init`
writes it for you (deriving both endpoint URLs from the sub host):

On the **EU** node, print the exact command to run on RU:

    rdda control-channel show

That prints, ready to paste on the **RU** node (via the provider console):

    rdda control-channel init --sub-host <cf-sub-host> --token <pull_token>

`init` writes `/etc/rdda/pull.env` (0600, owned by `rdda`) with
`RDDA_PULL_FROM`, `RDDA_HEALTH_TO`, and `RDDA_PULL_TOKEN`. Replace
`<cf-sub-host>` with the Cloudflare sub hostname configured on the EU node
(e.g. `sub.example.com`).

### 4.2 Enable the timers

The installer already staged `rdda-pull.{service,timer}` and
`rdda-health.{service,timer}` but left them stopped (they need the `pull.env`
from step 4.1). Enable both now:

    sudo systemctl enable --now rdda-pull.timer rdda-health.timer

### 4.3 Verify

Trigger the first pull immediately and check its status:

    sudo systemctl start rdda-pull.service
    sudo systemctl status rdda-pull.service

A successful run exits 0 and logs the fetch URL. The RU node no longer needs
a manual `render ru` copy — the timer keeps `/etc/rdda/singbox.json` in sync.

## 5. Health beat (`rdda-health.timer`) — enabled with the control channel

The installer stages `rdda-health.timer` but leaves it stopped until `pull.env`
exists; you enable it in step 4.2 alongside `rdda-pull.timer`. The timer
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

## Updates & self-heal

**Self-heal (on by default).** `rdda-heal.timer` runs every ~2 minutes and restarts
any RDDA unit that systemd has marked `failed` (the case `Restart=on-failure` does
not cover: a unit that exhausted its start-limit). It never touches a running unit.
Nothing to configure; check it with `systemctl status rdda-heal.timer`.

**Updating the binary.** Update `rdda` to the latest release (verified by checksum):

    rdda update --check     # report: "vX installed, vY available" — no changes
    sudo rdda update        # download, verify, swap, restart rdda-sub

`update` keeps the previous binary at `/usr/local/bin/rdda.prev` and **rolls back
automatically** if the new binary fails to run or `rdda-sub` does not come back up.
If a release runs but misbehaves, revert manually:

    sudo rdda update --rollback

**Auto-update (enabled by the installer).** `rdda-update.timer` runs `rdda update`
on a staggered (randomized) schedule so both nodes track new releases hands-off:

    systemctl status rdda-update.timer     # confirm it's active

It is staggered so a bad release does not hit every node at once.
**Risk:** auto-rollback only catches a *broken* binary — a release that runs but is
subtly wrong will deploy to every node. To opt out and update by hand:
`sudo systemctl disable --now rdda-update.timer` (then use `rdda update` / `--rollback`).

> **Note:** `rdda update` swaps only the **binary**. Changes shipped in systemd
> units or the installer (e.g. new timers, the nfqws fooling flag) require a
> re-run of `install.sh --version <tag>` to pick up.

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

- **Auto-refresh (default):** the installer enables **`rdda-geoip.timer`**, which
  runs `rdda route update-geoip` **weekly** (randomized). It fetches the latest
  rule-set, validates it, and swaps + reloads sing-box **only if the data changed**.
  Fail-safe: any fetch/validation failure keeps the current file and exits cleanly,
  so the node is never left without geoip data and the timer never wedges.
- **Update now (manual):** `sudo rdda route update-geoip` (or re-run the installer).
- **Inspect routing:** `rdda route test <ip|domain> [--trace]` shows whether a
  destination goes **direct** or through the **EU tunnel**, and which rule decided.
- **Disable split-routing** (tunnel everything — safe, less efficient): on the EU
  node, `rdda init … --geoip-path ""` and re-render; the RU config then carries no
  rule-set and needs no `.srs` file.
