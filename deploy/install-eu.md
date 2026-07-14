# EU node setup (Ubuntu 24.04)

The EU node is the controller and internet exit. It holds RDDA's state and is
the only node you run `rdda` commands on in v0.1.

## 1. Provision with the installer

Run as root (SSH is fine on EU):

    curl -fsSL https://raw.githubusercontent.com/KoRORland/rdda/main/install.sh | sudo bash -s -- eu

This installs the `rdda` binary (checksum-verified), sing-box, the systemd
units, sets up `/etc/rdda` and the `rdda` user, enables time sync + automatic
security updates, and configures the firewall to allow 443 and 22.

## 2. Initialize state and the EU data plane

    rdda init --ru-host <RU_IP> --eu-host <EU_HOST>
    rdda render eu > /etc/rdda/singbox.json
    chown -R rdda:rdda /etc/rdda
    systemctl enable --now rdda-singbox

`init` generates the REALITY keys, tunnel UUID, and shortIds and writes
`/etc/rdda/config.yaml` — this node is the source of truth.

## 3. Produce the RU node's config

    rdda render ru

Copy that output to the RU node's `/etc/rdda/singbox.json` (see `install-ru.md`).
Re-run and re-copy whenever you add or remove clients (pull-sync is a v0.2
feature).

## 4. Add a client and hand out the link

    rdda client add aunt-olga

This prints a sing-box client config (JSON). Send it to the person over a private
channel (Signal, Telegram, in person); they import it into Hiddify (sing-box core)
and connect. Delivery is out-of-band on purpose (nothing client-facing is exposed
on the EU node).

> **Mux:** the sing-box JSON config already carries the multiplex settings, so
> no manual Mux toggling is needed in the client (unlike a bare `vless://` link).

> The subscription server (`rdda-sub`) is installed but dormant in v0.1; it
> comes online behind Cloudflare in v0.2.

## Backup & restore (EU state)

The EU node holds RDDA's source of truth (`config.yaml` + `clients/`). Back it up
**encrypted** and keep the archive + passphrase somewhere safe — losing the EU
state means re-issuing every client.

    rdda backup --out rdda-backup.rdda     # prompts for a passphrase (entered twice)

The archive is encrypted (argon2id + XChaCha20-Poly1305). **The passphrase cannot
be recovered** — if you lose it, the backup is unreadable. For unattended use, set
`RDDA_BACKUP_PASSPHRASE` or pass `--passphrase-file <file>` instead of the prompt.

**Off-node copy (recommended).** A lost EU node with only a local backup is still a
lost source of truth. `--push` streams the *already-encrypted* archive to a
destination you control (it never leaves the node in the clear):

    rdda backup --push 'rclone rcat remote:rdda/backup.rdda'
    rdda backup --push "ssh backup-host 'cat > /backups/rdda.rdda'"

The push runs before the local write, so a failed upload surfaces loudly instead
of being hidden by a successful local file. Pair it with `rdda-backup`-style cron
for hands-off off-site backups.

To rebuild an EU node (fresh install, then):

    rdda restore rdda-backup.rdda          # refuses to overwrite unless --force

Restore writes `config.yaml` + `clients/` into `/etc/rdda` and chowns them to the
`rdda` user. Use `--force` to overwrite an existing state directory.

## Alerting (email via msmtp)

The EU node can email you when the RU node goes down, an EU service stops, or the
public TLS cert nears expiry. It fires once per transition (and once on recovery).

1. Configure an SMTP relay with msmtp (system-wide `/etc/msmtprc`):

       sudo apt-get install -y msmtp
       # write /etc/msmtprc with your SMTP host/user/password (see msmtp docs)

2. Enable alerting in `/etc/rdda/config.yaml`:

       alert:
         enabled: true
         email: you@example.com
         # command: msmtp        # optional; default
         # cert_warn_days: 14     # optional; default

3. Verify delivery, then let the timer run it every ~5 min:

       rdda alert --test          # sends one test email
       systemctl status rdda-alert.timer

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

**Opt-in auto-update.** A disabled-by-default timer can run `rdda update` for you:

    sudo systemctl enable --now rdda-update.timer

It is staggered (randomized delay) so a bad release does not hit every node at once.
**Risk:** auto-rollback only catches a *broken* binary — a release that runs but is
subtly wrong will deploy to every opted-in node. Leave the timer off if you prefer
to update by hand, and recover with `--rollback` or a pinned re-install.

## 5. Health & diagnostics

Run `rdda doctor` any time to actively check this node: services, the REALITY
dest, the Cloudflare control channel, and (on RU) a real fetch through the
RU→EU tunnel. It exits non-zero if a check fails, so it works in monitoring/cron.

## 6. Cloudflare Tunnel

This section brings up the Cloudflare tunnel so the subscription endpoint and
the sing-box ingress are reachable without exposing any inbound port to the internet.
After this, **close all inbound firewall ports except 22** — sing-box and the sub
server now listen on loopback only and are reached exclusively via cloudflared.

### 6.0 One command (recommended): `rdda cf setup`

After `rdda init` (with `--cf-tunnel-host` / `--cf-sub-host` set, step 2), the
whole bring-up below is a single idempotent command:

    rdda cf setup            # login → create → config → route DNS (verified) → service
    rdda cf setup --dry-run  # preview every step without changing anything

`cf setup` creates or reuses the tunnel, captures the ID + credentials
automatically (no copy/paste), writes the `cloudflare:` block into `config.yaml`,
renders `/etc/cloudflared/config.yml`, and **verifies each `route dns` actually
reaches this tunnel** — refusing to enable the service if a stale DNS record
would leave a hostname serving the wrong origin. Only install cloudflared first
(§6.1); then skip to §6.7 to lock the firewall.

The manual steps §6.1–§6.6 below remain as a reference / fallback.

### 6.1 Install cloudflared

    curl -fsSL https://pkg.cloudflare.com/cloudflare-main.gpg \
        | sudo tee /usr/share/keyrings/cloudflare-main.gpg >/dev/null
    echo "deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] \
        https://pkg.cloudflare.com/cloudflared $(lsb_release -cs) main" \
        | sudo tee /etc/apt/sources.list.d/cloudflared.list
    sudo apt-get update && sudo apt-get install -y cloudflared

### 6.2 Authenticate and create the tunnel

    cloudflared tunnel login          # opens browser; authorizes your Cloudflare account
    cloudflared tunnel create rdda    # note the Tunnel ID and credentials file path printed here

The credentials file is written to `~/.cloudflared/<TUNNEL_ID>.json` (or the
path printed by the command). Record both values — you need them in the next step.

### 6.3 Re-initialize with tunnel flags

Pass the tunnel parameters to `rdda init` (re-run with the existing config or
use `--force` if you already ran init in step 2):

    rdda init \
        --ru-host <RU_IP> --eu-host <EU_HOST> \
        --cf-tunnel-host <tunnel-hostname>.example.com \
        --cf-sub-host    <sub-hostname>.example.com \
        --cf-tunnel-id   <TUNNEL_ID> \
        --cf-credentials-file /etc/cloudflared/<TUNNEL_ID>.json

### 6.4 Write the cloudflared config

The EU installer already created `/etc/cloudflared`; if you're on an older
installer, create it first so the redirect below doesn't fail on a missing dir:

    sudo mkdir -p /etc/cloudflared
    rdda render cloudflared > /etc/cloudflared/config.yml
    sudo chmod 600 /etc/cloudflared/config.yml

### 6.5 Create DNS routes

    cloudflared tunnel route dns rdda <tunnel-hostname>.example.com
    cloudflared tunnel route dns rdda <sub-hostname>.example.com

Each command must log `Added CNAME <host> which will route to this tunnel
tunnelID=<...>`. **If a hostname already has a DNS record** (e.g. you reused an
existing subdomain), `route dns` fails/no-ops and the name keeps serving its old
origin — delete the existing record in the Cloudflare dashboard, then re-run.
Confirm each host actually reaches the sub server (JSON, not a stale page):

    curl -s "https://<sub-hostname>.example.com/ru/config?token=<PULL_TOKEN>" | head -c 40   # expect: {"…

### 6.6 Create the cloudflared system user and enable the service

    sudo useradd --system --no-create-home --shell /usr/sbin/nologin cloudflared
    sudo cp ~/.cloudflared/<TUNNEL_ID>.json /etc/cloudflared/
    sudo chown -R cloudflared:cloudflared /etc/cloudflared
    sudo cp deploy/systemd/cloudflared.service /etc/systemd/system/
    sudo systemctl daemon-reload
    sudo systemctl enable --now cloudflared

### 6.7 Lock the firewall

With all traffic routed through cloudflared, close every inbound port except SSH:

    sudo ufw delete allow 443/tcp   # or your equivalent firewall rule
    # 22/tcp must stay open for management
    sudo ufw status

Verify the tunnel is up: `cloudflared tunnel info rdda`.

> **Cloud security groups (AWS / GCP / Azure / …):** the provider's security group
> is a separate network layer *outside* `ufw`. With Cloudflare fronting, the EU node
> needs **no inbound ports except SSH** — so deny inbound 443 at the security group
> too. Only the direct (non-Cloudflare) fallback needs inbound 443 opened in the
> security group; a missing rule there shows up as a silent `i/o timeout` from the
> RU node even though `ufw` and the service look healthy.
