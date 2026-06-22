# RU node setup (Ubuntu 24.04)

1. Install xray-core (same installer as EU).
2. `mkdir -p /etc/rdda`.
3. On the EU node, render the RU config and copy it to the RU node:
   `rdda --dir /etc/rdda render ru`  → save output to the RU node's `/etc/rdda/xray.json`.
   (v0.1: manual copy. v0.2 automates this via `rdda pull`.)
4. Install and start the data plane unit:
   `cp deploy/systemd/rdda-xray.service /etc/systemd/system/`
   `systemctl daemon-reload && systemctl enable --now rdda-xray`
5. Repeat step 3 whenever clients change (after `rdda client add/rm` on EU).
