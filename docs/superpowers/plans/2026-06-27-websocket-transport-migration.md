# WebSocket Transport Migration Implementation Plan

> **SUPERSEDED 2026-06-27 by docs/superpowers/plans/2026-06-27-lane-b-singbox-migration.md (Lane B).** Do not execute. Lane B migrated both nodes to a single sing-box data plane (VLESS+REALITY+multiplex on the inspected hop, VLESS+WebSocket+TLS+multiplex over Cloudflare), which subsumes and replaces this xray-WS approach.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move RDDA's transport from XHTTP (xray-only) to **VLESS + WebSocket + mux** so a real **Hiddify (sing-box)** client can connect, while still defeating the June-2026 three-signal DPI scheme.

**Architecture:** Both hops switch network `xhttp → ws`. Client→RU stays **REALITY** (now over WS) with a **non-Chrome uTLS fingerprint**; RU→EU stays **CF-fronted TLS** (now WS). Client and RU→EU **outbounds gain mux** (Signal-3 / connection-frequency fix; XMUX was xray's mux, sing-box has its own). The subscription emits a Hiddify-consumable profile so **mux is actually enabled client-side**.

**Tech Stack:** Go 1.22, xray-core (servers) + sing-box/Hiddify (client), VLESS, WebSocket, REALITY, Cloudflare Tunnel, systemd.

## Global Constraints

- Module path `github.com/KoRORland/rdda`; `go.mod` stays `go 1.22`; never run bare `go mod tidy`; no new Go deps.
- **Transport rationale (binding):** XHTTP is xray-only; sing-box/Hiddify cannot speak it. WS+mux+CDN satisfies the same three signals per `docs/superpowers/specs` + `docs/ARCHITECTURE.md §2/§6` (updated 2026-06-27): subnet→CDN, frequency→**mux**, fingerprint→**non-Chrome uTLS** + REALITY. This plan supersedes the XHTTP references in `2026-06-24-v0.2-program-design.md`, `2026-06-24-v0.2-workstream-a-cloudflare-keystone.md`, `2026-06-25-multihost-integration-*.md`.
- **Default uTLS fingerprint must be non-Chrome** — `firefox` (mimicking Chrome is itself a flag per the June-2026 analysis).
- Client→RU keeps **REALITY**; RU→EU keeps **CF-terminated TLS** (REALITY can't sit behind a CDN). Only the `network` (+ its settings block) and `fingerprint` change, plus `mux` on outbounds.
- Run tests with `go test ./...` from repo root. Shell gated with `bash -n` + shellcheck.
- Commit + push to `main` per repo convention.

## File Structure

- `internal/state/config.go` — **modify**: add `Config.Fingerprint string` (default `firefox`).
- `internal/xrayconf/render.go` — **modify**: all five transport blocks `xhttp→ws`; `fingerprint chrome→cfg.Fingerprint`; add `mux` to the client→RU outbound (`RenderClient`) and the RU→EU outbound (`RenderRU` proxy out).
- `internal/subscription/subscription.go` — **modify**: `ClientURI` emits `type=ws` + `host` + `fp=<fingerprint>`; `Build`/subscription emits a profile that enables mux.
- `internal/cli/cli.go` — **modify**: `init` gains `--fingerprint` (default `firefox`).
- `internal/xrayconf/render_test.go`, `render_cf_test.go`, `internal/subscription/subscription_test.go` — **modify**: assert WS/mux/fingerprint.
- `test/integration/run.sh` — **modify**: single-host gate uses WS (nginx WebSocket-proxy headers + EU WS inbound).
- `test/integration/multihost/*` — **modify** (the actual-stack harness): real `cloudflared`+real Cloudflare (creds already wired as `CF_TUNNEL_*` secrets), **sing-box client** consuming the real `vless://`/profile, REALITY-over-WS to a real site, NAT egress.
- `.github/workflows/integration.yml` — **modify**: pass `CF_TUNNEL_CREDENTIALS` / `CF_TUNNEL_ID`.

---

### Task 1: Config — non-Chrome fingerprint field

**Files:**
- Modify: `internal/state/config.go`
- Test: `internal/state/config_test.go`

**Interfaces:**
- Produces: `Config.Fingerprint string` (yaml `fingerprint`); empty means default `firefox` at render time via a helper `func (c Config) FP() string`.

- [ ] **Step 1: Write the failing test**

```go
func TestConfigFingerprintDefault(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	if err := s.SaveConfig(Config{RUHost: "ru.example"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.FP() != "firefox" {
		t.Fatalf("FP() default = %q, want firefox", got.FP())
	}
	got.Fingerprint = "safari"
	if got.FP() != "safari" {
		t.Fatalf("FP() = %q, want safari", got.FP())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/state/ -run TestConfigFingerprint -v`
Expected: FAIL — `Fingerprint`/`FP` undefined.

- [ ] **Step 3: Implement**

Add to the `Config` struct (after `PullToken`):

```go
	Fingerprint   string     `yaml:"fingerprint"`
```

Add the helper at end of file:

```go
// FP returns the uTLS fingerprint to mimic; defaults to a non-Chrome profile
// (mimicking Chrome is itself a DPI flag under the June-2026 scheme).
func (c Config) FP() string {
	if c.Fingerprint == "" {
		return "firefox"
	}
	return c.Fingerprint
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/state/ -run TestConfigFingerprint -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/state/config.go internal/state/config_test.go
git commit -m "feat(state): add non-Chrome uTLS fingerprint config (default firefox)"
git push
```

---

### Task 2: Render — WebSocket + mux + fingerprint

**Files:**
- Modify: `internal/xrayconf/render.go`
- Test: `internal/xrayconf/render_ws_test.go` (create)

**Interfaces:**
- Consumes: `Config.FP()` (Task 1), `Config.CFEnabled()`, `Config.Cloudflare`, `Config.ClientPath`, `Config.TunnelPath`.
- Produces: unchanged signatures `RenderRU/RenderEU/RenderClient`; emitted JSON now uses `"network":"ws"` with `wsSettings`, `fingerprint = cfg.FP()`, and `mux` on the client and tunnel outbounds.

- [ ] **Step 1: Write the failing test**

```go
package xrayconf

import (
	"encoding/json"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
)

func wsCfg() state.Config {
	return state.Config{
		RUHost: "ru.example", RUPort: 443, EUHost: "eu.example", EUPort: 8443,
		ClientPath: "/cl", TunnelPath: "/tn", TunnelUUID: "uuid-1",
		ClientReality: state.Reality{Target: "www.microsoft.com:443", ServerName: "www.microsoft.com", PrivateKey: "k", PublicKey: "pub", ShortIDs: []string{"aa"}},
		TunnelReality: state.Reality{ServerName: "www.apple.com", PublicKey: "tpub", ShortIDs: []string{"bb"}},
		Cloudflare:    state.Cloudflare{TunnelHostname: "tunnel.example.com"},
		Fingerprint:   "firefox",
	}
}

func TestRenderClient_WSWithMuxAndFirefox(t *testing.T) {
	b, err := RenderClient(wsCfg(), "uuid-1", 1080)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	out := doc["outbounds"].([]any)[0].(map[string]any)
	ss := out["streamSettings"].(map[string]any)
	if ss["network"] != "ws" {
		t.Fatalf("network = %v, want ws", ss["network"])
	}
	if _, ok := ss["wsSettings"]; !ok {
		t.Fatal("missing wsSettings")
	}
	if ss["wsSettings"].(map[string]any)["path"] != "/cl" {
		t.Fatalf("ws path wrong: %v", ss["wsSettings"])
	}
	if ss["security"] != "reality" {
		t.Fatalf("client outbound must stay reality, got %v", ss["security"])
	}
	if ss["realitySettings"].(map[string]any)["fingerprint"] != "firefox" {
		t.Fatalf("fingerprint must be firefox, got %v", ss["realitySettings"])
	}
	mux, ok := out["mux"].(map[string]any)
	if !ok || mux["enabled"] != true {
		t.Fatalf("client outbound must enable mux, got %v", out["mux"])
	}
}

func TestRenderRU_TunnelOutboundWSMux(t *testing.T) {
	b, _ := RenderRU(wsCfg(), nil)
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	out := doc["outbounds"].([]any)[0].(map[string]any)
	ss := out["streamSettings"].(map[string]any)
	if ss["network"] != "ws" || ss["security"] != "tls" {
		t.Fatalf("tunnel outbound must be ws+tls, got %v/%v", ss["network"], ss["security"])
	}
	hdr := ss["wsSettings"].(map[string]any)["headers"].(map[string]any)
	if hdr["Host"] != "tunnel.example.com" {
		t.Fatalf("ws Host header must be the CF hostname, got %v", hdr["Host"])
	}
	if out["mux"].(map[string]any)["enabled"] != true {
		t.Fatal("tunnel outbound must enable mux")
	}
}

func TestRenderRU_ClientInboundWSReality(t *testing.T) {
	b, _ := RenderRU(wsCfg(), nil)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	in := doc["inbounds"].([]any)[0].(map[string]any)
	ss := in["streamSettings"].(map[string]any)
	if ss["network"] != "ws" || ss["security"] != "reality" {
		t.Fatalf("client inbound must be ws+reality, got %v/%v", ss["network"], ss["security"])
	}
}

func TestRenderEU_CFInboundWSPlaintext(t *testing.T) {
	b, _ := RenderEU(wsCfg())
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	in := doc["inbounds"].([]any)[0].(map[string]any)
	ss := in["streamSettings"].(map[string]any)
	if ss["network"] != "ws" || ss["security"] != "none" || in["listen"] != "127.0.0.1" {
		t.Fatalf("EU CF inbound must be loopback ws+none, got %v/%v/%v", in["listen"], ss["network"], ss["security"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/xrayconf/ -run WS -v`
Expected: FAIL — still `xhttp`, no `mux`, fingerprint `chrome`.

- [ ] **Step 3: Implement the WS conversion**

In `internal/xrayconf/render.go`, apply these exact substitutions in every transport block:

1. `"network": "xhttp"` → `"network": "ws"`.
2. `"xhttpSettings": obj{"path": cfg.ClientPath}` → `"wsSettings": obj{"path": cfg.ClientPath}` (inbounds and the client/EU blocks that carry no Host).
3. The CF tunnel **outbound** block: `"xhttpSettings": obj{"path": cfg.TunnelPath, "host": cfg.Cloudflare.TunnelHostname}` → `"wsSettings": obj{"path": cfg.TunnelPath, "headers": obj{"Host": cfg.Cloudflare.TunnelHostname}}`.
4. Every `"fingerprint": "chrome"` → `"fingerprint": cfg.FP()`.

Then add `mux` to the two **client-side outbounds**. In `RenderClient`, on the `outbound` object (the vless `proxy` outbound), add a sibling key:

```go
		"mux": obj{"enabled": true, "concurrency": 8, "xudpConcurrency": 16},
```

In `RenderRU`, on the `proxyOut` object (both the CF and non-CF branches), add the same `"mux"` key.

(Do **not** add mux to inbounds or to the EU freedom/blackhole outbounds.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/xrayconf/ -v`
Expected: PASS. Update any pre-existing `render_test.go`/`render_cf_test.go` assertions that hard-coded `xhttp`/`xhttpSettings`/`chrome` to the WS equivalents (network `ws`, `wsSettings`, `cfg.FP()`); re-run until the whole package is green.

- [ ] **Step 5: Commit**

```bash
git add internal/xrayconf/render.go internal/xrayconf/render_ws_test.go internal/xrayconf/render_test.go internal/xrayconf/render_cf_test.go
git commit -m "feat(xrayconf): WebSocket transport + mux + non-Chrome fingerprint"
git push
```

---

### Task 3: Subscription — WS link + mux-enabled profile

**Files:**
- Modify: `internal/subscription/subscription.go`
- Test: `internal/subscription/subscription_test.go`

**Interfaces:**
- Consumes: `Config.FP()`, `Config.ClientPath`, `Config.RUHost/RUPort`, `Config.ClientReality`.
- Produces: `ClientURI` query `type=ws`, `host=<RUHost>`, `path`, `security=reality`, `pbk`, `sid`, `sni`, `fp=<FP()>`. The subscription `Build` output must enable client-side **mux** (so Signal-3 actually holds) — emit the vless link **plus** a Hiddify/sing-box `mux` hint.

- [ ] **Step 1: Write the failing test**

```go
func TestClientURI_WSParams(t *testing.T) {
	cfg := state.Config{
		RUHost: "ru.example", RUPort: 443, ClientPath: "/cl",
		ClientReality: state.Reality{ServerName: "www.microsoft.com", PublicKey: "pub", ShortIDs: []string{"aa"}},
		Fingerprint:   "firefox",
	}
	uri := ClientURI(cfg, state.Client{UUID: "uuid-1"})
	for _, want := range []string{"type=ws", "security=reality", "fp=firefox", "path=%2Fcl", "host=ru.example"} {
		if !strings.Contains(uri, want) {
			t.Fatalf("uri missing %q:\n%s", want, uri)
		}
	}
	if strings.Contains(uri, "type=xhttp") {
		t.Fatalf("uri must not advertise xhttp:\n%s", uri)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/subscription/ -run WSParams -v`
Expected: FAIL — still `type=xhttp`, `fp=chrome`, no `host`.

- [ ] **Step 3: Implement**

In `ClientURI`, change the query construction:

```go
	q.Set("type", "ws")
	q.Set("path", cfg.ClientPath)
	q.Set("host", cfg.RUHost)
	q.Set("security", "reality")
	q.Set("pbk", cfg.ClientReality.PublicKey)
	// ... sid unchanged ...
	q.Set("sid", sid)
	q.Set("fp", cfg.FP())
```

Keep the existing `sni`/`Host`(URL host:port)/fragment construction. Then, so mux is actually enabled in the friend's client, append a `mux` note to the human-facing part of `Build` (a comment line above the link, e.g. `# enable Mux/multiplex in Hiddify settings`) — the bare `vless://` link cannot carry mux, and Hiddify exposes a global Mux toggle. Document this in `deploy/install-eu.md` (operator tells the friend to turn on Mux), and add a one-line note in `Build`'s output.

> **Design note for the implementer:** if a richer client config is later required to *force* mux without operator action, emit a sing-box outbound JSON (with `"multiplex":{"enabled":true}`) as an alternate subscription format. Out of scope for this task; the `vless://`+Mux-toggle path is the v0.2 deliverable.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/subscription/ -v`
Expected: PASS (update the existing `subscription_test.go` xhttp/chrome assertions to ws/firefox).

- [ ] **Step 5: Commit**

```bash
git add internal/subscription/subscription.go internal/subscription/subscription_test.go deploy/install-eu.md
git commit -m "feat(subscription): WS vless link + mux note (drop xhttp/chrome)"
git push
```

---

### Task 4: CLI — `init --fingerprint`

**Files:**
- Modify: `internal/cli/cli.go`
- Test: `internal/cli/cli_test.go` (add)

**Interfaces:**
- Consumes: `state.Config.Fingerprint` (Task 1).
- Produces: `rdda init --fingerprint <fp>` (default `firefox`) sets `Config.Fingerprint`.

- [ ] **Step 1: Write the failing test**

```go
func TestInitSetsFingerprint(t *testing.T) {
	dir := t.TempDir()
	root := newRoot()
	root.SetArgs([]string{"--dir", dir, "init", "--ru-host", "r", "--eu-host", "e", "--fingerprint", "safari"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	s, _ := state.Open(dir)
	cfg, _ := s.LoadConfig()
	if cfg.Fingerprint != "safari" {
		t.Fatalf("fingerprint = %q, want safari", cfg.Fingerprint)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run Fingerprint -v`
Expected: FAIL — unknown flag `--fingerprint`.

- [ ] **Step 3: Implement**

In `newInitCmd`, add a var `fp string`, register the flag, and set it on the `Config` literal:

```go
	cmd.Flags().StringVar(&fp, "fingerprint", "firefox", "uTLS fingerprint to mimic (non-Chrome recommended)")
```

and in the `Config{...}` literal add `Fingerprint: fp,`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -v` then `go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go
git commit -m "feat(cli): init --fingerprint (default firefox)"
git push
```

---

### Task 5: Single-host transport gate → WebSocket

**Files:**
- Modify: `test/integration/run.sh`

**Interfaces:**
- Consumes: the WS render from Task 2; the gate already runs xray + nginx + a test CA + h2.
- Produces: the fast regression gate proving the **WS** RU→EU hop survives the reverse proxy.

- [ ] **Step 1: Add WebSocket upgrade headers to the nginx data-hop location**

In the `location /` block of the nginx `cf.conf` heredoc, the WS hop needs upgrade headers instead of (or in addition to) the streaming directives. Replace the `location /` body with:

```bash
    location / {
        proxy_pass http://127.0.0.1:$EU_PORT;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 300s;
    }
```

(WebSocket needs `Upgrade`/`Connection: upgrade`; drop the XHTTP buffering directives — WS is a single upgraded connection, not split streams. Keep `listen 443 ssl http2` — h2 ALPN is fine; the WS upgrade rides HTTP/1.1 via `proxy_http_version 1.1`.)

- [ ] **Step 2: Run the gate locally if possible, else push and watch CI**

Run: `bash -n test/integration/run.sh && /home/asharov/.local/bin/shellcheck test/integration/run.sh`
Then commit/push and watch `gh run watch` for the `integration` job. Expected end state: `OK: two-hop tunnel works` + `--- PASS`.

- [ ] **Step 3: Commit**

```bash
git add test/integration/run.sh
git commit -m "test(integration): single-host gate uses WebSocket upgrade (not XHTTP)"
git push
```

---

### Task 6: Multi-host actual-stack harness (real CF + real sing-box + WS)

**Files:**
- Modify: `test/integration/multihost/image.sh`, `provision-eu.sh`, `provision-ru.sh`, `provision-client.sh`, `assert.sh`, `net.sh`, `run-multihost.sh`
- Remove: `provision-edge.sh`, `provision-target.sh` (no stand-ins)
- Modify: `.github/workflows/integration.yml`

**Interfaces:**
- Consumes: real Cloudflare (tunnel `rdda-ci`, hostnames `tunnel.rdda-test.kororland.com` / `sub.rdda-test.kororland.com`, secrets `CF_TUNNEL_CREDENTIALS` + `CF_TUNNEL_ID`); the WS render (Task 2); the WS `vless://` link (Task 3).
- Produces: the actual-stack green — real Hiddify-core client → RU → **real Cloudflare** → EU → real internet.

- [ ] **Step 1: Image — add real cloudflared + sing-box; drop chisel**

In `image.sh`, replace the chisel download with real `cloudflared` (Debian package or the static binary) and **sing-box** (static binary). Keep xray, geoip.dat/geosite.dat, the deps, and `sudo`. (cloudflared: `cloudflared-linux-amd64`; sing-box: the `sing-box-*-linux-amd64.tar.gz` release — pin the version to avoid api.github.com 403, as we did for chisel.)

- [ ] **Step 2: Net — NAT egress; drop the synthetic-target rules**

In `net.sh`: keep the bridge + the `iptables -I FORWARD` Docker whitelist; **add** `sysctl -w net.ipv4.ip_forward=1` and `iptables -t nat -A POSTROUTING -s 203.0.113.0/24 ! -d 203.0.113.0/24 -j MASQUERADE` so the hosts reach the real internet (cloudflared→CF, REALITY→real site, EU exit). Set the containers' `DNS=1.1.1.1` in the `.network`. Drop the `ip daddr 203.0.113.50 …` target rule (no synthetic target); keep the EU zero-inbound rule (belt-and-suspenders; real cloudflared makes EU outbound-only anyway).

- [ ] **Step 3: EU — real cloudflared against the real tunnel**

Rewrite `provision-eu.sh`: write `CF_TUNNEL_CREDENTIALS` to `/etc/cloudflared/<CF_TUNNEL_ID>.json` (from env, passed by the workflow), run `rdda init … --cf-tunnel-host tunnel.rdda-test.kororland.com --cf-sub-host sub.rdda-test.kororland.com --cf-tunnel-id "$CF_TUNNEL_ID" --cf-credentials-file /etc/cloudflared/<id>.json`, `rdda render eu > /etc/rdda/xray.json` (WS, loopback), `rdda render cloudflared > /etc/cloudflared/config.yml`, and run the **real** `cloudflared tunnel --config /etc/cloudflared/config.yml run` as a unit. EU xray binds loopback; cloudflared dials out to real Cloudflare. No edge container.

- [ ] **Step 4: RU — real xray, WS tunnel via real CF, REALITY→real site**

`provision-ru.sh`: pull the WS RU config from EU through real Cloudflare (`https://sub.rdda-test.kororland.com/ru/config`). The tunnel outbound dials `tunnel.rdda-test.kororland.com` (real CF). REALITY camouflage stays the init default real site (`www.microsoft.com`), now reachable via the Task-2 NAT. No `allowInsecure`, no port surgery (real 443 / real CF cert).

- [ ] **Step 5: Client — real sing-box consuming the real vless:// link**

Rewrite `provision-client.sh`: take the `vless://` link from `rdda client add` (run on EU), convert it to a sing-box config (VLESS + ws + reality + `multiplex.enabled=true`, mixed inbound `127.0.0.1:1080`), and run **sing-box** (not xray). This is the real Hiddify core driving the real link.

- [ ] **Step 6: Assert — exit via EU to the real internet**

`assert.sh`: green = the client fetches a real external resource through the tunnel, e.g. `curl --socks5-hostname 127.0.0.1:1080 https://www.example.com` returns 200, **and** an IP-echo through the tunnel (`https://api.ipify.org`) returns a stable address (the EU egress). Exit-via-EU is by construction (RU's only non-RU route is the tunnel to EU). Drop the synthetic-target assertion.

- [ ] **Step 7: Workflow — pass the secrets; run-multihost host list**

In `run-multihost.sh`, `HOSTS=(eu ru client)` (drop edge/target) and drop their provision calls. In `.github/workflows/integration.yml`, expose the secrets to the step:

```yaml
      - name: Run multi-host integration harness
        env:
          CF_TUNNEL_CREDENTIALS: ${{ secrets.CF_TUNNEL_CREDENTIALS }}
          CF_TUNNEL_ID: ${{ secrets.CF_TUNNEL_ID }}
        run: sudo -E env "PATH=$PATH" go test -tags=integration ./test/integration/... -v -timeout 25m
```

- [ ] **Step 8: Gate + iterate to green**

`bash -n` + shellcheck all changed scripts; push; `gh run watch` the `integration` job; iterate on real-CF specifics (cloudflared connect time, sing-box config shape) until `ALL ASSERTIONS PASSED` / `=== PASS ===`. Record the green run URL in the progress ledger.

- [ ] **Step 9: Commit (per logically-complete script)**

Commit each script change with a focused message; push. Final: `ci: actual-stack multi-host (real Cloudflare + sing-box client + WS)`.

---

## Self-Review

- **Spec coverage:** transport pivot (WS) — Tasks 2,3,5,6; non-Chrome fingerprint — Tasks 1,2,3,4; mux (Signal 3) — Task 2 (render) + Task 3 (client-side enablement) + Task 6 step 5 (sing-box multiplex); real client core (Hiddify/sing-box) — Task 6 step 5; real Cloudflare — Task 6 steps 3,7; docs already updated (`ARCHITECTURE.md`) + this plan supersedes the XHTTP specs. ✓
- **Placeholder scan:** code steps carry concrete code; the one judgment item (sing-box config shape, Task 6 step 5) is inherent to wiring a real client and is bounded by the WS+reality+mux requirements. No TBDs.
- **Type consistency:** `Config.FP()` (Task 1) used in render (Task 2), subscription (Task 3); `Config.Fingerprint` set by CLI (Task 4). `network:"ws"` / `wsSettings` consistent across render + the nginx WS headers (Task 5) + sing-box (Task 6). The CF hostname Host header (render Task 2) matches the real `tunnel.rdda-test.kororland.com` (Task 6).
- **Open caveat carried forward (not in scope here):** Signal-1 keys on the *client's first hop* subnet — the RU node's own subnet still matters on client→RU. Tracked in `ARCHITECTURE.md §2` for a topology pass; this plan delivers the transport, not the topology change.

## Out of scope

The Signal-1 topology change (fronting the client→RU hop); a sing-box-native subscription format that force-enables mux without the Hiddify Mux toggle; XHTTP+xray-client as an alternate profile.
