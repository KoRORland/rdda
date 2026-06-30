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

To rebuild an EU node (fresh install, then):

    rdda restore rdda-backup.rdda          # refuses to overwrite unless --force

Restore writes `config.yaml` + `clients/` into `/etc/rdda` and chowns them to the
`rdda` user. Use `--force` to overwrite an existing state directory.

## 5. Health & diagnostics

Run `rdda doctor` any time to actively check this node: services, the REALITY
dest, the Cloudflare control channel, and (on RU) a real fetch through the
RU→EU tunnel. It exits non-zero if a check fails, so it works in monitoring/cron.

## 6. Cloudflare Tunnel (v0.2)

This section brings up the Cloudflare tunnel so the subscription endpoint and
the sing-box ingress are reachable without exposing any inbound port to the internet.
After this, **close all inbound firewall ports except 22** — sing-box and the sub
server now listen on loopback only and are reached exclusively via cloudflared.

### 5.1 Install cloudflared

    curl -fsSL https://pkg.cloudflare.com/cloudflare-main.gpg \
        | sudo tee /usr/share/keyrings/cloudflare-main.gpg >/dev/null
    echo "deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] \
        https://pkg.cloudflare.com/cloudflared $(lsb_release -cs) main" \
        | sudo tee /etc/apt/sources.list.d/cloudflared.list
    sudo apt-get update && sudo apt-get install -y cloudflared

### 5.2 Authenticate and create the tunnel

    cloudflared tunnel login          # opens browser; authorizes your Cloudflare account
    cloudflared tunnel create rdda    # note the Tunnel ID and credentials file path printed here

The credentials file is written to `~/.cloudflared/<TUNNEL_ID>.json` (or the
path printed by the command). Record both values — you need them in the next step.

### 5.3 Re-initialize with tunnel flags

Pass the tunnel parameters to `rdda init` (re-run with the existing config or
use `--force` if you already ran init in step 2):

    rdda init \
        --ru-host <RU_IP> --eu-host <EU_HOST> \
        --cf-tunnel-host <tunnel-hostname>.example.com \
        --cf-sub-host    <sub-hostname>.example.com \
        --cf-tunnel-id   <TUNNEL_ID> \
        --cf-credentials-file /etc/cloudflared/<TUNNEL_ID>.json

### 5.4 Write the cloudflared config

    rdda render cloudflared > /etc/cloudflared/config.yml
    chmod 600 /etc/cloudflared/config.yml

### 5.5 Create DNS routes

    cloudflared tunnel route dns rdda <tunnel-hostname>.example.com
    cloudflared tunnel route dns rdda <sub-hostname>.example.com

### 5.6 Create the cloudflared system user and enable the service

    sudo useradd --system --no-create-home --shell /usr/sbin/nologin cloudflared
    sudo cp ~/.cloudflared/<TUNNEL_ID>.json /etc/cloudflared/
    sudo chown -R cloudflared:cloudflared /etc/cloudflared
    sudo cp deploy/systemd/cloudflared.service /etc/systemd/system/
    sudo systemctl daemon-reload
    sudo systemctl enable --now cloudflared

### 5.7 Lock the firewall

With all traffic routed through cloudflared, close every inbound port except SSH:

    sudo ufw delete allow 443/tcp   # or your equivalent firewall rule
    # 22/tcp must stay open for management
    sudo ufw status

Verify the tunnel is up: `cloudflared tunnel info rdda`.
