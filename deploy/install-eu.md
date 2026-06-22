# EU node setup (Ubuntu 24.04)

1. Install xray-core:
   `bash -c "$(curl -L https://github.com/XTLS/Xray-install/raw/main/install-release.sh)" @ install`
2. Install the `rdda` binary to `/usr/local/bin/rdda` and `mkdir -p /etc/rdda`.
3. Initialize state (run on EU — it is the source of truth):
   `rdda --dir /etc/rdda init --ru-host <RU_IP> --eu-host <EU_HOST>`
4. Render and install the EU xray config:
   `rdda --dir /etc/rdda render eu > /etc/rdda/xray.json`
5. Create the `rdda` system user and give it ownership of the state directory
   (the operator runs `rdda init`/`render` as root, then hands off ownership before starting rdda-sub):
   `sudo useradd --system --no-create-home --shell /usr/sbin/nologin rdda || true`
   `sudo chown -R rdda:rdda /etc/rdda`
6. Install units and start:
   `cp deploy/systemd/rdda-xray.service deploy/systemd/rdda-sub.service /etc/systemd/system/`
   `systemctl daemon-reload && systemctl enable --now rdda-xray rdda-sub`
7. Put the subscription server behind TLS (nginx/caddy on 443 → 127.0.0.1:8080) so
   `SubBaseURL` (https://<EU_HOST>) serves `/sub/<token>`.
