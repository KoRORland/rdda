# RDDA v0.1 Deployment & Distribution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a real install path for RDDA v0.1 — prebuilt release binaries, a single `curl | bash` installer that provisions and hardens a node (eu/ru), out-of-band client delivery, and detailed deploy docs.

**Architecture:** A tag-triggered GitHub Actions workflow builds static `rdda` binaries (amd64/arm64), stamps the version via ldflags, and publishes them with checksums to a GitHub Release. A POSIX-bash `install.sh` downloads+verifies the binary, installs xray-core, sets up systemd units, hardens the host (timesync, auto-updates, ufw), and — as its final step — locks the RU node to port 443 only. `rdda client add` prints an importable `vless://` link for out-of-band delivery; the subscription server stays dormant until v0.2.

**Tech Stack:** Go 1.22 (existing CLI), GitHub Actions, POSIX bash, shellcheck, ufw, systemd, xray-core (external), Hiddify (client, external).

## Global Constraints

- Go pinned `go 1.22` in go.mod (no `toolchain` line); `golang.org/x/crypto v0.31.0`, `golang.org/x/net v0.33.0`. **Never run a bare `go mod tidy`** — use `go mod tidy -go=1.22` if needed. This work adds NO Go dependencies.
- Module path `github.com/KoRORland/rdda`. Binary installs to `/usr/local/bin/rdda`. State dir `/etc/rdda` (mode 0700), owned by the `rdda` system user.
- Release artifacts: `rdda-linux-amd64`, `rdda-linux-arm64`, `SHA256SUMS`. Build static (`CGO_ENABLED=0`). Version injected via `-ldflags "-X github.com/KoRORland/rdda/internal/cli.Version=<tag>"`; default `Version = "dev"`.
- Installer usage: `curl -fsSL https://raw.githubusercontent.com/KoRORland/rdda/main/install.sh | sudo bash -s -- <eu|ru> [--version vX.Y.Z] [--keep-ssh]`.
- RU node: only `443/tcp` inbound; SSH (22) closed by the installer's final step unless `--keep-ssh`. EU node: `443/tcp` + `22/tcp`.
- Installer is non-interactive (`set -euo pipefail`), choices via flags. Firewall lockdown is the LAST action so a mid-run failure never locks the operator out.
- Commit + push each task to `main`. Version bumps (tags) are manual operator decisions.
- Out of scope (v0.2): Cloudflare-fronted subscription, pull-sync, rotation, failover, ASN blocklists.

## File Structure

```
internal/cli/cli.go                 # MODIFY: Version const→var (ldflags); client add prints vless:// link
internal/cli/cli_test.go            # MODIFY: update client-add test for the link output
.github/workflows/release.yml       # CREATE: tag-triggered multi-arch release + checksums
.github/workflows/ci.yml            # MODIFY: add shellcheck job + release dry-run job
install.sh                          # CREATE: curl|bash installer (eu/ru, hardening, firewall)
deploy/install-eu.md                # REWRITE: detailed EU steps
deploy/install-ru.md                # REWRITE: detailed RU steps (console, SSH closes)
README.md                           # MODIFY: operator quickstart → installer one-liner + out-of-band link
```

---

### Task 1: Version stamping (const → ldflags-overridable var)

**Files:**
- Modify: `internal/cli/cli.go`
- Test: `internal/cli/cli_test.go`

**Interfaces:**
- Produces: `cli.Version` is now a package-level `var` (default `"dev"`), overridable with `-ldflags "-X github.com/KoRORland/rdda/internal/cli.Version=<tag>"`. `rdda version` prints whatever `Version` holds.

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/cli_test.go`:
```go
func TestVersionIsOverridable(t *testing.T) {
	// Version must be a var (ldflags-injectable), defaulting to a non-empty string.
	if Version == "" {
		t.Fatal("Version must default to a non-empty value")
	}
	orig := Version
	t.Cleanup(func() { Version = orig })
	Version = "v9.9.9"
	out := run(t, "version")
	if !strings.Contains(out, "v9.9.9") {
		t.Fatalf("version command should print injected Version, got %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestVersionIsOverridable`
Expected: FAIL — currently `Version` is a `const`, so `Version = "v9.9.9"` will not compile (`cannot assign to Version`).

- [ ] **Step 3: Change the const to a var**

In `internal/cli/cli.go`, change:
```go
const Version = "0.1.0-dev"
```
to:
```go
// Version is the RDDA release, injected at build time via
// -ldflags "-X github.com/KoRORland/rdda/internal/cli.Version=<tag>".
// Local/dev builds report "dev".
var Version = "dev"
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/`
Expected: PASS (TestVersionIsOverridable + existing tests).

- [ ] **Step 5: Verify ldflags injection works**

Run: `go run -ldflags "-X github.com/KoRORland/rdda/internal/cli.Version=v0.1.0" ./cmd/rdda version`
Expected: prints `v0.1.0`. Then `go run ./cmd/rdda version` prints `dev`.

- [ ] **Step 6: Commit and push**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go
git commit -m "feat(cli): make Version ldflags-overridable for release stamping"
git push origin main
```

---

### Task 2: `rdda client add` prints the importable vless:// link

**Files:**
- Modify: `internal/cli/cli.go`
- Test: `internal/cli/cli_test.go`

**Interfaces:**
- Consumes: `subscription.ClientURI(cfg state.Config, c state.Client) string` (already exists).
- Produces: `rdda client add <name>` prints the `vless://...` link (out-of-band delivery). The dormant `/sub/<token>` URL is no longer printed in v0.1.

- [ ] **Step 1: Update the test for the new output**

In `internal/cli/cli_test.go`, replace the body of the existing client-add test (currently `TestClientAddPrintsSubURL`) — rename it and assert the vless link:
```go
func TestClientAddPrintsVlessLink(t *testing.T) {
	dir := t.TempDir()
	run(t, "--dir", dir, "init", "--ru-host", "ru.example.net", "--eu-host", "eu.example.net")
	out := run(t, "--dir", dir, "client", "add", "granny")
	if !strings.HasPrefix(strings.TrimSpace(out), "vless://") {
		t.Fatalf("expected a vless:// link, got: %s", out)
	}
	if !strings.Contains(out, "ru.example.net") {
		t.Fatalf("link should point at the RU host, got: %s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestClientAddPrintsVlessLink`
Expected: FAIL — `client add` currently prints `<SubBaseURL>/sub/<token>`, not a `vless://` link.

- [ ] **Step 3: Change the command to print the link**

In `internal/cli/cli.go`, in the `client add` `RunE`, replace the success print line:
```go
			fmt.Fprintf(cmd.OutOrStdout(), "%s/sub/%s\n", cfg.SubBaseURL, c.Token)
```
with:
```go
			fmt.Fprintln(cmd.OutOrStdout(), subscription.ClientURI(cfg, c))
```
Ensure `internal/subscription` is imported in `cli.go` (add `"github.com/KoRORland/rdda/internal/subscription"` to the import block if not present).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/`
Expected: PASS. Then `go test ./...` — all packages green.

- [ ] **Step 5: Commit and push**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go
git commit -m "feat(cli): client add prints importable vless link for out-of-band delivery"
git push origin main
```

---

### Task 3: Release workflow (tag-triggered, multi-arch, checksums)

**Files:**
- Create: `.github/workflows/release.yml`

**Interfaces:**
- Produces: on a pushed `v*` tag, a GitHub Release with assets `rdda-linux-amd64`, `rdda-linux-arm64`, `SHA256SUMS`, each binary version-stamped with the tag.

- [ ] **Step 1: Write the release workflow**

`.github/workflows/release.yml`:
```yaml
name: release
on:
  push:
    tags: ["v*"]
permissions:
  contents: write
jobs:
  build:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.22" }
      - name: Build binaries
        run: |
          set -euo pipefail
          TAG="${GITHUB_REF_NAME}"
          LDFLAGS="-s -w -X github.com/KoRORland/rdda/internal/cli.Version=${TAG}"
          mkdir -p dist
          for arch in amd64 arm64; do
            CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
              go build -ldflags "$LDFLAGS" -o "dist/rdda-linux-${arch}" ./cmd/rdda
          done
          cd dist && sha256sum rdda-linux-amd64 rdda-linux-arm64 > SHA256SUMS
      - name: Verify amd64 version string
        run: |
          set -euo pipefail
          # amd64 binary runs on the amd64 runner; confirm the tag was stamped in.
          out="$(./dist/rdda-linux-amd64 version)"
          echo "version output: $out"
          test "$out" = "${GITHUB_REF_NAME}"
      - name: Publish release
        uses: softprops/action-gh-release@v2
        with:
          files: |
            dist/rdda-linux-amd64
            dist/rdda-linux-arm64
            dist/SHA256SUMS
```

- [ ] **Step 2: Validate the workflow YAML locally**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yml')); print('release.yml valid YAML')"`
Expected: `release.yml valid YAML`

- [ ] **Step 3: Confirm the build line works locally (smoke)**

Run:
```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X github.com/KoRORland/rdda/internal/cli.Version=v0.0.0-test" -o /tmp/rdda-smoke ./cmd/rdda && /tmp/rdda-smoke version
```
Expected: prints `v0.0.0-test`. Then `rm -f /tmp/rdda-smoke`.

- [ ] **Step 4: Commit and push**

```bash
git add .github/workflows/release.yml
git commit -m "ci: add tag-triggered multi-arch release workflow"
git push origin main
```

---

### Task 4: CI — shellcheck job + release dry-run job

**Files:**
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: `install.sh` will exist after Task 5; the shellcheck job tolerates its absence until then by guarding on the file (see step 1). The dry-run job builds both arches with release ldflags but does not publish.

- [ ] **Step 1: Add the two jobs to ci.yml**

Append these jobs under `jobs:` in `.github/workflows/ci.yml` (keep the existing `unit` and `integration` jobs unchanged):
```yaml
  shell:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v4
      - name: shellcheck install.sh
        run: |
          set -euo pipefail
          if [ -f install.sh ]; then
            sudo apt-get update && sudo apt-get install -y shellcheck
            shellcheck install.sh
            bash -n install.sh
          else
            echo "install.sh not present yet; skipping"
          fi
  release-dryrun:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.22" }
      - name: Build both arches (no publish)
        run: |
          set -euo pipefail
          LDFLAGS="-s -w -X github.com/KoRORland/rdda/internal/cli.Version=v0.0.0-dryrun"
          for arch in amd64 arm64; do
            CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
              go build -ldflags "$LDFLAGS" -o "/tmp/rdda-${arch}" ./cmd/rdda
          done
          test "$(/tmp/rdda-amd64 version)" = "v0.0.0-dryrun"
```

- [ ] **Step 2: Validate the YAML**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml')); print('ci.yml valid YAML')"`
Expected: `ci.yml valid YAML`

- [ ] **Step 3: Confirm existing jobs still present**

Run: `grep -E '^  (unit|integration|shell|release-dryrun):' .github/workflows/ci.yml`
Expected: all four job names listed.

- [ ] **Step 4: Commit and push**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add shellcheck and release dry-run jobs"
git push origin main
```

---

### Task 5: The installer — `install.sh`

**Files:**
- Create: `install.sh`

**Interfaces:**
- Consumes: GitHub Releases assets `rdda-linux-<arch>` + `SHA256SUMS` (Task 3); unit files at `deploy/systemd/*.service` fetched from raw GitHub at the resolved tag (per spec O1).
- Produces: a provisioned node. No other task consumes it.

- [ ] **Step 1: Write the installer script**

Create `install.sh` (POSIX bash). Write it exactly as below:
```bash
#!/usr/bin/env bash
# RDDA node installer. Usage:
#   curl -fsSL https://raw.githubusercontent.com/KoRORland/rdda/main/install.sh | sudo bash -s -- <eu|ru> [--version vX.Y.Z] [--keep-ssh]
set -euo pipefail

REPO="KoRORland/rdda"
BIN_DST="/usr/local/bin/rdda"
STATE_DIR="/etc/rdda"
UNIT_DIR="/etc/systemd/system"

log()  { printf '\033[1;34m[rdda]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[rdda]\033[0m %s\n' "$*" >&2; }
fail() { printf '\033[1;31m[rdda] ERROR:\033[0m %s\n' "$*" >&2; exit 1; }

ROLE=""
VERSION="latest"
KEEP_SSH="no"

# --- parse args ---
[ "$#" -ge 1 ] || fail "role required: eu or ru"
ROLE="$1"; shift
case "$ROLE" in eu|ru) ;; *) fail "role must be 'eu' or 'ru', got '$ROLE'";; esac
while [ "$#" -gt 0 ]; do
  case "$1" in
    --version) VERSION="${2:-}"; shift 2 || fail "--version needs a value";;
    --keep-ssh) KEEP_SSH="yes"; shift;;
    *) fail "unknown argument: $1";;
  esac
done

# --- preconditions ---
[ "$(id -u)" -eq 0 ] || fail "must run as root (use sudo)"
command -v curl >/dev/null || fail "curl is required"

case "$(uname -m)" in
  x86_64)  ARCH="amd64";;
  aarch64) ARCH="arm64";;
  *) fail "unsupported architecture: $(uname -m)";;
esac
log "role=$ROLE arch=$ARCH version=$VERSION"

# --- resolve release tag ---
if [ "$VERSION" = "latest" ]; then
  TAG="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -m1 '"tag_name"' | cut -d'"' -f4)"
  [ -n "$TAG" ] || fail "could not resolve latest release tag (has a release been published?)"
else
  TAG="$VERSION"
fi
log "installing tag $TAG"

# --- download + verify + install rdda binary ---
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
BASE="https://github.com/${REPO}/releases/download/${TAG}"
curl -fsSL "${BASE}/rdda-linux-${ARCH}" -o "${TMP}/rdda-linux-${ARCH}"
curl -fsSL "${BASE}/SHA256SUMS"         -o "${TMP}/SHA256SUMS"
( cd "$TMP" && grep "rdda-linux-${ARCH}\$" SHA256SUMS | sha256sum -c - ) \
  || fail "checksum verification failed for rdda-linux-${ARCH}"
install -m 0755 "${TMP}/rdda-linux-${ARCH}" "$BIN_DST"
log "installed $($BIN_DST version) to $BIN_DST"

# --- install xray-core, disable its stock unit ---
log "installing xray-core"
bash -c "$(curl -L https://github.com/XTLS/Xray-install/raw/main/install-release.sh)" @ install
systemctl disable --now xray.service 2>/dev/null || true

# --- state dir + user ---
mkdir -p "$STATE_DIR"; chmod 0700 "$STATE_DIR"
id rdda >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin rdda
log "state dir $STATE_DIR ready; rdda user present"

# --- systemd units (fetched at the resolved tag to match the binary) ---
RAW="https://raw.githubusercontent.com/${REPO}/${TAG}/deploy/systemd"
curl -fsSL "${RAW}/rdda-xray.service" -o "${UNIT_DIR}/rdda-xray.service"
if [ "$ROLE" = "eu" ]; then
  curl -fsSL "${RAW}/rdda-sub.service" -o "${UNIT_DIR}/rdda-sub.service"
fi
systemctl daemon-reload
log "installed systemd units (rdda-sub stays dormant on eu until v0.2)"

# --- host hardening (both roles) ---
log "hardening host: time sync + unattended upgrades + firewall"
systemctl enable --now systemd-timesyncd 2>/dev/null || true
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y unattended-upgrades ufw
systemctl enable --now unattended-upgrades 2>/dev/null || true

# --- firewall: FINAL step (so a mid-run failure never locks us out) ---
ufw default deny incoming
ufw default allow outgoing
ufw allow 443/tcp
if [ "$ROLE" = "eu" ]; then
  ufw allow 22/tcp
else
  if [ "$KEEP_SSH" = "yes" ]; then
    ufw allow 22/tcp
    warn "RU node: --keep-ssh set, leaving SSH (22) OPEN"
  else
    warn "RU node: closing SSH (22). Use your VPS provider console from now on."
  fi
fi
ufw --force enable
log "firewall active: $(ufw status | tr '\n' ' ')"

# --- next steps ---
cat <<EOF

[rdda] $ROLE node provisioned. Next steps:
EOF
if [ "$ROLE" = "eu" ]; then
  cat <<'EOF'
  1. rdda --dir /etc/rdda init --ru-host <RU_IP> --eu-host <EU_HOST>
  2. rdda --dir /etc/rdda render eu > /etc/rdda/xray.json
  3. chown -R rdda:rdda /etc/rdda
  4. systemctl enable --now rdda-xray
  5. rdda --dir /etc/rdda client add <name>   # send the printed vless:// link privately
  (RU config: run `rdda --dir /etc/rdda render ru` and copy the output to the RU node.)
EOF
else
  cat <<'EOF'
  1. On the EU node, run: rdda --dir /etc/rdda render ru
  2. Copy that output to this RU node's /etc/rdda/xray.json
  3. chown -R rdda:rdda /etc/rdda
  4. systemctl enable --now rdda-xray
EOF
fi
```

- [ ] **Step 2: Lint and syntax-check**

Run:
```bash
bash -n install.sh && { command -v shellcheck >/dev/null && shellcheck install.sh || echo "shellcheck not installed locally; CI will run it"; }
```
Expected: `bash -n` produces no output (valid syntax); shellcheck reports no errors (or note it runs in CI).

- [ ] **Step 3: Verify argument parsing without mutating the host**

Run these (they exit before touching the system, on the precondition/arg checks):
```bash
bash install.sh 2>&1 | head -1            # expect: role required
bash install.sh bogus 2>&1 | head -1      # expect: role must be 'eu' or 'ru'
bash install.sh eu --version 2>&1 | head -1   # expect: --version needs a value (or root error if run as root)
```
Expected: each prints the matching `ERROR:` line and exits non-zero. (Run as non-root so it stops at the role/arg checks before the root check; the point is the arg-validation messages appear.)

- [ ] **Step 4: Commit and push**

```bash
git add install.sh
git commit -m "feat(deploy): curl|bash installer with host hardening and 443-only RU firewall"
git push origin main
```

---

### Task 6: Rewrite deploy docs + README operator quickstart

**Files:**
- Modify: `deploy/install-eu.md`
- Modify: `deploy/install-ru.md`
- Modify: `README.md`

**Interfaces:**
- Consumes: the installer (Task 5) and the `rdda` CLI commands (init/render/client add).
- Produces: docs only. (Documentation task — verified by review + link/command accuracy, no unit test.)

- [ ] **Step 1: Rewrite `deploy/install-eu.md`**

Replace the entire file with:
```markdown
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
```

- [ ] **Step 2: Rewrite `deploy/install-ru.md`**

Replace the entire file with:
```markdown
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
```

- [ ] **Step 3: Update the README operator quickstart**

In `README.md`, replace the body of the "🚀 Quickstart — Operators" section (the numbered list and the code block(s), down to but not including the "📱 Quickstart — Clients" heading) with:
```markdown
## 🚀 Quickstart — Operators

You need **two Ubuntu 24.04 VPSes** — one in Russia (RU), one abroad (EU).

1. **EU node** (SSH is fine here):
   ```bash
   curl -fsSL https://raw.githubusercontent.com/KoRORland/rdda/main/install.sh | sudo bash -s -- eu
   rdda --dir /etc/rdda init --ru-host <RU_IP> --eu-host <EU_HOST>
   rdda --dir /etc/rdda render eu > /etc/rdda/xray.json
   chown -R rdda:rdda /etc/rdda && systemctl enable --now rdda-xray
   ```
2. **RU node** (run from your VPS provider **console** — the installer closes SSH):
   ```bash
   curl -fsSL https://raw.githubusercontent.com/KoRORland/rdda/main/install.sh | sudo bash -s -- ru
   ```
   Then, on EU, `rdda --dir /etc/rdda render ru` and copy the output to the RU node's
   `/etc/rdda/xray.json`; on RU `systemctl enable --now rdda-xray`.
3. **Add a friend** (on EU) and send them the printed `vless://` link privately:
   ```bash
   rdda --dir /etc/rdda client add aunt-olga
   ```

See [`deploy/install-eu.md`](deploy/install-eu.md) and [`deploy/install-ru.md`](deploy/install-ru.md) for the detailed walkthrough.
```

- [ ] **Step 4: Verify no stale references remain**

Run:
```bash
grep -n "render eu > /etc/rdda/xray.json" deploy/install-eu.md
grep -rn "install.sh" deploy/install-eu.md deploy/install-ru.md README.md
grep -rn "sub/" deploy/ README.md || echo "no stale /sub references (good)"
```
Expected: the installer one-liner appears in all three docs; no leftover instructions to fetch a `/sub/<token>` URL as the client handoff.

- [ ] **Step 5: Commit and push**

```bash
git add deploy/install-eu.md deploy/install-ru.md README.md
git commit -m "docs: detailed installer-based deploy guide and out-of-band client delivery"
git push origin main
```

---

## Self-Review

**1. Spec coverage:**
- Release pipeline + version stamping → Tasks 1 (var) + 3 (workflow). ✅
- Installer (download+verify, xray, units, user, hardening, firewall-last, eu/ru, --version, --keep-ssh) → Task 5. ✅
- Out-of-band client delivery (vless link) → Task 2. ✅
- Rewritten docs (eu/ru/README, honest EU→RU copy step) → Task 6. ✅
- Testing (shellcheck + release dry-run; Go tests updated) → Task 4 + Tasks 1/2 tests. ✅
- O1 (units fetched from tagged release source) → Task 5 step 1 (`RAW=.../${TAG}/deploy/systemd`). ✅
- Deferred-to-v0.2 (subscription/serve untouched, just unused) → respected; no code removed. ✅

**2. Placeholder scan:** No TBD/TODO/"handle errors" placeholders. Task 5's installer is shown in full. Doc tasks show full file contents. Commands have expected output. ✅

**3. Type consistency:** `cli.Version` is `var` (Task 1) and read by `version` (existing) and the release ldflags (Tasks 3/4) using the exact path `github.com/KoRORland/rdda/internal/cli.Version`. `subscription.ClientURI(cfg, c)` (Task 2) matches the existing signature. Installer asset names (`rdda-linux-amd64/arm64`, `SHA256SUMS`) match between Task 3 (produces) and Task 5 (consumes). Unit filenames (`rdda-xray.service`, `rdda-sub.service`) match `deploy/systemd/`. ✅

**Note on sequencing:** Task 4's `shell` job guards on `install.sh` existence, so it is green whether it runs before or after Task 5. The `release.yml` (Task 3) only fires on tags, so it does not run during normal task pushes. The installer (Task 5) requires a published release to actually run end-to-end; cutting the first tag (`v0.1.0`) is a manual operator decision after these tasks land.
