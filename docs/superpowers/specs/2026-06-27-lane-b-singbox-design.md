# Lane B — sing-box Data Plane (design spec)

**Date:** 2026-06-27
**Status:** approved (supersedes the WebSocket-transport migration plan and the XHTTP→WS pivot)
**Context:** the June-2026 transport review found the current `WS + mux + REALITY + Hiddify` stack
broken three independent ways: WebSocket self-signatures (xray-deprecated), the mux is
inoperative across the xray-server/sing-box-client split (`mux.cool` ≠ sing-box
`smux/yamux/h2mux`, so Signal-3 is silently unmitigated), and uTLS is disavowed by sing-box's
own maintainers. This spec re-platforms the data plane onto a single core (sing-box) so the
Hiddify client and both nodes speak one protocol and one multiplex scheme end to end.

See `docs/ARCHITECTURE.md`, and the review captured in memory `rdda-transport-review-june2026`.

---

## 1. Goal & non-goals

**Goal.** A coherent Lane B data plane: sing-box on both nodes, the granny-grade Hiddify
(sing-box) client unchanged in spirit ("install, paste link, connect"), transports chosen so the
June-2026 DPI scheme is defeated on the only hop it inspects, and zapret2/nfqws2 on the RU node
keeping the tunnel's own egress handshakes alive against local RU-ISP DPI.

**Non-goals.** No xray-core anywhere. No XHTTP (sing-box can't speak it). No AnyTLS in this
iteration (documented as the future Signal-2 hedge, not built now). No client-side routing. No
Docker/Ansible/DB — the existing operational model (local CLI + pull sync + native systemd) is
retained unchanged.

---

## 2. Key reframe — which hop the DPI actually inspects

The June-2026 scheme (Habr 1044396) evaluates the **ClientHello of TLS connections originating
from inside RU** against an AND-chain of three signals (subnet / browser fingerprint / parallel
frequency to one SNI). Breaking **any one** link currently suppresses the restriction.

- **Client→RU (domestic)** is the **only** hop the scheme inspects. REALITY does its real work
  here.
- **RU→EU (cross-border)** terminates at a **Cloudflare** IP, whose subnet is de-facto
  whitelisted → **Signal-1 is already cleared**, so the chain cannot fire on this hop regardless
  of transport. This hop only needs to be CF-frontable and resemble ordinary web traffic.

This reframe is load-bearing: it justifies a heavyweight REALITY+multiplex treatment on the
domestic hop and a light, CDN-friendly transport on the cross-border hop.

---

## 3. Architecture

### 3.1 Core swap

| Before | After |
|---|---|
| `internal/xrayconf` renders xray JSON | `internal/singboxconf` renders sing-box JSON |
| `rdda-xray.service` (RU + EU) | `rdda-singbox.service` (RU + EU) |
| subscription = bare `vless://` link | subscription = **sing-box JSON outbound** |
| (no desync) | `rdda-nfqws.service` (zapret2/nfqws2) on RU node |

Client remains **Hiddify** (sing-box core), all platforms, one subscription URL.

### 3.2 Topology

```
[Client + Hiddify] ──VLESS / REALITY + multiplex──▶ [RU node: sing-box]
                                                        │ owns ALL routing
                                                        │  RU-zone + intl-allowlist → exit local
                                                        ▼
                                  VLESS / HTTPUpgrade + TLS + multiplex
                                                        ▼
                                       [Cloudflare] ──▶ [EU node: sing-box] ──▶ Internet

  RU egress handshakes (→EU, →direct) hardened by nfqws2 desync against local RU-ISP DPI.
  subscription pull / RU config sync / operator CLI: unchanged (HTTPS pull via CF, local CLI).
```

### 3.3 Transports per hop

| Hop | sing-box config | Rationale |
|---|---|---|
| **Client→RU** (domestic) | VLESS inbound/outbound, `tls.reality` (server borrows famous-site identity), **no `flow`** (plain VLESS, not Vision), `tls.utls` non-Chrome preset, **`multiplex`** enabled | The inspected hop. REALITY breaks S1 (on a clean RU subnet) + S2; multiplex breaks S3 — and because both ends are sing-box, multiplex actually negotiates. `flow` (Vision) is omitted deliberately: Vision is incompatible with multiplex, and we chose multiplex to cover S3. |
| **RU→EU** (cross-border) | VLESS outbound (RU) / inbound (EU), transport `httpupgrade`, TLS to the CF hostname, `multiplex` | CF clears S1; HTTPUpgrade is light and CDN-oriented. EU inbound runs `security: none` because Cloudflare terminates the public TLS. |

**uTLS residual risk (S2).** sing-box documents uTLS as imperfect ("use NaiveProxy instead").
We accept this: REALITY+uTLS still raises the bar, and on a clean RU subnet S1 alone already
breaks the chain. AnyTLS is recorded as the future drop-in hedge (§7).

### 3.4 Signal mapping (domestic hop)

- **S1 subnet** → clean/residential RU subnet + REALITY-borrowed SNI (operator guidance, not code).
- **S2 fingerprint** → non-Chrome uTLS preset (default `firefox`); residual risk accepted.
- **S3 frequency** → sing-box multiplex, functional end-to-end (both ends sing-box).

### 3.5 zapret2 / nfqws2 on the RU node

- New unit **`rdda-nfqws.service`** runs nfqws2 (zapret2, the maintained successor; v1 `nfqws`
  is EOL).
- An **nft egress hook** queues the RU node's outbound 443 handshakes (to Cloudflare for the
  tunnel, and to intl-allowlist destinations for local exit) into nfqws2 for DPI desync.
- Desync profile is **config-driven** (a `desync:` block in `config.yaml`), so the fooling
  strategy can be tuned without code changes.
- Scope: **egress handshake survival only** — nfqws2 is not a transport and not a client; it
  keeps RDDA's own outbound connections from being throttled by local RU-ISP DPI.

### 3.6 Subscription = sing-box JSON

A bare `vless://` link **cannot carry multiplex settings**, so the S3 fix would not
deterministically reach Hiddify. The subscription endpoint therefore serves a **sing-box JSON
outbound** (Hiddify accepts sing-box-format subscriptions) carrying: VLESS + REALITY params
(`public_key`/`short_id`/`server_name`), uTLS fingerprint, and the `multiplex` block. The
`rdda client add` flow and `rdda-sub.service` emit this format instead of a `vless://` string.

---

## 4. Components changed

| Component | Change |
|---|---|
| `internal/xrayconf/render.go` (+ tests) | Replaced by `internal/singboxconf` — `RenderRU`, `RenderEU`, `RenderClient` emit sing-box JSON for the three transports above. |
| `internal/subscription` | Emits sing-box JSON outbound (REALITY + multiplex), not a `vless://` link. |
| `internal/state/config.go` | Transport fields re-expressed for sing-box; add `desync:` (nfqws2) block; keep `client_reality` / `tunnel_*` keys (REALITY still used on domestic hop). |
| `internal/cli` | `--fingerprint` retained (non-Chrome default); flags that named xray concepts renamed/dropped. |
| systemd units (both nodes) | `rdda-xray.service` → `rdda-singbox.service`; add `rdda-nfqws.service` + nft egress hook on RU. `rdda-decoy.service` (nginx REALITY dest) unchanged. |
| Integration harness | Real sing-box client (already); add assertions: multiplex negotiated, REALITY handshake, HTTPUpgrade-through-CF tunnel up, routing split correct. |

---

## 5. Testing

- **Unit:** `singboxconf` render tests (one per transport branch, CF and non-CF); subscription
  emits valid sing-box JSON with REALITY + multiplex; config parsing incl. `desync:`.
- **Integration (CI):** real Hiddify/sing-box client ⇄ RU sing-box ⇄ (Cloudflare) ⇄ EU sing-box.
  Assert: tunnel up, **multiplex actually negotiated** (guard against silent S3 regression),
  REALITY handshake succeeds, routing split (RU/allowlist local vs default→EU), subscription
  consumable by sing-box. nfqws2 unit starts and the nft hook is installed.
- **De-risk first (P0):** a focused smoke proving HTTPUpgrade survives Cloudflare and that a
  real Hiddify negotiates REALITY+multiplex to a sing-box inbound — **before** the mass rewrite.

---

## 6. Risks & mitigations

| Risk | Mitigation |
|---|---|
| **HTTPUpgrade through Cloudflare unproven** | P0 de-risk smoke before rewrite; fallback = WebSocket (drop-in transport swap on the CF hop only). |
| uTLS detectable (S2) | Accept residual; rely on S1 break (clean subnet) + REALITY; AnyTLS hedge documented. |
| sing-box option-name drift vs this spec | TDD: render tests assert exact emitted JSON; verify against installed sing-box version during P1. |
| Silent multiplex regression (the original bug class) | Explicit integration assertion that multiplex negotiated. |
| zapret2 coupled into core phase raises CI-green risk | nft hook + unit are additive; gate so a desync failure doesn't break the tunnel path (fail-open on the desync layer). |

---

## 7. Future (out of scope here)

- **AnyTLS** as an alternate domestic-hop profile (Signal-2 hedge; native anti-detection + mux).
- Multi-RU failover, ASN-based blocklists, Telegram alerts, DPI smoke via `dpi-checkers`
  (existing v0.3+ backlog).

---

## 8. Decisions locked (this spec)

1. Full sing-box swap on **both** nodes; no xray anywhere.
2. Domestic hop: VLESS + REALITY (no flow) + **multiplex**, non-Chrome uTLS.
3. CF hop: VLESS + **HTTPUpgrade** + TLS + multiplex (fallback WebSocket).
4. zapret2/nfqws2 built **in the core migration**, egress-handshake scope, config-driven, fail-open.
5. Subscription becomes **sing-box JSON** (carries multiplex); `vless://` link retired.
6. P0 de-risk (HTTPUpgrade-via-CF + REALITY/multiplex with real Hiddify) precedes the rewrite.
