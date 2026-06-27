# RDDA — High-Level Architecture

**RDDA = Russian Doll Double Agent.** An easy-to-run, highly automated 2-hop anti-censorship
VPN designed to circumvent Russia's Roskomnadzor (RKN) DPI / active-probing / behavioral
analysis. **Legitimate use only.** Non-commercial, for a close group (friends, family).

This is the high-level design. It deliberately favors **fewer moving parts, well-known
maintained components, and observability** over cleverness. See `basic design layout.txt`
for the original requirements seed.

---

## 1. Platform scope

- **Nodes (RU & EU):** Ubuntu 24.04 LTS VPS (forward-compatible with 26.04 LTS).
- **Clients:** Linux (rpm- and deb-based), Windows, Android.

---

## 2. Topology

```
[Client + Hiddify] ──VLESS / REALITY+WS+mux──▶ [RU node] ══ CDN-fronted WS tunnel ══▶ [EU node] ──▶ Internet
                                                       │ owns ALL routing
                                                       ▼
                                          RU-zone + intl-allowlist exit locally

  subscription pull:  [Hiddify] ── HTTPS ──▶ [Cloudflare] ──▶ [EU node]
  RU config sync:     [RU node] ── HTTPS pull ──▶ [Cloudflare] ──▶ [EU node]   (looks like normal web; no SSH)
  operator control:   local `rdda` CLI on each node                            (no Ansible, no exposed mgmt port)
```

The "Russian Doll": the client tunnels to the **RU node**, which tunnels again to the
**EU node**. Two hops RKN can observe:

1. **Client → RU node (domestic).** Carries **VLESS + WebSocket + mux + REALITY** so a real
   **Hiddify (sing-box)** client can speak it. REALITY borrows a famous site's TLS identity
   (+ a real decoy web server) for active-probing camouflage and the JA4/fingerprint signal;
   **mux** smooths the connection-frequency signal. (Caveat: the June-2026 scheme keys the
   *subnet* signal on the **first hop's** destination, so the RU node's own subnet still
   matters here — prefer a clean/residential RU subnet; see §6.)
2. **RU node → EU node (cross-border).** Carries **VLESS + WebSocket + mux** behind
   **Cloudflare** (REALITY can't sit behind a CDN, so this hop is WS over CF-terminated TLS).
   CDN-fronting removes the "suspicious subnet" signal on the cross-border hop; **mux** removes
   the "connection frequency" signal.

> **Why WebSocket, not XHTTP (changed 2026-06-27):** XHTTP is xray-only — **sing-box (Hiddify's
> core) does not support it**, so an XHTTP server is unreachable by real Hiddify clients. The
> June-2026 dpi-tls analysis names the Signal-1 fix as "VLESS over **XHTTP / WebSocket** behind
> Cloudflare" (WS is an equal CDN transport) and Signal-3 as **mux** (XMUX is just xray's mux).
> So **WS + mux + CDN** satisfies the same three-signal model while keeping the granny-grade
> Hiddify client. XHTTP+xray-client remains a swappable future profile (§6).

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
| `rdda-xray.service` | xray-core: terminates client VLESS (REALITY + WebSocket + mux); originates the WS tunnel to EU |
| `rdda-decoy.service` | nginx serving a plausible site — REALITY `dest` + active-probing camouflage |
| `rdda-router.service` | applies nft/ipset rules so RU-zone + intl-allowlist egress locally, default → EU tunnel |
| `rdda-pull.timer` | periodically pulls desired config (incl. new clients) from EU over Cloudflare |
| `rdda-health.timer` | local health check; restarts unhealthy units, reports status |

Client-exposing data (UUIDs, nicknames) lives in the running config only; nothing extra is
persisted beyond what xray needs to serve clients.

### EU node — controller + exit (native systemd services, behind Cloudflare)
| Unit | Role |
|---|---|
| `rdda-xray.service` | xray-core: terminates the RU→EU tunnel; exits to the internet |
| `rdda-sub.service` | subscription endpoint: serves per-client Hiddify configs (behind Cloudflare) |
| `rdda-health.timer` | aggregates EU + RU health; triggers operator alerts |

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
rdda update                # fetch pinned versions, restart affected services
rdda backup                # write state to a single archive file
```

**RU node (mostly hands-off):**
```
rdda status                # local health
rdda heal                  # restart unhealthy units
rdda update                # self-update to pinned versions
```
New clients reach the RU node automatically via `rdda-pull.timer`; the operator does not
touch the RU node for routine onboarding.

Commands are short, single-purpose, and print clear human-readable output.

---

## 5. State — plain files on the EU node

```
clients/            # one file per client (name, UUID, metadata)
config.yaml         # node + transport settings
VERSION             # pinned component versions (xray, etc.)
```
No database. `rdda backup` is a tar of this directory. The RU node's pulled config is derived
from these files.

---

## 6. Swappable transport — a config value, not a framework

The transport/obfuscation engine is selected by a **profile** in `config.yaml` plus an xray
config template. Censorship is a never-ending chase, so the engine must be replaceable
without rearchitecting.

- **Default:** `VLESS + WebSocket + mux (+ REALITY client-side / CDN-TLS cross-border)` —
  defeats all three signals of the June-2026 passive DPI scheme: **subnet** via CDN-fronting
  (the structural fix; REALITY never helped here), **frequency** via mux, **fingerprint** via a
  non-Chrome uTLS profile + REALITY. Chosen over XHTTP because **sing-box/Hiddify cannot speak
  XHTTP**; the dpi-tls-june-2026 analysis lists WebSocket-behind-Cloudflare as an equal Signal-1
  fix and mux (not XMUX specifically) as the Signal-3 fix. Pick a **non-Chrome** uTLS
  fingerprint (Firefox/Safari/Edge/iOS/OkHttp) — mimicking Chrome is now itself a flag.
- **Future profiles (drop-in):** `XHTTP + XMUX + REALITY` with an **xray-core client**
  (strongest/newest, but not Hiddify-compatible), Vision+REALITY direct (faster, for un-flagged
  subnets), AmneziaWG, NaiveProxy, and `zapret2`/`nfqws2` (Lua-based desync) as an
  egress-handshake helper. (zapret v1/`nfqws` is EOL; zapret2 is the maintained successor.)

Switching engines = change one profile value + its template; no other code changes.

---

## 7. Self-healing, alerting, safety

- **Self-healing:** systemd `Restart=on-failure` on every service + `rdda-health.timer`
  (detect unhealthy → restart → report). No orchestrator needed.
- **Alerting (v1):** **email** via a minimal SMTP relay (e.g. `msmtp`) — lowest overhead,
  nothing to host. Alerts on node-down and cert/key expiry. **Telegram** alerting is a
  planned v2 add-on behind the same alert interface.
- **Failover:** a subscription may list a backup RU entry; clients fail over automatically.
- **Fail-closed:** if the tunnel drops, the client does not leak — it shows disconnected.
- **Key rotation:** REALITY keys / certs rotated via `rdda update`; expiry surfaces as an alert.

---

## 8. Testing & delivery (TDD-first)

- **Unit tests:** config templating, routing-rule generation, subscription generation.
- **Integration (every minor release, GitHub Actions):** containerized
  **EU node ⇄ RU node ⇄ simple Linux xray client** — assert tunnel comes up, routing split is
  correct, and the generated subscription is valid.
- **Optional DPI smoke:** run `dpi-checkers` against a staged node.
- GitHub-hosted, CI on GitHub Actions.

---

## 9. Deliberately NOT building (YAGNI)

- No custom client app — use Hiddify.
- No custom tunnel protocol — use xray-core.
- No Ansible / no always-on control API — local CLI + pull-based sync.
- No Docker — native systemd for observability.
- No database — plain files.
- No billing / multi-tenant dashboard.
