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

    rdda --dir /etc/rdda render ru

Copy that output into this RU node's `/etc/rdda/xray.json` (paste it via the
console, or use a one-time secure copy — there is no exposed management channel
on the RU node, and automatic pull-sync is a v0.2 feature). Then:

    chown -R rdda:rdda /etc/rdda
    systemctl enable --now rdda-xray

## 3. After client changes

Whenever you add/remove clients on EU, re-run `rdda render ru` there and
re-copy the output here, then `systemctl restart rdda-xray`.
