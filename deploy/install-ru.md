# RU node setup (Ubuntu 24.04)

The RU node is the in-country entry point. It exposes **only port 443** and runs
no management service. Its xray config is produced on the EU node and copied
here.

## 1. Provision with the installer — from the VPS provider console

> Run this from your VPS provider's web/serial **console**, NOT over SSH: the
> installer closes port 22 as its final step, so an SSH session would be cut.

    curl -fsSL https://raw.githubusercontent.com/KoRORland/rdda/main/install.sh | sudo bash -s -- ru

This installs `rdda` + xray-core + the `rdda-xray` unit, hardens the host (time
sync, automatic security updates), and locks the firewall to **443/tcp only**
(SSH closed). For ongoing maintenance, use the provider console — the node is
designed to run hands-off (auto security updates + systemd restart-on-failure).
(`--keep-ssh` leaves 22 open if you really need it during debugging.)

## 2. Install the config from the EU node

On the **EU** node:

    rdda render ru

Copy that output into this RU node's `/etc/rdda/xray.json` (paste it via the
console, or use a one-time secure copy — there is no exposed management channel
on the RU node, and automatic pull-sync is a v0.2 feature). Then:

    chown -R rdda:rdda /etc/rdda
    systemctl enable --now rdda-xray

## 3. After client changes

Whenever you add/remove clients on EU, re-run `rdda render ru` there and
re-copy the output here, then `systemctl restart rdda-xray`.

In v0.2, client changes are propagated automatically via the pull-sync timer
(see section 4 below) — no manual copy is needed.

## 4. Pull-sync (v0.2)

The pull-sync timer runs `rdda pull` every ~5 minutes, fetching the latest
xray config from the EU subscription endpoint and reloading xray if the config
changed. This replaces the manual `render ru` + copy workflow.

### 4.1 Write the pull environment file

On the **EU** node, look up the pull token:

    grep pull_token /etc/rdda/config.yaml

Then on the **RU** node (via the provider console):

    sudo tee /etc/rdda/pull.env > /dev/null <<'EOF'
    RDDA_PULL_FROM=https://<cf-sub-host>/ru/config
    RDDA_PULL_TOKEN=<pull_token from EU config.yaml>
    EOF
    sudo chmod 600 /etc/rdda/pull.env
    sudo chown rdda:rdda /etc/rdda/pull.env

Replace `<cf-sub-host>` with the Cloudflare sub hostname configured on the EU
node (e.g. `sub.example.com`).

### 4.2 Grant rdda permission to reload xray

The pull service runs as the `rdda` user but needs to call
`systemctl reload-or-restart rdda-xray` after a config change. Create a
sudoers drop-in:

    sudo tee /etc/sudoers.d/rdda-reload > /dev/null <<'EOF'
    rdda ALL=(root) NOPASSWD: /usr/bin/systemctl reload-or-restart rdda-xray
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
a manual `render ru` copy — the timer keeps `/etc/rdda/xray.json` in sync.
