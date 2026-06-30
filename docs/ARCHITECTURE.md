# RDDA — High-Level Architecture

**RDDA = Russian Doll Double Agent.** An easy-to-run, highly automated 2-hop anti-censorship
VPN designed to circumvent Russia's Roskomnadzor (RKN) DPI / active-probing / behavioral
analysis. **Legitimate use only.** Non-commercial, for a close group (friends, family).

This is the high-level design. It deliberately favors **fewer moving parts, well-known
maintained components, and observability** over cleverness. See `basic design layout.txt`
for the original requirements seed.

> **Implementation status — [`v0.3.0`](https://github.com/KoRORland/rdda/releases/tag/v0.3.0)
> (Lane B + operator QoL).** The DPI-facing core is shipped and integration-gated: 2-hop
> sing-box data plane, client onboarding, sub server, Cloudflare ingress, nfqws2 egress desync,
> and config pull-sync. v0.3 adds the operator tooling: the RU→EU health beat + `rdda status`,
> `rdda doctor`, encrypted `rdda backup`/`restore`, email `rdda alert` (msmtp), and
> `rdda update` (self-update + rollback) + `rdda heal` (auto-restart of failed units).
> Not yet built (designed below, on the roadmap): RU failover and key rotation.
> Sections marked *(planned)* call out the gaps inline.

---

## 1. Platform scope

- **Nodes (RU & EU):** Ubuntu 24.04 LTS VPS (forward-compatible with 26.04 LTS).
- **Clients:** Linux (rpm- and deb-based), Windows, Android.

---

## 2. Topology

```
[Client + Hiddify] ──VLESS / REALITY + multiplex──▶ [RU node: sing-box] ── WebSocket+TLS+mux via CF ──▶ [EU node: sing-box] ──▶ Internet
                                                            │ owns ALL routing
                                                            ▼
                                               RU-zone + intl-allowlist exit locally
                                               (RU egress handshakes hardened by nfqws2 desync)

  subscription pull:  [Hiddify] ── HTTPS ──▶ [Cloudflare] ──▶ [EU node]
  RU config sync:     [RU node] ── HTTPS pull ──▶ [Cloudflare] ──▶ [EU node]   (looks like normal web; no SSH)
  operator control:   local `rdda` CLI on each node                            (no Ansible, no exposed mgmt port)
```

The "Russian Doll": the client tunnels to the **RU node**, which tunnels again to the
**EU node**. **Both nodes run sing-box** — one core, so the Hiddify (sing-box) client and both
nodes share one protocol and one multiplex scheme end to end. Two hops RKN can observe, but only
the first is actually inspected by the June-2026 scheme:

1. **Client → RU node (domestic) — the inspected hop.** Carries **VLESS + REALITY + multiplex**.
   REALITY borrows a famous site's TLS identity (+ a real decoy web server) to defeat the
   *subnet* (on a clean RU subnet) and *fingerprint* signals; **multiplex** defeats the
   *parallel-frequency* signal — and because both ends are sing-box, the multiplex actually
   negotiates. Breaking any one of the three signals suppresses the restriction; REALITY breaks
   two. (Caveat: the *subnet* signal keys on this first hop's destination, so the RU node's own
   subnet matters — prefer a clean/residential RU subnet; see §6.)
2. **RU node → EU node (cross-border) — not inspected.** Carries **VLESS + WebSocket + TLS +
   multiplex** behind **Cloudflare**. The hop terminates at a Cloudflare IP whose subnet is
   de-facto whitelisted, so the three-signal chain is already broken at Signal-1 here — this hop
   only needs to be CDN-frontable and resemble normal web traffic. EU's WebSocket inbound runs
   with **no TLS of its own** — Cloudflare terminates the public TLS.

> **Why this stack (decided 2026-06-27, Lane B):** the earlier `WS + mux + REALITY + Hiddify`
> design was broken three ways — WebSocket self-signatures and is xray-deprecated; its mux was
> **inoperative** across the xray-server/sing-box-client split (`mux.cool` ≠ sing-box multiplex),
> silently leaving Signal-3 unmitigated; and uTLS is disavowed by sing-box's own maintainers.
> XHTTP (the all-three-signal cure) is **xray-only** and unreachable by the Hiddify app. Lane B
> resolves this by putting **sing-box on both nodes** so multiplex works end to end, using
> **REALITY+multiplex** on the inspected domestic hop and a **WebSocket** transport on the
> CF-cleared cross-border hop. (The Lane B spec first chose HTTPUpgrade for that hop; the P0
> de-risk proved HTTPUpgrade does **not** survive Cloudflare — CF rewrites the plain HTTP/1.1
> Upgrade into a true WebSocket the `httpupgrade` inbound rejects — so the shipped transport is
> WebSocket.) uTLS's residual weakness is accepted (clean subnet + REALITY already break the
> chain); **AnyTLS** is the documented future Signal-2 hedge (§6). See
> `docs/superpowers/specs/2026-06-27-lane-b-singbox-design.md`.

**All client traffic goes to the RU node.** Clients do **no** routing — they never learn the
topology and cannot misconfigure or leak it. The RU node owns routing decisions:
RU-zone + an international allowlist exit locally; everything else goes through the tunnel to
the EU node, which is the internet exit.

---

## 3. Components

### Client
- **Hiddify** (Android / Windows / Linux), configured by pasting **one subscription URL**.
- No RDDA-authored client code. "Install Hiddify, paste link, Connect" — that is the whole
  client experience.

### RU node — entry + router (native systemd services)
| Unit | Role |
|---|---|
| `rdda-singbox.service` | sing-box: terminates client VLESS (REALITY + multiplex); originates the WebSocket tunnel to EU via Cloudflare; **owns routing** in its `route` block (RU-zone via a local geoip rule-set + intl-allowlist → direct, default → EU tunnel) |
| `rdda-nfqws.service` | nfqws2 (zapret2): DPI-desync of the RU node's **outbound** 443 handshakes (→EU, →direct allowlist) via an nft egress hook — survives local RU-ISP DPI |
| `rdda-pull.timer` | periodically pulls desired config (incl. new clients) from EU over Cloudflare |
| `rdda-health.timer` | posts a periodic health beat (units, versions) to EU at a randomized interval; EU stamps `received_at` and judges staleness |
| `rdda-heal.timer` | restarts any RDDA unit stuck in systemd `failed` state (start-limit recovery); never touches a running unit |

> **Implemented vs designed:** routing and the REALITY camouflage are **folded into sing-box**, not
> separate units. There is no `rdda-router.service` — routing lives in the sing-box `route` block
> above (split-routing data is a **local** geoip-ru `.srs`, shipped at install time, so the data
> plane never blocks startup on a remote download). There is no `rdda-decoy.service` — REALITY
> borrows a **real external site** as its handshake `dest` (e.g. `www.apple.com`); unauthenticated
> probes are transparently proxied to that real site, which *is* the active-probing camouflage, so
> no local decoy web server is needed.

Client-exposing data (UUIDs, nicknames) lives in the running config only; nothing extra is
persisted beyond what sing-box needs to serve clients. The nfqws2 desync layer is **fail-open**:
a desync failure must not break the tunnel path.

### EU node — controller + exit (native systemd services, behind Cloudflare)
| Unit | Role |
|---|---|
| `rdda-singbox.service` | sing-box: terminates the RU→EU WebSocket tunnel (TLS terminated by Cloudflare, so the inbound runs no TLS); exits to the internet |
| `rdda-sub.service` | subscription endpoint: serves per-client **sing-box JSON** configs (REALITY + multiplex; behind Cloudflare) |
| `cloudflared.service` | Cloudflare Tunnel ingress (config rendered by `rdda render cloudflared`) — fronts the sub + tunnel endpoints with no inbound port exposed |
| `rdda-alert.timer` | evaluates conditions (RU beat stale, an EU unit down, cert near expiry) and emails the operator via `msmtp` on transitions — fires once when it breaks, once on recovery |
| `rdda-heal.timer` | restarts any RDDA unit stuck in systemd `failed` state; never touches a running unit |
| `rdda-update.timer` *(opt-in, off by default)* | runs `rdda update` (checksum-verified self-update + auto-rollback) on a staggered schedule |

EU node is the **single source of truth**.

---

## 4. Management — local `rdda` CLI, one goal per command

No Ansible. No inbound management port on the RU node. Routine changes propagate by the
**RU node pulling** desired state from the EU node over Cloudflare (HTTPS, indistinguishable
from normal web traffic). The operator interacts through a small local CLI on each node.

**EU node (the box you operate):**
```
rdda client add <name>     # create a client, print the subscription URL to hand out
rdda client rm <name>      # revoke a client
rdda client list           # list clients
rdda status                # EU + RU health at a glance
rdda doctor                # active checks (services, REALITY dest, Cloudflare, tunnel)
rdda alert --test          # verify email alerting (msmtp) is wired
rdda update                # checksum-verified self-update of the rdda binary (auto-rollback)
rdda backup                # write encrypted state to a single archive file
```

**RU node (mostly hands-off):**
```
rdda doctor                # active local checks (incl. a real fetch through the tunnel)
rdda heal                  # restart units stuck in systemd failed state
rdda update                # checksum-verified self-update of the rdda binary (auto-rollback)
```
New clients reach the RU node automatically via `rdda-pull.timer`; the operator does not
touch the RU node for routine onboarding.

Commands are short, single-purpose, and print clear human-readable output.

---

## 5. State — plain files on the EU node

```
clients/            # one file per client (name, UUID, metadata)
config.yaml         # node + transport settings + desync: (nfqws2) block
VERSION             # pinned component versions (sing-box, nfqws2, etc.)
```
No database. `rdda backup` is a tar of this directory. The RU node's pulled config is derived
from these files.

---

## 6. Swappable transport — a config value, not a framework

The transport/obfuscation engine is selected by a **profile** in `config.yaml` plus a sing-box
config template. Censorship is a never-ending chase, so the engine must be replaceable without
rearchitecting. **One core (sing-box) on both nodes** is a deliberate constraint: it keeps the
protocol and multiplex scheme identical end to end, which is exactly what the previous mixed-core
design got wrong.

- **Default (Lane B):** domestic hop = `VLESS + REALITY (no flow) + multiplex` with a non-Chrome
  uTLS preset; cross-border hop = `VLESS + WebSocket + TLS + multiplex` behind Cloudflare.
  Against the June-2026 passive DPI scheme: **subnet** broken on the domestic hop by a clean RU
  subnet + REALITY's borrowed identity (and structurally by Cloudflare on the cross-border hop),
  **fingerprint** by the non-Chrome uTLS preset + REALITY, **frequency** by multiplex (now
  functional, since both ends are sing-box). Breaking any one signal suffices; REALITY breaks two.
  Pick a **non-Chrome** uTLS fingerprint (Firefox default / Safari / Edge / iOS) — mimicking
  Chrome is now itself a flag.
- **uTLS caveat:** sing-box's maintainers document uTLS as imperfect for fingerprint resistance.
  Accepted residual risk here (clean subnet + REALITY already break the chain). **AnyTLS**
  (sing-box's native anti-detection protocol with its own multiplex) is the planned drop-in
  Signal-2 hedge.
- **Future profiles (drop-in):** `AnyTLS` (domestic-hop Signal-2 hedge), Vision+REALITY direct
  (faster, for un-flagged subnets), AmneziaWG, NaiveProxy. (The CF-hop transport is **WebSocket**:
  HTTPUpgrade-through-Cloudflare was tried in P0 de-risk and rejected — CF rewrites it to a true
  WebSocket — so WebSocket is the shipped default, not a fallback.)
- **Egress hardening (not a transport):** `zapret2`/`nfqws2` on the RU node desyncs RDDA's own
  outbound handshakes against local RU-ISP DPI (§3, `rdda-nfqws.service`). It is complementary,
  fail-open, and never a client. (zapret v1/`nfqws` is EOL; zapret2 is the maintained successor.)

Switching engines = change one profile value + its template; no other code changes.

---

## 7. Self-healing, alerting, safety

- **Self-healing:** systemd `Restart=on-failure` on every service + `rdda-heal.timer`,
  which recovers units stuck in systemd `failed` state once they exhaust their start-limit
  (the gap `Restart=on-failure` leaves). No orchestrator needed.
- **Alerting:** **email** via a minimal SMTP relay (`msmtp`) — lowest overhead, nothing to
  host. `rdda-alert.timer` fires on transitions (RU node down, an EU unit down, cert near
  expiry), once when it breaks and once on recovery. **Telegram** is a planned v2 add-on
  behind the same alert interface.
- **Fail-closed:** if the tunnel drops, the client does not leak — it shows disconnected.
- **Failover** *(planned)*: a subscription may list a backup RU entry; clients fail over
  automatically.
- **Key rotation** *(planned)*: REALITY keys / certs rotated by a future command; cert
  expiry already surfaces as an alert today.

---

## 8. Testing & delivery (TDD-first)

- **Unit tests:** sing-box config templating (per transport branch), routing-rule generation,
  sing-box-JSON subscription generation.
- **Integration (every minor release, GitHub Actions):** a containerized multi-host harness —
  **EU node ⇄ RU node ⇄ real sing-box client** — behind a **Cloudflare stand-in** (nginx
  WebSocket + a chisel reverse tunnel; the real CF edge is exercised in the P0 de-risk, not in
  CI). Asserts the tunnel comes up, **multiplex actually negotiates** (guards against silent
  Signal-3 regression), the routing split is correct (EU is the only exit), and config pull-sync
  delivers a new client. The client core is the **real production core** (sing-box), never a
  stand-in, or transport/multiplex divergence would pass silently. It runs as the pre-publish gate
  on every `v*` release tag.
- **De-risk first:** before the migration, a focused smoke against a **real Cloudflare tunnel**
  established that HTTPUpgrade does **not** survive Cloudflare (→ WebSocket) and that a real
  sing-box core negotiates REALITY+multiplex end to end.
- **Optional DPI smoke:** run `dpi-checkers` against a staged node.
- GitHub-hosted, CI on GitHub Actions.

---

## 9. Deliberately NOT building (YAGNI)

- No custom client app — use Hiddify.
- No custom tunnel protocol — use sing-box (one core on both nodes).
- No Ansible / no always-on control API — local CLI + pull-based sync.
- No Docker — native systemd for observability.
- No database — plain files.
- No billing / multi-tenant dashboard.
