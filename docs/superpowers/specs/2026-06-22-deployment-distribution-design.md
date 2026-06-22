# RDDA v0.1 Deployment & Distribution — Design

**Status:** approved for planning (2026-06-22)
**Builds on:** `docs/ARCHITECTURE.md`, `docs/superpowers/plans/2026-06-21-rdda-v0.1-mvp.md` (the v0.1 MVP, now CI-verified)

## Goal

Make "install RDDA" a concrete, detailed, low-touch operation. Provide prebuilt binaries, a single `curl | bash` installer that provisions and hardens a node, out-of-band client delivery, and rewritten step-by-step deploy docs. No client-facing service is exposed on either node (cover-safe); the RU node exposes nothing but port 443.

## Background / why these choices

- The current `deploy/install-{eu,ru}.md` hand-wave "install the `rdda` binary" — there is no mechanism. CI builds but publishes nothing.
- `rdda` is a static Go binary, ideal for prebuilt distribution. Operators should not need a Go toolchain or source tree on a VPS (especially the RU node, which must stay minimal and innocuous).
- The EU node's real "cover" comes from Cloudflare, which is a **v0.2** item. In v0.1 there is no Cloudflare, so we must NOT stand up a client-facing TLS endpoint (e.g. Caddy + Let's Encrypt) on EU: that would be a naked, unknown-reputation TLS host on an unknown VPS subnet, visible to RKN, and would newly expose the EU IP to every client (today the EU IP is contacted only by the one RU node). Therefore v0.1 delivers client configs **out-of-band**, and the subscription server we already built stays dormant until v0.2 (when it sits behind Cloudflare).
- The RU node must look like a boring HTTPS site and expose no management surface. With REALITY "steal-others" SNI, xray relays the TLS handshake to a *remote* borrowed site (no local decoy server), and routing is app-layer (no kernel IP-forwarding/NAT). So the RU node needs **only port 443 inbound** — nothing else.

## Components

### 1. Release pipeline + version stamping

**File:** `.github/workflows/release.yml`

- Trigger: push of a tag matching `v*` (operator cuts tags manually — matches the project's manual-versioning rule).
- Build matrix: `GOOS=linux` × `GOARCH={amd64,arm64}`, `CGO_ENABLED=0` (static).
- Version stamping: `internal/cli.Version` becomes a package-level `var Version = "dev"` (currently a `const`), overridden at build time with `-ldflags "-X github.com/KoRORland/rdda/internal/cli.Version=<tag>"`. Local/dev builds report `dev`; release builds report the tag (e.g. `v0.1.0`).
- Artifacts published to a GitHub Release for the tag: `rdda-linux-amd64`, `rdda-linux-arm64`, and `SHA256SUMS` (checksums of both binaries).
- Uses `softprops/action-gh-release` (or `gh release create`) to attach assets.

A separate **PR dry-run** (in the existing `ci.yml` or a new job): build both-arch binaries with the same ldflags but DO NOT publish — proves the release build compiles for both arches before a tag is cut.

### 2. Installer — `install.sh`

**File:** `install.sh` (repo root; curl-able from `https://raw.githubusercontent.com/KoRORland/rdda/main/install.sh`).

**Usage:** `curl -fsSL https://raw.githubusercontent.com/KoRORland/rdda/main/install.sh | sudo bash -s -- <eu|ru> [--version vX.Y.Z] [--keep-ssh]`

- `<eu|ru>` (required): node role.
- `--version` (optional): release tag to install; default = latest release (resolved via the GitHub API `releases/latest`).
- `--keep-ssh` (optional): skip the RU firewall's closing of port 22 (debugging escape hatch).

**Behaviour (ordered; firewall lockdown is LAST so a mid-run failure never locks the operator out):**

1. Preconditions: must run as root (`EUID -eq 0`); role arg is `eu` or `ru`; `curl` present. Detect arch: `uname -m` → `x86_64`⇒`amd64`, `aarch64`⇒`arm64`; otherwise error out.
2. Resolve the release tag (latest unless `--version`), download `rdda-linux-<arch>` and `SHA256SUMS` to a temp dir.
3. Verify the binary against `SHA256SUMS` (`sha256sum -c`); abort on mismatch. Install to `/usr/local/bin/rdda` (mode 0755). Both roles get the binary (forward-compatible with v0.2 pull-sync).
4. Install xray-core via the official XTLS installer (`bash -c "$(curl -L https://github.com/XTLS/Xray-install/raw/main/install-release.sh)" @ install`). Then `systemctl disable --now xray.service` (the stock unit) so it does not conflict with `rdda-xray.service`.
5. `mkdir -p /etc/rdda` (mode 0700). Create the `rdda` system user if absent: `useradd --system --no-create-home --shell /usr/sbin/nologin rdda`.
6. Install systemd units (fetched from the release tag's source, or embedded in the script — see Open question O1):
   - `eu`: install `rdda-xray.service` + `rdda-sub.service`, reload daemon. `rdda-sub` is left **installed but neither enabled nor started** in v0.1 (dormant until v0.2; when used it binds only `127.0.0.1:8080`).
   - `ru`: `rdda-xray.service` only. Reload daemon.
   (Services are not *started* here because `/etc/rdda/xray.json` does not exist yet — the operator runs `rdda init`/`render` next. The installer prints this.)
7. Host hardening (both roles):
   - `systemctl enable --now systemd-timesyncd` (accurate clock; TLS/REALITY depend on it).
   - Install + enable `unattended-upgrades` for automatic security patches.
   - Install `ufw` if absent.
8. Firewall (FINAL step):
   - `ru`: `ufw --force reset` is NOT used; instead `ufw default deny incoming`, `ufw default allow outgoing`, `ufw allow 443/tcp`, then (unless `--keep-ssh`) ensure 22 is NOT allowed, `ufw --force enable`. Print a prominent notice: SSH is now closed; use the VPS provider console for future access.
   - `eu`: `ufw default deny incoming`, `ufw default allow outgoing`, `ufw allow 443/tcp`, `ufw allow 22/tcp`, `ufw --force enable`.
   - If the script detects it is running over an SSH session on the `ru` role without `--keep-ssh`, it prints a clear warning before enabling (the operator is expected to have read the docs / be on console; the warning is the safety net, and because this is the last step everything else is already done).
9. Print next-steps tailored to the role.

The script is POSIX-bash, `set -euo pipefail`, with a `fail()` helper and clear section logging. No interactive prompts (it runs under `curl | bash`); choices come from flags.

### 3. Out-of-band client delivery (CLI change)

In v0.1, `rdda client add <name>` prints the importable **`vless://` link** produced by `subscription.ClientURI(cfg, client)` instead of the `<SubBaseURL>/sub/<token>` URL (nothing serves `/sub` in v0.1). The operator sends the link to the recipient over a private channel; the recipient pastes it into Hiddify (which imports a `vless://` link directly).

- Change `internal/cli/cli.go` `client add` to print `subscription.ClientURI(cfg, c)`.
- Update `TestClientAddPrintsSubURL` (in `internal/cli/cli_test.go`) to assert the output starts with `vless://` and contains the RU host. Rename the test to reflect the new behaviour (e.g. `TestClientAddPrintsVlessLink`).
- The `/sub` URL + `serve` return in v0.2 behind Cloudflare (no code removed now; the subserver/serve stay in place, just unused in deployment).

### 4. Rewritten deploy docs

Rewrite `deploy/install-eu.md`, `deploy/install-ru.md`, and the README "Quickstart — Operators" section so each step states *what it does and why*:

- **EU:** run the installer (`... | sudo bash -s -- eu`); `rdda --dir /etc/rdda init --ru-host <RU_IP> --eu-host <EU_HOST>`; `rdda --dir /etc/rdda render eu > /etc/rdda/xray.json`; `chown -R rdda:rdda /etc/rdda`; `systemctl enable --now rdda-xray`; add a client and send the printed link.
- **RU:** run the installer **from the VPS provider console** (`... | sudo bash -s -- ru`) — note that it closes SSH at the end; obtain the rendered RU config from EU (`rdda --dir /etc/rdda render ru` on EU) and place it at the RU node's `/etc/rdda/xray.json`; `systemctl enable --now rdda-xray`.
- **The EU→RU config copy** is the one manual cross-node step in v0.1. Documented honestly: the operator transfers the rendered `xray.json` using their own access (paste via console, or a one-time secure copy) — there is no exposed management channel on RU, and automatic pull-sync is a v0.2 feature.
- Re-running `rdda render ru` and re-copying is required whenever clients change (until pull-sync lands).

### 5. Testing

- New CI lint job (`shell`): `shellcheck install.sh` and `bash -n install.sh`.
- Release dry-run job: build `rdda` for both arches with the release ldflags; assert the binaries exist and `rdda version` (amd64) prints the injected version. Does not publish.
- Existing Go suite updated for the `client add` output change; `go test ./...` stays green.
- The installer's full runtime behaviour (xray install, ufw) is validated manually on a real VPS / documented as a smoke checklist — not unit-tested (it mutates host state).

## Deferred to v0.2 (explicitly out of scope here)

Cloudflare-fronted subscription endpoint (revives `serve`/`rdda-sub`), automatic pull-sync of the RU config, key rotation, failover, and ASN-based outbound telemetry blocklists.

## Open questions / decisions locked

- **O1 — unit/source delivery in the installer:** the installer needs the systemd unit files. Decision: the installer downloads the unit files from the tagged release source (raw GitHub at the resolved tag) so units match the installed binary version. (Alternative considered: embed unit text as heredocs in `install.sh` — rejected to avoid drift between the script and `deploy/systemd/`.)
- **Arch targets:** `linux/amd64` + `linux/arm64` (locked).
- **EU SSH policy:** EU keeps `22/tcp` open (management zone); key-only SSH recommended in docs, not enforced by the installer.
- **RU SSH:** open during provisioning, closed by the installer as its final step; `--keep-ssh` escape hatch; ongoing maintenance via VPS console.
