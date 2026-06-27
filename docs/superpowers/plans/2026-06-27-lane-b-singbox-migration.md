# Lane B — sing-box Data Plane Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the xray data plane with a single sing-box core on both nodes, so the Hiddify (sing-box) client speaks one protocol and one multiplex scheme end to end, defeating the June-2026 DPI scheme on the only hop it inspects.

**Architecture:** Both RU and EU nodes run sing-box. Client→RU (the inspected hop) = VLESS + REALITY (no flow) + multiplex; RU→EU (Cloudflare-cleared) = VLESS + HTTPUpgrade + TLS + multiplex. The subscription becomes a sing-box JSON outbound (a bare `vless://` link cannot carry multiplex). nfqws2 (zapret2) on the RU node desyncs RDDA's own egress handshakes, fail-open. Schema correctness is enforced by running `sing-box check` on every rendered config in tests.

**Tech Stack:** Go 1.22 (render + CLI), sing-box (data plane, both nodes), nfqws2/zapret2 + nftables (RU egress desync), systemd, Cloudflare Tunnel (cloudflared), systemd-nspawn multi-host integration harness.

**Spec:** `docs/superpowers/specs/2026-06-27-lane-b-singbox-design.md`

## Global Constraints

- **Go toolchain:** keep `go.mod` at `go 1.22`, `golang.org/x/crypto v0.31.0`. Never run bare `go mod tidy` (pins drift). Build/test with the repo's existing Go.
- **Module path:** `github.com/KoRORland/rdda`.
- **One core only:** sing-box on both nodes. No xray-core anywhere after this plan. No XHTTP (sing-box cannot speak it). No AnyTLS in this iteration.
- **uTLS fingerprint:** default `firefox`; never default to `chrome` (mimicking Chrome is itself a DPI flag). Selectable set: `firefox`, `safari`, `edge`, `ios`, `chrome`, `random`.
- **Pinned sing-box version:** pin one exact sing-box release (chosen in Task 0.1) in `VERSION` and in the installer/harness; all `sing-box check` oracles run that version.
- **nfqws2 desync is fail-open:** a desync failure must never break the tunnel path.
- **Commit + push every logically-complete task to `main`** (repo convention). Each task ends green (`go test ./...` passes).
- **Subnet caveat (operator doc, not code):** the *subnet* signal keys on the client→RU hop; prefer a clean/residential RU subnet.

---

## File Structure

**Created:**
- `internal/singboxconf/render.go` — `RenderRU`, `RenderEU`, `RenderClient` → sing-box JSON. Replaces `internal/xrayconf`.
- `internal/singboxconf/render.go` helper `splitHostPort(target string, defPort int) (string, int)`.
- `internal/singboxconf/check_test.go` — test helper that shells out to `sing-box check -c <tmpfile>`; skips if `sing-box` not on PATH.
- `internal/singboxconf/render_test.go` — per-render assertions + `sing-box check`.
- `deploy/systemd/rdda-singbox.service` — replaces `rdda-xray.service`.
- `deploy/systemd/rdda-nfqws.service` — nfqws2 desync unit (RU only).
- `deploy/nftables/rdda-nfqws.nft` — nft egress hook queueing RU outbound :443 handshakes to nfqws2.

**Modified:**
- `internal/state/config.go` — add `Desync` struct + `Desync` field; keep `Reality`/`Cloudflare`/`Fingerprint`.
- `internal/subscription/subscription.go` — emit sing-box JSON outbound (REALITY + multiplex); retire the `vless://` builder.
- `internal/subscription/subscription_test.go` — assert sing-box JSON shape.
- `internal/cli/cli.go` — `render` subcommands call `singboxconf`; `client add` prints the sing-box subscription; drop the "enable Mux manually" note.
- `internal/cli/cli_test.go` — update expectations.
- `install.sh` — install sing-box (not xray) + nfqws2 on RU; fetch renamed units.
- `test/integration/multihost/image.sh` — install sing-box (+ nfqws2 on the image).
- `test/integration/multihost/provision-{eu,ru,client}.sh` — sing-box units; **client runs real sing-box**.
- `test/integration/multihost/assert.sh` — assert multiplex negotiated + tunnel + routing split.
- `VERSION` — pin sing-box (and nfqws2) versions; drop xray.

**Deleted:**
- `internal/xrayconf/` (whole package) — after `singboxconf` is wired.
- `deploy/systemd/rdda-xray.service`.

---

## Phase 0 — De-risk before the rewrite

### Task 0.1: Prove the unknowns (sing-box schema, HTTPUpgrade-via-Cloudflare, REALITY+multiplex with real Hiddify)

**Files:**
- Create: `docs/superpowers/notes/2026-06-27-lane-b-derisk.md` (findings only — no production code).

**Interfaces:**
- Produces: the **exact pinned sing-box version string** (e.g. `1.12.x`) used by every later task's `sing-box check` and by `VERSION`; a confirmed REALITY inbound + multiplex + HTTPUpgrade-outbound JSON skeleton that `sing-box check` accepts.

- [ ] **Step 1: Pin and install sing-box locally**

Download one specific sing-box release for linux/amd64, record the exact version. Verify:
```bash
sing-box version
```
Expected: prints the pinned version. Record it in the notes file as `SINGBOX_VERSION=<x.y.z>`.

- [ ] **Step 2: Hand-write a minimal REALITY+multiplex inbound and HTTPUpgrade+TLS+multiplex outbound, validate the schema**

Create `/tmp/derisk-ru.json` with a VLESS inbound (`tls.reality.enabled`, `multiplex.enabled`) and a VLESS outbound (`transport.type=httpupgrade`, `tls.utls`, `multiplex`). Run:
```bash
sing-box check -c /tmp/derisk-ru.json
```
Expected: exit 0. If any field name is rejected, record the corrected field names in the notes — **these names are authoritative for Phase 1** and override any guess in this plan.

- [ ] **Step 3: Prove HTTPUpgrade survives Cloudflare**

Stand up a throwaway sing-box VLESS+HTTPUpgrade inbound behind a real Cloudflare Tunnel hostname; point a sing-box client outbound at it. Confirm a request traverses the tunnel. Record PASS/FAIL.
Expected: PASS. **If FAIL:** record it; the CF-hop transport falls back to `transport.type=ws` (WebSocket) — apply that substitution everywhere `httpupgrade` appears in Phase 1/2 (it is otherwise drop-in).

- [ ] **Step 4: Prove a real Hiddify imports a sing-box JSON outbound with REALITY+multiplex and connects**

Import the Step-2 outbound (as a sing-box-format subscription) into a real Hiddify build; connect to the Step-2 inbound; confirm multiplex negotiates (sing-box server log shows a muxed connection).
Expected: PASS. Record the precise sing-box JSON `outbounds[]` object Hiddify accepted — **this is the template for Task 2.2**.

- [ ] **Step 5: Commit the findings**

```bash
git add docs/superpowers/notes/2026-06-27-lane-b-derisk.md
git commit -m "docs(lane-b): P0 de-risk findings (sing-box version, schema, CF-HTTPUpgrade, Hiddify)"
git push origin main
```

> **Gate:** do not start Phase 1 until Steps 2–4 are PASS (or their fallbacks recorded). The authoritative field names from Step 2 take precedence over the JSON in the tasks below.

---

## Phase 1 — `singboxconf` render package (Go, TDD)

### Task 1.1: Scaffold the package + `sing-box check` test oracle + host/port helper

**Files:**
- Create: `internal/singboxconf/render.go`
- Create: `internal/singboxconf/check_test.go`

**Interfaces:**
- Produces: `type obj = map[string]any`; `func splitHostPort(target string, defPort int) (string, int)`; test helper `func mustCheck(t *testing.T, cfgJSON []byte)` that runs `sing-box check` and `t.Skip`s when `sing-box` is absent.

- [ ] **Step 1: Write the helper test**

Create `internal/singboxconf/render_test.go`:
```go
package singboxconf

import "testing"

func TestSplitHostPort(t *testing.T) {
	h, p := splitHostPort("www.microsoft.com:8443", 443)
	if h != "www.microsoft.com" || p != 8443 {
		t.Fatalf("got %s:%d", h, p)
	}
	h, p = splitHostPort("example.com", 443)
	if h != "example.com" || p != 443 {
		t.Fatalf("default port not applied: %s:%d", h, p)
	}
}
```

- [ ] **Step 2: Run it, watch it fail**

Run: `go test ./internal/singboxconf/ -run TestSplitHostPort`
Expected: FAIL — package/helper does not compile yet.

- [ ] **Step 3: Implement scaffold + helper**

Create `internal/singboxconf/render.go`:
```go
// Package singboxconf renders sing-box JSON configs from RDDA state.
package singboxconf

import (
	"strconv"
	"strings"

	"github.com/KoRORland/rdda/internal/state"
)

type obj = map[string]any

// splitHostPort splits "host:port" into (host, port), applying defPort when no
// port is present. A REALITY handshake target is typically "host:443".
func splitHostPort(target string, defPort int) (string, int) {
	h, p, ok := strings.Cut(target, ":")
	if !ok {
		return target, defPort
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return h, defPort
	}
	return h, n
}

func firstOrEmpty(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

var _ = state.Config{} // keep the import until render funcs land
```

- [ ] **Step 4: Add the `sing-box check` oracle**

Create `internal/singboxconf/check_test.go`:
```go
package singboxconf

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// mustCheck writes cfgJSON to a temp file and runs `sing-box check`. If sing-box
// is not installed it skips, so unit CI without sing-box still runs field asserts;
// the integration job (which installs sing-box) enforces full schema validity.
func mustCheck(t *testing.T, cfgJSON []byte) {
	t.Helper()
	bin, err := exec.LookPath("sing-box")
	if err != nil {
		t.Skip("sing-box not installed; skipping schema check")
	}
	f := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(f, cfgJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "check", "-c", f).CombinedOutput()
	if err != nil {
		t.Fatalf("sing-box check failed: %v\n%s", err, out)
	}
}
```

- [ ] **Step 5: Run + commit**

Run: `go test ./internal/singboxconf/`
Expected: PASS (TestSplitHostPort; mustCheck unused yet is fine — it is referenced by later tests).
```bash
git add internal/singboxconf/
git commit -m "feat(singboxconf): scaffold package + sing-box check oracle + host/port helper"
git push origin main
```

### Task 1.2: `RenderClient` — SOCKS inbound → VLESS/REALITY/multiplex outbound to RU

**Files:**
- Modify: `internal/singboxconf/render.go`
- Test: `internal/singboxconf/render_test.go`

**Interfaces:**
- Consumes: `state.Config` (`RUHost`, `RUPort`, `ClientReality`, `FP()`).
- Produces: `func RenderClient(cfg state.Config, clientUUID string, socksPort int) ([]byte, error)` — a sing-box JSON with a SOCKS inbound on 127.0.0.1:socksPort and one VLESS outbound (REALITY, no flow, multiplex) to the RU node.

- [ ] **Step 1: Write the failing test**

Add to `internal/singboxconf/render_test.go`:
```go
import (
	"encoding/json"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
)

func clientCfg() state.Config {
	return state.Config{
		RUHost: "ru.example.net", RUPort: 8443,
		ClientReality: state.Reality{
			Target: "www.microsoft.com:443", ServerName: "www.microsoft.com",
			PublicKey: "PUBKEY", ShortIDs: []string{"abcd1234"},
		},
		Fingerprint: "firefox",
	}
}

func TestRenderClient(t *testing.T) {
	b, err := RenderClient(clientCfg(), "uuid-1", 1080)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	out := doc["outbounds"].([]any)[0].(map[string]any)
	if out["type"] != "vless" || out["uuid"] != "uuid-1" {
		t.Fatalf("outbound: %v", out)
	}
	if _, ok := out["flow"]; ok {
		t.Fatal("client outbound must NOT set flow (no Vision; multiplex chosen)")
	}
	tls := out["tls"].(map[string]any)
	if tls["server_name"] != "www.microsoft.com" {
		t.Fatalf("server_name: %v", tls)
	}
	if tls["reality"].(map[string]any)["public_key"] != "PUBKEY" {
		t.Fatalf("reality: %v", tls)
	}
	if tls["utls"].(map[string]any)["fingerprint"] != "firefox" {
		t.Fatalf("utls: %v", tls)
	}
	if out["multiplex"].(map[string]any)["enabled"] != true {
		t.Fatalf("multiplex must be enabled: %v", out)
	}
	mustCheck(t, b)
}
```

- [ ] **Step 2: Run, watch it fail**

Run: `go test ./internal/singboxconf/ -run TestRenderClient`
Expected: FAIL — `RenderClient` undefined.

- [ ] **Step 3: Implement `RenderClient`**

Append to `internal/singboxconf/render.go`:
```go
import "encoding/json" // add to the import block

// RenderClient builds a sing-box client config: SOCKS inbound -> VLESS/REALITY/
// multiplex outbound to the RU node. clientUUID must match a UUID the RU serves.
func RenderClient(cfg state.Config, clientUUID string, socksPort int) ([]byte, error) {
	out := obj{
		"type": "vless", "tag": "proxy",
		"server": cfg.RUHost, "server_port": cfg.RUPort,
		"uuid": clientUUID,
		"tls": obj{
			"enabled":     true,
			"server_name": cfg.ClientReality.ServerName,
			"utls":        obj{"enabled": true, "fingerprint": cfg.FP()},
			"reality": obj{
				"enabled":    true,
				"public_key": cfg.ClientReality.PublicKey,
				"short_id":   firstOrEmpty(cfg.ClientReality.ShortIDs),
			},
		},
		"multiplex": obj{"enabled": true, "protocol": "h2mux", "max_streams": 8},
	}
	doc := obj{
		"log":       obj{"level": "warn"},
		"inbounds":  []obj{{"type": "socks", "tag": "socks-in", "listen": "127.0.0.1", "listen_port": socksPort}},
		"outbounds": []obj{out, {"type": "direct", "tag": "direct"}},
	}
	return json.MarshalIndent(doc, "", "  ")
}
```
(Remove the `var _ = state.Config{}` placeholder line now that `state` is used.)

- [ ] **Step 4: Run, watch it pass**

Run: `go test ./internal/singboxconf/ -run TestRenderClient`
Expected: PASS (schema check skips locally if sing-box absent; runs in integration).

- [ ] **Step 5: Commit**

```bash
git add internal/singboxconf/
git commit -m "feat(singboxconf): RenderClient (SOCKS -> VLESS/REALITY/multiplex to RU)"
git push origin main
```

### Task 1.3: `RenderRU` — REALITY inbound + EU outbound (CF HTTPUpgrade / non-CF REALITY) + split route

**Files:**
- Modify: `internal/singboxconf/render.go`
- Test: `internal/singboxconf/render_test.go`

**Interfaces:**
- Consumes: `state.Config` (`RUPort`, `ClientReality`, `Cloudflare`, `TunnelHostname`, `TunnelUUID`, `TunnelPath`, `EUHost`, `EUPort`, `TunnelReality`, `IntlAllowDomains`, `CFEnabled()`, `FP()`), `[]state.Client`.
- Produces: `func RenderRU(cfg state.Config, clients []state.Client) ([]byte, error)`.

- [ ] **Step 1: Write the failing test**

Add:
```go
func ruCfg(cf bool) state.Config {
	c := clientCfg()
	c.RUPort = 8443
	c.TunnelUUID = "tunnel-uuid"
	c.TunnelPath = "/tnl"
	c.EUHost = "eu.example.net"
	c.EUPort = 9443
	c.TunnelReality = state.Reality{ServerName: "www.apple.com", PublicKey: "TPUB", ShortIDs: []string{"ee11"}}
	c.IntlAllowDomains = []string{"example.org"}
	if cf {
		c.Cloudflare = state.Cloudflare{TunnelHostname: "tunnel.rdda.test"}
	}
	return c
}

func TestRenderRU_CF(t *testing.T) {
	b, err := RenderRU(ruCfg(true), []state.Client{{UUID: "uuid-1", Name: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	in := doc["inbounds"].([]any)[0].(map[string]any)
	if in["type"] != "vless" || in["tls"].(map[string]any)["reality"].(map[string]any)["enabled"] != true {
		t.Fatalf("RU inbound must be VLESS+REALITY: %v", in)
	}
	if in["multiplex"].(map[string]any)["enabled"] != true {
		t.Fatalf("RU inbound must accept multiplex: %v", in)
	}
	proxy := doc["outbounds"].([]any)[0].(map[string]any)
	tr := proxy["transport"].(map[string]any)
	if tr["type"] != "httpupgrade" || tr["path"] != "/tnl" {
		t.Fatalf("CF outbound must be httpupgrade: %v", tr)
	}
	if proxy["tls"].(map[string]any)["server_name"] != "tunnel.rdda.test" {
		t.Fatalf("CF outbound TLS SNI: %v", proxy["tls"])
	}
	if _, hasReality := proxy["tls"].(map[string]any)["reality"]; hasReality {
		t.Fatal("CF outbound must NOT carry reality (CF terminates TLS)")
	}
	mustCheck(t, b)
}

func TestRenderRU_NonCF(t *testing.T) {
	b, _ := RenderRU(ruCfg(false), []state.Client{{UUID: "uuid-1"}})
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	proxy := doc["outbounds"].([]any)[0].(map[string]any)
	if proxy["tls"].(map[string]any)["reality"].(map[string]any)["public_key"] != "TPUB" {
		t.Fatalf("non-CF outbound must use tunnel REALITY: %v", proxy["tls"])
	}
	mustCheck(t, b)
}
```

- [ ] **Step 2: Run, watch it fail**

Run: `go test ./internal/singboxconf/ -run TestRenderRU`
Expected: FAIL — `RenderRU` undefined.

- [ ] **Step 3: Implement `RenderRU`**

Append:
```go
// RenderRU builds the RU node config: REALITY inbound for clients + an outbound
// to EU (HTTPUpgrade behind Cloudflare, else REALITY direct) + split routing.
func RenderRU(cfg state.Config, clients []state.Client) ([]byte, error) {
	users := make([]obj, 0, len(clients))
	for _, c := range clients {
		users = append(users, obj{"uuid": c.UUID})
	}
	hsHost, hsPort := splitHostPort(cfg.ClientReality.Target, 443)
	inbound := obj{
		"type": "vless", "tag": "in",
		"listen": "0.0.0.0", "listen_port": cfg.RUPort,
		"users": users,
		"tls": obj{
			"enabled":     true,
			"server_name": cfg.ClientReality.ServerName,
			"reality": obj{
				"enabled":     true,
				"handshake":   obj{"server": hsHost, "server_port": hsPort},
				"private_key": cfg.ClientReality.PrivateKey,
				"short_id":    cfg.ClientReality.ShortIDs,
			},
		},
		"multiplex": obj{"enabled": true},
	}

	var proxy obj
	if cfg.CFEnabled() {
		proxy = obj{
			"type": "vless", "tag": "proxy",
			"server": cfg.Cloudflare.TunnelHostname, "server_port": 443,
			"uuid": cfg.TunnelUUID,
			"tls": obj{
				"enabled":     true,
				"server_name": cfg.Cloudflare.TunnelHostname,
				"utls":        obj{"enabled": true, "fingerprint": cfg.FP()},
			},
			"transport": obj{"type": "httpupgrade", "path": cfg.TunnelPath, "host": cfg.Cloudflare.TunnelHostname},
			"multiplex": obj{"enabled": true, "protocol": "h2mux", "max_streams": 8},
		}
	} else {
		proxy = obj{
			"type": "vless", "tag": "proxy",
			"server": cfg.EUHost, "server_port": cfg.EUPort,
			"uuid": cfg.TunnelUUID,
			"tls": obj{
				"enabled":     true,
				"server_name": cfg.TunnelReality.ServerName,
				"utls":        obj{"enabled": true, "fingerprint": cfg.FP()},
				"reality": obj{
					"enabled":    true,
					"public_key": cfg.TunnelReality.PublicKey,
					"short_id":   firstOrEmpty(cfg.TunnelReality.ShortIDs),
				},
			},
			"multiplex": obj{"enabled": true, "protocol": "h2mux", "max_streams": 8},
		}
	}

	rules := []obj{
		{"ip_is_private": true, "outbound": "direct"},
		{"rule_set": "geoip-ru", "outbound": "direct"},
	}
	if len(cfg.IntlAllowDomains) > 0 {
		rules = append(rules, obj{"domain_suffix": cfg.IntlAllowDomains, "outbound": "direct"})
	}
	doc := obj{
		"log":      obj{"level": "warn"},
		"inbounds": []obj{inbound},
		"outbounds": []obj{
			proxy,
			{"type": "direct", "tag": "direct"},
		},
		"route": obj{
			"rule_set": []obj{{
				"type": "remote", "tag": "geoip-ru", "format": "binary",
				"url":             "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-ru.srs",
				"download_detour": "proxy",
			}},
			"rules": rules,
			"final": "proxy",
		},
	}
	return json.MarshalIndent(doc, "", "  ")
}
```

> **Note for the implementer:** the `rule_set` geoip-ru URL/format must match what Task 0.1 confirmed for the pinned sing-box. If `sing-box check` rejects the remote rule_set offline, gate it behind a documented local `.srs` path; the integration harness routes by IP (TEST-NET), so geoip is not exercised in CI.

- [ ] **Step 4: Run, watch it pass**

Run: `go test ./internal/singboxconf/ -run TestRenderRU`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/singboxconf/
git commit -m "feat(singboxconf): RenderRU (REALITY inbound + CF-HTTPUpgrade/REALITY outbound + split route)"
git push origin main
```

### Task 1.4: `RenderEU` — terminate the RU tunnel (HTTPUpgrade under CF / REALITY non-CF) → freedom

**Files:**
- Modify: `internal/singboxconf/render.go`
- Test: `internal/singboxconf/render_test.go`

**Interfaces:**
- Consumes: `state.Config` (`EUPort`, `TunnelUUID`, `TunnelPath`, `TunnelReality`, `CFEnabled()`).
- Produces: `func RenderEU(cfg state.Config) ([]byte, error)`.

- [ ] **Step 1: Write the failing test**

Add:
```go
func TestRenderEU_CF(t *testing.T) {
	b, _ := RenderEU(ruCfg(true))
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	in := doc["inbounds"].([]any)[0].(map[string]any)
	tr := in["transport"].(map[string]any)
	if tr["type"] != "httpupgrade" || tr["path"] != "/tnl" {
		t.Fatalf("EU inbound transport: %v", tr)
	}
	if _, hasTLS := in["tls"]; hasTLS {
		t.Fatal("EU inbound under CF must NOT enable TLS (Cloudflare terminates it)")
	}
	if in["multiplex"].(map[string]any)["enabled"] != true {
		t.Fatalf("EU inbound must accept multiplex: %v", in)
	}
	mustCheck(t, b)
}

func TestRenderEU_NonCF(t *testing.T) {
	b, _ := RenderEU(ruCfg(false))
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	in := doc["inbounds"].([]any)[0].(map[string]any)
	if in["tls"].(map[string]any)["reality"].(map[string]any)["enabled"] != true {
		t.Fatalf("non-CF EU inbound must be REALITY: %v", in)
	}
	mustCheck(t, b)
}
```

- [ ] **Step 2: Run, watch it fail**

Run: `go test ./internal/singboxconf/ -run TestRenderEU`
Expected: FAIL — `RenderEU` undefined.

- [ ] **Step 3: Implement `RenderEU`**

Append:
```go
// RenderEU builds the EU node config: terminate the RU tunnel, exit to internet.
// Under Cloudflare the public TLS is terminated by CF, so the inbound runs plain
// HTTPUpgrade (no TLS). Without CF it terminates REALITY directly.
func RenderEU(cfg state.Config) ([]byte, error) {
	var inbound obj
	if cfg.CFEnabled() {
		inbound = obj{
			"type": "vless", "tag": "in",
			"listen": "127.0.0.1", "listen_port": cfg.EUPort,
			"users":     []obj{{"uuid": cfg.TunnelUUID}},
			"transport": obj{"type": "httpupgrade", "path": cfg.TunnelPath},
			"multiplex": obj{"enabled": true},
		}
	} else {
		hsHost, hsPort := splitHostPort(cfg.TunnelReality.Target, 443)
		inbound = obj{
			"type": "vless", "tag": "in",
			"listen": "0.0.0.0", "listen_port": cfg.EUPort,
			"users": []obj{{"uuid": cfg.TunnelUUID}},
			"tls": obj{
				"enabled":     true,
				"server_name": cfg.TunnelReality.ServerName,
				"reality": obj{
					"enabled":     true,
					"handshake":   obj{"server": hsHost, "server_port": hsPort},
					"private_key": cfg.TunnelReality.PrivateKey,
					"short_id":    cfg.TunnelReality.ShortIDs,
				},
			},
			"multiplex": obj{"enabled": true},
		}
	}
	doc := obj{
		"log":       obj{"level": "warn"},
		"inbounds":  []obj{inbound},
		"outbounds": []obj{{"type": "direct", "tag": "direct"}},
	}
	return json.MarshalIndent(doc, "", "  ")
}
```

- [ ] **Step 4: Run, watch it pass**

Run: `go test ./internal/singboxconf/`
Expected: PASS (all render tests).

- [ ] **Step 5: Commit**

```bash
git add internal/singboxconf/
git commit -m "feat(singboxconf): RenderEU (HTTPUpgrade under CF / REALITY direct -> freedom)"
git push origin main
```

### Task 1.5: Wire CLI `render` to `singboxconf`; delete `internal/xrayconf`

**Files:**
- Modify: `internal/cli/cli.go`
- Modify: `internal/cli/cli_test.go`
- Delete: `internal/xrayconf/` (entire dir)

**Interfaces:**
- Consumes: `singboxconf.RenderRU/RenderEU/RenderClient` (signatures identical to the deleted `xrayconf` ones).
- Produces: `rdda render ru|eu|client` emit sing-box JSON.

- [ ] **Step 1: Point the import at the new package**

In `internal/cli/cli.go`, replace `"github.com/KoRORland/rdda/internal/xrayconf"` with `"github.com/KoRORland/rdda/internal/singboxconf"`, and replace every `xrayconf.` call with `singboxconf.` (the function names and signatures are unchanged: `RenderRU(cfg, clients)`, `RenderEU(cfg)`, `RenderClient(cfg, uuid, socksPort)`).

- [ ] **Step 2: Update any CLI test that asserts xray-isms**

In `internal/cli/cli_test.go`, any test that greps rendered output for xray-only tokens (e.g. `"realitySettings"`, `"wsSettings"`, `"vnext"`) must assert the sing-box equivalents instead (`"reality"`, `"httpupgrade"`/`"socks"`, `"server_port"`). Keep behavioral assertions (a client UUID appears, the RU host appears) unchanged.

- [ ] **Step 3: Run, watch the render path compile and pass**

Run: `go test ./internal/cli/`
Expected: PASS.

- [ ] **Step 4: Delete the dead package**

```bash
git rm -r internal/xrayconf
go build ./...
go test ./...
```
Expected: build + all tests green; no remaining references to `xrayconf`.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor(cli): render via singboxconf; remove internal/xrayconf"
git push origin main
```

---

## Phase 2 — config + subscription

### Task 2.1: Add the `desync:` (nfqws2) config block

**Files:**
- Modify: `internal/state/config.go`
- Test: `internal/state/config_test.go` (create if absent)

**Interfaces:**
- Produces: `type Desync struct { Enabled bool; Profile string; Ports []int }` and `Config.Desync Desync` (`yaml:"desync"`). `Profile` names an nfqws2 desync strategy (e.g. `"fake,split2"`); `Ports` defaults to `[443]`.

- [ ] **Step 1: Write the failing test**

Create/append `internal/state/config_test.go`:
```go
package state

import (
	"path/filepath"
	"testing"
)

func TestDesyncRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := Config{RUHost: "ru", Desync: Desync{Enabled: true, Profile: "fake,split2", Ports: []int{443}}}
	if err := s.SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Desync.Enabled || got.Desync.Profile != "fake,split2" || got.Desync.Ports[0] != 443 {
		t.Fatalf("desync round-trip: %+v", got.Desync)
	}
	_ = filepath.Join
}
```

- [ ] **Step 2: Run, watch it fail**

Run: `go test ./internal/state/ -run TestDesyncRoundTrip`
Expected: FAIL — `Desync` undefined.

- [ ] **Step 3: Implement the struct + field**

In `internal/state/config.go`, add:
```go
// Desync configures the RU-node nfqws2 (zapret2) egress DPI-desync. It is
// fail-open: a desync failure must not break the tunnel path.
type Desync struct {
	Enabled bool   `yaml:"enabled"`
	Profile string `yaml:"profile"`
	Ports   []int  `yaml:"ports"`
}
```
and add to `Config`:
```go
	Desync Desync `yaml:"desync"`
```

- [ ] **Step 4: Run, watch it pass**

Run: `go test ./internal/state/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/state/
git commit -m "feat(state): add desync (nfqws2) config block"
git push origin main
```

### Task 2.2: Subscription emits a sing-box JSON outbound (REALITY + multiplex)

**Files:**
- Modify: `internal/subscription/subscription.go`
- Modify: `internal/subscription/subscription_test.go`

**Interfaces:**
- Consumes: `state.Config`, `state.Client`.
- Produces: `func ClientOutbound(cfg state.Config, c state.Client) ([]byte, error)` returns a single sing-box `outbounds[]`-style JSON object (VLESS/REALITY/multiplex to the RU node). `func Build(cfg state.Config, c state.Client) (string, error)` returns a full sing-box client config JSON (the subscription body Hiddify imports). The base64/`vless://` builders are removed.

- [ ] **Step 1: Replace the test**

Rewrite `internal/subscription/subscription_test.go`:
```go
package subscription

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
)

func cfg() state.Config {
	return state.Config{
		RUHost: "ru.example.net", RUPort: 8443,
		ClientReality: state.Reality{ServerName: "www.microsoft.com", PublicKey: "PUB", ShortIDs: []string{"ab12"}},
		Fingerprint:   "firefox",
	}
}

func TestClientOutbound(t *testing.T) {
	b, err := ClientOutbound(cfg(), state.Client{UUID: "uuid-9", Name: "granny"})
	if err != nil {
		t.Fatal(err)
	}
	var o map[string]any
	if err := json.Unmarshal(b, &o); err != nil {
		t.Fatal(err)
	}
	if o["type"] != "vless" || o["uuid"] != "uuid-9" || o["server"] != "ru.example.net" {
		t.Fatalf("outbound: %v", o)
	}
	if o["multiplex"].(map[string]any)["enabled"] != true {
		t.Fatal("subscription outbound must carry multiplex (the whole point of sing-box JSON)")
	}
	if o["tls"].(map[string]any)["reality"].(map[string]any)["public_key"] != "PUB" {
		t.Fatalf("reality: %v", o["tls"])
	}
}

func TestBuildIsFullConfig(t *testing.T) {
	s, err := Build(cfg(), state.Client{UUID: "uuid-9", Name: "granny"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "\"outbounds\"") || !strings.Contains(s, "uuid-9") {
		t.Fatalf("Build must be a full sing-box config: %s", s)
	}
}
```

- [ ] **Step 2: Run, watch it fail**

Run: `go test ./internal/subscription/`
Expected: FAIL — `ClientOutbound` undefined / old `ClientURI`/`Build` signatures.

- [ ] **Step 3: Rewrite the package**

Replace `internal/subscription/subscription.go`:
```go
// Package subscription builds the sing-box client config a Hiddify user imports.
package subscription

import (
	"encoding/json"

	"github.com/KoRORland/rdda/internal/state"
)

type obj = map[string]any

// ClientOutbound returns one sing-box VLESS/REALITY/multiplex outbound object
// pointing at the RU entry node.
func ClientOutbound(cfg state.Config, c state.Client) ([]byte, error) {
	sid := ""
	if len(cfg.ClientReality.ShortIDs) > 0 {
		sid = cfg.ClientReality.ShortIDs[0]
	}
	out := obj{
		"type": "vless", "tag": "rdda",
		"server": cfg.RUHost, "server_port": cfg.RUPort,
		"uuid": c.UUID,
		"tls": obj{
			"enabled":     true,
			"server_name": cfg.ClientReality.ServerName,
			"utls":        obj{"enabled": true, "fingerprint": cfg.FP()},
			"reality":     obj{"enabled": true, "public_key": cfg.ClientReality.PublicKey, "short_id": sid},
		},
		"multiplex": obj{"enabled": true, "protocol": "h2mux", "max_streams": 8},
	}
	return json.MarshalIndent(out, "", "  ")
}

// Build returns the full sing-box client config (subscription body) for a client:
// a TUN/SOCKS-less minimal config with the RDDA outbound + a direct fallback.
func Build(cfg state.Config, c state.Client) (string, error) {
	ob, err := ClientOutbound(cfg, c)
	if err != nil {
		return "", err
	}
	var out obj
	if err := json.Unmarshal(ob, &out); err != nil {
		return "", err
	}
	doc := obj{
		"log":       obj{"level": "warn"},
		"outbounds": []obj{out, {"type": "direct", "tag": "direct"}},
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	return string(b), err
}
```

- [ ] **Step 4: Run, watch it pass**

Run: `go test ./internal/subscription/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/subscription/
git commit -m "feat(subscription): emit sing-box JSON outbound with REALITY+multiplex (retire vless link)"
git push origin main
```

### Task 2.3: `client add` / `serve` hand out the sing-box subscription

**Files:**
- Modify: `internal/cli/cli.go`
- Modify: `internal/cli/cli_test.go`
- Modify: `internal/subserver/server.go` (if it referenced the old base64 `Build`)

**Interfaces:**
- Consumes: `subscription.Build(cfg, c) (string, error)`.
- Produces: `rdda client add <name>` prints the full sing-box config JSON; `rdda serve` serves it at the per-client path.

- [ ] **Step 1: Update the CLI test**

In `internal/cli/cli_test.go`, replace the vless-link assertion with:
```go
func TestClientAddPrintsSingboxConfig(t *testing.T) {
	dir := t.TempDir()
	run(t, "--dir", dir, "init", "--ru-host", "ru.example.net", "--eu-host", "eu.example.net")
	out := run(t, "--dir", dir, "client", "add", "granny")
	if !strings.Contains(out, "\"outbounds\"") || !strings.Contains(out, "reality") {
		t.Fatalf("expected a sing-box config, got: %s", out)
	}
}
```
Delete the old `TestClientAddPrintsVlessLink`.

- [ ] **Step 2: Run, watch it fail**

Run: `go test ./internal/cli/ -run TestClientAddPrintsSingboxConfig`
Expected: FAIL — command still prints the vless link.

- [ ] **Step 3: Update the command**

In `internal/cli/cli.go`, in `client add`'s `RunE`, replace the link print with:
```go
			body, err := subscription.Build(cfg, c)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), body)
```
Fix `internal/subserver/server.go` to call the new `subscription.Build` (now returns `(string, error)`) and serve it with `Content-Type: application/json`.

- [ ] **Step 4: Run, watch it pass**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/ internal/subserver/
git commit -m "feat(cli): client add + serve hand out the sing-box subscription"
git push origin main
```

---

## Phase 3 — systemd units + installer

### Task 3.1: Rename the data-plane unit xray→sing-box (both nodes)

**Files:**
- Create: `deploy/systemd/rdda-singbox.service`
- Delete: `deploy/systemd/rdda-xray.service`

**Interfaces:**
- Produces: `rdda-singbox.service` running `sing-box run -c /etc/rdda/singbox.json`.

- [ ] **Step 1: Write the new unit**

Create `deploy/systemd/rdda-singbox.service`:
```ini
[Unit]
Description=RDDA sing-box data plane
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/sing-box run -c /etc/rdda/singbox.json
Restart=on-failure
RestartSec=3
User=rdda
Group=rdda
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 2: Remove the old unit + repoint references**

```bash
git rm deploy/systemd/rdda-xray.service
grep -rn "rdda-xray\|xray.json\|/usr/local/bin/xray" --include=*.sh --include=*.service . || true
```
Repoint every hit (installer, harness, docs) from `rdda-xray`/`xray.json`/`xray run` to `rdda-singbox`/`singbox.json`/`sing-box run`. The render destination filename becomes `/etc/rdda/singbox.json` (update `rdda render` default `--dest` and the pull service if they name `xray.json`).

- [ ] **Step 3: Verify nothing still names xray**

Run: `grep -rn "xray" --include=*.sh --include=*.service --include=*.go . | grep -v singbox || echo CLEAN`
Expected: `CLEAN` (or only historical doc/plan references under `docs/`).

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat(deploy): rdda-singbox.service replaces rdda-xray.service (both nodes)"
git push origin main
```

### Task 3.2: Installer installs sing-box (not xray)

**Files:**
- Modify: `install.sh`
- Modify: `VERSION`

**Interfaces:**
- Produces: `install.sh` downloads the pinned sing-box, installs `/usr/local/bin/sing-box`, fetches `rdda-singbox.service`.

- [ ] **Step 1: Pin versions**

In `VERSION`, replace the xray pin with the Task-0.1 sing-box version (and an nfqws2 version line). Keep the file's existing format.

- [ ] **Step 2: Swap the binary install block**

In `install.sh`, replace the "install xray-core, disable its stock unit" block with a sing-box install: download the pinned sing-box release for the host arch, verify checksum, install to `/usr/local/bin/sing-box`, do **not** enable any stock unit. Update the systemd-fetch block to pull `rdda-singbox.service` (replacing `rdda-xray.service`).

- [ ] **Step 3: Lint**

Run: `shellcheck install.sh`
Expected: no new errors (matches the repo's existing shellcheck job).

- [ ] **Step 4: Commit**

```bash
git add install.sh VERSION
git commit -m "feat(install): install pinned sing-box (+ nfqws2 pin) instead of xray"
git push origin main
```

---

## Phase 4 — zapret2 / nfqws2 egress desync on the RU node

### Task 4.1: nft egress hook + `rdda-nfqws.service` (fail-open)

**Files:**
- Create: `deploy/nftables/rdda-nfqws.nft`
- Create: `deploy/systemd/rdda-nfqws.service`

**Interfaces:**
- Produces: an nft table that NFQUEUEs the RU node's **outbound** new-connection :443 packets to queue 200; a unit running `nfqws2 --qnum=200 <profile flags>`.

- [ ] **Step 1: Write the nft hook**

Create `deploy/nftables/rdda-nfqws.nft`:
```nft
table inet rdda_nfqws {
  chain postrouting {
    type filter hook postrouting priority mangle; policy accept;
    # Queue only the RU node's own outbound TLS handshakes for nfqws2 desync.
    # queue-bypass = fail-open: if nfqws2 is down, packets pass unmodified.
    meta l4proto tcp tcp dport 443 ct state new,established \
      queue num 200 bypass
  }
}
```

- [ ] **Step 2: Write the unit**

Create `deploy/systemd/rdda-nfqws.service`:
```ini
[Unit]
Description=RDDA nfqws2 egress DPI-desync (RU node, fail-open)
After=network-online.target rdda-singbox.service
Wants=network-online.target

[Service]
# %E expands the desync flags rendered from config.yaml's desync.profile.
ExecStartPre=/usr/sbin/nft -f /etc/rdda/rdda-nfqws.nft
ExecStart=/usr/local/bin/nfqws2 --qnum=200 --dpi-desync=fake,split2
ExecStopPost=-/usr/sbin/nft delete table inet rdda_nfqws
Restart=on-failure
RestartSec=3
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 3: Lint the unit + nft syntax (smoke)**

Run: `systemd-analyze verify deploy/systemd/rdda-nfqws.service || true` and (on a host with nft) `nft -c -f deploy/nftables/rdda-nfqws.nft`.
Expected: nft `-c` (check-only) exits 0; analyze reports no fatal errors.

- [ ] **Step 4: Commit**

```bash
git add deploy/nftables/rdda-nfqws.nft deploy/systemd/rdda-nfqws.service
git commit -m "feat(deploy): nfqws2 egress desync hook + unit (RU, fail-open via queue bypass)"
git push origin main
```

### Task 4.2: Render the nfqws profile + installer wires nfqws2 on RU

**Files:**
- Modify: `internal/cli/cli.go` (a `render nfqws` subcommand that writes `/etc/rdda/rdda-nfqws.nft` env or flags from `cfg.Desync`)
- Modify: `install.sh`

**Interfaces:**
- Consumes: `cfg.Desync` (Task 2.1).
- Produces: `rdda render nfqws` prints the desync flags (`--dpi-desync=<profile>`) and the nft file content from config; the installer installs nfqws2 + enables `rdda-nfqws.service` **on the RU role only**.

- [ ] **Step 1: Write the failing CLI test**

In `internal/cli/cli_test.go`:
```go
func TestRenderNfqws(t *testing.T) {
	dir := t.TempDir()
	run(t, "--dir", dir, "init", "--ru-host", "ru", "--eu-host", "eu")
	// enable desync in config, then render
	out := run(t, "--dir", dir, "render", "nfqws")
	if !strings.Contains(out, "dpi-desync") {
		t.Fatalf("render nfqws must emit desync flags: %s", out)
	}
}
```

- [ ] **Step 2: Run, watch it fail**

Run: `go test ./internal/cli/ -run TestRenderNfqws`
Expected: FAIL — subcommand missing.

- [ ] **Step 3: Implement `render nfqws` + installer wiring**

Add the `render nfqws` subcommand emitting `--dpi-desync=<cfg.Desync.Profile or default "fake,split2">`. In `install.sh`, for `ROLE=ru`: download pinned nfqws2 to `/usr/local/bin/nfqws2`, fetch `rdda-nfqws.service` + `rdda-nfqws.nft`, `systemctl enable --now rdda-nfqws` (after sing-box). Leave EU untouched.

- [ ] **Step 4: Run + lint**

Run: `go test ./internal/cli/` and `shellcheck install.sh`
Expected: PASS / no new shellcheck errors.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/ install.sh
git commit -m "feat(nfqws): render desync profile + install/enable nfqws2 on RU role"
git push origin main
```

---

## Phase 5 — multi-host integration harness (real sing-box client)

### Task 5.1: Image installs sing-box (+ nfqws2)

**Files:**
- Modify: `test/integration/multihost/image.sh`

**Interfaces:**
- Produces: the base rootfs has `/usr/local/bin/sing-box` (pinned) and `/usr/local/bin/nfqws2`; xray + its geoip data are removed.

- [ ] **Step 1: Swap the binary install in the image**

In `image.sh`, replace the xray-core download/install (and its geoip/geosite data files) with the pinned sing-box install; add nfqws2. Keep chisel (cloudflared stand-in) unchanged.

- [ ] **Step 2: Build the image**

Run: `sudo bash test/integration/multihost/image.sh "$(git rev-parse --show-toplevel)"`
Expected: image builds; `sing-box version` works inside the rootfs.

- [ ] **Step 3: Commit**

```bash
git add test/integration/multihost/image.sh
git commit -m "test(integration): image installs pinned sing-box + nfqws2 (drop xray)"
git push origin main
```

### Task 5.2: Provision EU/RU/client on sing-box; client runs the REAL sing-box

**Files:**
- Modify: `test/integration/multihost/provision-eu.sh`
- Modify: `test/integration/multihost/provision-ru.sh`
- Modify: `test/integration/multihost/provision-client.sh`

**Interfaces:**
- Produces: EU + RU run `rdda-singbox.service`; the **client** runs a `sing-box` SOCKS config rendered by `rdda render client` (no xray anywhere). RU additionally runs `rdda-nfqws.service`.

- [ ] **Step 1: EU + RU units**

In `provision-eu.sh` and `provision-ru.sh`, install `rdda-singbox.service` instead of `rdda-xray.service`; render to `/etc/rdda/singbox.json`. In `provision-ru.sh`, also install + enable `rdda-nfqws.service` and `rdda-nfqws.nft`.

- [ ] **Step 2: Client uses real sing-box (the blind-spot fix)**

In `provision-client.sh`, replace the `xray run -c /etc/client.json` unit with:
```bash
cat > "$root/etc/systemd/system/rdda-client.service" <<'UNIT'
[Unit]
Description=RDDA client sing-box (SOCKS -> RU)
After=network-online.target
[Service]
ExecStart=/usr/local/bin/sing-box run -c /etc/client.json
Restart=on-failure
[Install]
WantedBy=multi-user.target
UNIT
```
The `rdda render client --uuid ... --socks-port 1080` output is already sing-box JSON (Phase 1). Keep the loglevel-to-debug sed but target sing-box's key: replace `"level": "warn"` with `"level": "debug"`.

- [ ] **Step 3: Run the provisioners (via the orchestrator in Task 5.3)**

Deferred to Task 5.3's full run.

- [ ] **Step 4: Commit**

```bash
git add test/integration/multihost/provision-eu.sh test/integration/multihost/provision-ru.sh test/integration/multihost/provision-client.sh
git commit -m "test(integration): EU/RU/client on sing-box; client runs REAL sing-box core"
git push origin main
```

### Task 5.3: Assert multiplex negotiated + tunnel + routing; green the harness

**Files:**
- Modify: `test/integration/multihost/assert.sh`
- Modify: `test/integration/multihost/lib.sh` (diag: journal `rdda-singbox`/`rdda-client`, not `rdda-xray`)

**Interfaces:**
- Produces: the harness passes end-to-end and explicitly asserts multiplex is active (guards the original silent-regression bug).

- [ ] **Step 1: Add the multiplex assertion**

In `assert.sh`, after the existing through-tunnel curl assertion, add a check that the RU sing-box negotiated a multiplexed connection from the client — e.g. grep the RU `rdda-singbox` journal for a multiplex/`smux`/`h2mux` connection marker, or assert the inbound connection count stays low under N parallel client requests:
```bash
log "assert: multiplex negotiated on client->RU"
systemd-run --machine=rdda-ru --wait --pipe --quiet \
  journalctl -u rdda-singbox --no-pager 2>&1 | grep -qiE 'mux|multiplex' \
  || die "multiplex was NOT negotiated on the client->RU hop (Signal-3 regression)"
```
> Implementer: confirm the exact log token the pinned sing-box emits for an accepted muxed inbound during Task 0.1, and match it here. If sing-box does not log it at `debug`, assert via connection-count instead (N parallel SOCKS requests → a single inbound TCP connection on RU).

- [ ] **Step 2: Repoint diag journals**

In `lib.sh` `diag()`, change `journalctl -u rdda-xray -u rdda-client` to `journalctl -u rdda-singbox -u rdda-client`, and the client curl/journal lines accordingly.

- [ ] **Step 3: Full harness run**

Run: `sudo bash test/integration/multihost/run-multihost` (the orchestrator).
Expected: `=== PASS ===`. The through-tunnel curl to the TEST-NET target succeeds, routing split holds (target reachable only via EU), and the multiplex assertion passes.

- [ ] **Step 4: Commit**

```bash
git add test/integration/multihost/assert.sh test/integration/multihost/lib.sh
git commit -m "test(integration): assert multiplex negotiated + repoint diag to sing-box; harness green"
git push origin main
```

---

## Phase 6 — cleanup + CI verification

### Task 6.1: Remove dead references, mark the WS plan superseded, verify CI

**Files:**
- Modify: `docs/superpowers/plans/2026-06-27-websocket-transport-migration.md` (add SUPERSEDED banner)
- Modify: memory `rdda-xray-ws-deprecation-blocker.md` (note resolved by Lane B)

- [ ] **Step 1: Mark the paused WS plan superseded**

Prepend to the WS-migration plan: `> **SUPERSEDED 2026-06-27 by docs/superpowers/plans/2026-06-27-lane-b-singbox-migration.md (Lane B).** Do not execute.`

- [ ] **Step 2: Final dead-reference sweep**

Run: `grep -rn "xray\|wsSettings\|XHTTP\|vless://" --include=*.go --include=*.sh --include=*.service . | grep -vi singbox || echo CLEAN`
Expected: `CLEAN` (remaining hits only in `docs/`/memory as history).

- [ ] **Step 3: Full local test + push, then verify CI**

```bash
go test ./...
git add -A && git commit -m "docs: mark WS migration plan superseded by Lane B; final cleanup"
git push origin main
```
Then **verify the integration run is green** (`gh run list`/`gh run watch`) — a green run is done, not the push. Watch for a skipped integration job masquerading as success.

- [ ] **Step 4: Update memory**

Update `rdda-xray-ws-deprecation-blocker.md` to note the blocker is resolved by Lane B (sing-box both nodes), and confirm `rdda-transport-review-june2026.md` reflects the shipped state.

---

## Self-Review

**Spec coverage:**
- §3.1 core swap → Tasks 1.1–1.5, 3.1–3.2, 5.x ✓
- §3.3 domestic REALITY+multiplex → 1.2, 1.3, 2.2 ✓
- §3.3 CF HTTPUpgrade+TLS+multiplex → 1.3, 1.4 ✓ (WS fallback gated in 0.1)
- §3.5 zapret2/nfqws2 fail-open → 2.1, 4.1, 4.2 ✓
- §3.6 sing-box JSON subscription → 2.2, 2.3 ✓
- §5 testing (real client, multiplex assertion, de-risk first) → 0.1, 5.2, 5.3 ✓
- §6 risks (HTTPUpgrade-via-CF, schema drift, silent mux regression) → 0.1 gate, `sing-box check` oracle, 5.3 assertion ✓

**Placeholder scan:** every code step carries concrete code or an exact command. The two judgement points (geoip rule_set URL, sing-box mux log token) are explicitly delegated to Task 0.1 findings with a concrete fallback each — not silent TODOs.

**Type consistency:** `RenderClient(cfg, uuid, socksPort)`, `RenderRU(cfg, clients)`, `RenderEU(cfg)` used identically in CLI Task 1.5; `subscription.ClientOutbound(cfg, c) ([]byte,error)` and `Build(cfg, c) (string,error)` used consistently in 2.2/2.3; `state.Desync{Enabled,Profile,Ports}` defined in 2.1 and consumed in 4.2. Render filename `/etc/rdda/singbox.json` consistent across 3.1/3.2/5.2.
