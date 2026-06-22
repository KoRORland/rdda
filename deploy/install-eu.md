# EU node setup (Ubuntu 24.04)

The EU node is the controller and internet exit. It holds RDDA's state and is
the only node you run `rdda` commands on in v0.1.

## 1. Provision with the installer

Run as root (SSH is fine on EU):

    curl -fsSL https://raw.githubusercontent.com/KoRORland/rdda/main/install.sh | sudo bash -s -- eu

This installs the `rdda` binary (checksum-verified), xray-core, the systemd
units, sets up `/etc/rdda` and the `rdda` user, enables time sync + automatic
security updates, and configures the firewall to allow 443 and 22.

## 2. Initialize state and the EU data plane

    rdda --dir /etc/rdda init --ru-host <RU_IP> --eu-host <EU_HOST>
    rdda --dir /etc/rdda render eu > /etc/rdda/xray.json
    chown -R rdda:rdda /etc/rdda
    systemctl enable --now rdda-xray

`init` generates the REALITY keys, tunnel UUID, and shortIds and writes
`/etc/rdda/config.yaml` — this node is the source of truth.

## 3. Produce the RU node's config

    rdda --dir /etc/rdda render ru

Copy that output to the RU node's `/etc/rdda/xray.json` (see `install-ru.md`).
Re-run and re-copy whenever you add or remove clients (pull-sync is a v0.2
feature).

## 4. Add a client and hand out the link

    rdda --dir /etc/rdda client add aunt-olga

This prints a `vless://...` link. Send it to the person over a private channel
(Signal, Telegram, in person); they paste it into Hiddify and connect. In v0.1
there is no public subscription endpoint — delivery is out-of-band on purpose
(nothing client-facing is exposed on the EU node).

> The subscription server (`rdda-sub`) is installed but dormant in v0.1; it
> comes online behind Cloudflare in v0.2.
