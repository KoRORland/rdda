# Lane B — Phase 0 De-risk Findings (2026-06-27)

Executed Task 0.1 of `docs/superpowers/plans/2026-06-27-lane-b-singbox-migration.md`.
These findings are **authoritative** and override any speculative JSON in the plan body.

## Pinned version

```
SINGBOX_VERSION=1.13.14
```

Installed for linux/amd64 from `github.com/SagerNet/sing-box` release `v1.13.14`
(asset `sing-box-1.13.14-linux-amd64.tar.gz`). `sing-box version` confirms 1.13.14,
tags include `with_utls`, `with_quic`, `with_gvisor`. Use this exact version for every
`sing-box check` oracle and in `VERSION` / installer / harness.

## Step 2 — schema validation: PASS (no corrections)

All five rendered shapes pass `sing-box check` on 1.13.14 **as written in the plan**:
client, RU-CF, RU-nonCF, EU-CF, EU-nonCF. The plan's field names are correct:

- inbound REALITY: `tls.reality.{enabled, handshake:{server,server_port}, private_key, short_id:[...]}`
- outbound REALITY: `tls.reality.{enabled, public_key, short_id}` (short_id is a **string** on the outbound, **array** on the inbound)
- uTLS: `tls.utls.{enabled, fingerprint}` — valid fingerprints: firefox, chrome, safari, edge, ios, random
- multiplex (outbound): `{enabled, protocol:"h2mux", max_streams:8}`; (inbound): `{enabled:true}`
- route remote rule_set `{type:"remote", tag, format:"binary", url, download_detour}` accepted offline by `check`

➡ **Phase 1 render code may use the plan's field names verbatim.**

## Step 3 — HTTPUpgrade through Cloudflare: FAIL → use WebSocket (`ws`)

Stood up a real Cloudflare quick tunnel (`cloudflared tunnel --url http://127.0.0.1:9443`,
TryCloudflare, no account) in front of a sing-box VLESS inbound; pointed a sing-box client at
the `*.trycloudflare.com` hostname:443.

- `transport.type=httpupgrade`: **FAIL.** Server logged `real websocket request received`;
  client got `v2ray-http-upgrade: unexpected status: 404 Not Found`. Cloudflare's edge rewrites
  the plain HTTP/1.1 Upgrade into a true WebSocket handshake, which sing-box's `httpupgrade`
  inbound rejects.
- `transport.type=ws`: **PASS.** Full path client→CF→cloudflared→sing-box→internet works;
  `curl` via SOCKS returned `h=www.cloudflare.com`. Multiplex negotiated over it.

➡ **AUTHORITATIVE SUBSTITUTION for the CF hop:** everywhere the plan writes
`transport.type=httpupgrade` for the **Cloudflare-fronted** path (RenderRU CF outbound,
RenderEU CF inbound, and their tests `TestRenderRU_CF` / `TestRenderEU_CF`), use
`transport.type=ws` instead. Client outbound `ws` carries `path` + `headers.Host`; server
inbound `ws` carries `path`. It is otherwise drop-in. The **non-CF REALITY-direct** RU→EU
path is unaffected (no httpupgrade there).

## Step 4 — REALITY + multiplex client→RU with the real sing-box core: PASS

Hiddify embeds the sing-box core, so the client→RU hop was validated against stock
sing-box 1.13.14 using the **exact** `outbounds[]` object a Hiddify subscription carries
(VLESS + REALITY, no flow, h2mux). Result:

- Handshake + auth succeed; 4 parallel SOCKS requests all returned `h=www.cloudflare.com`.
- Multiplex negotiated: server log shows `inbound multiplex connection to ...` and the
  internal marker `sp.mux.sing-box.arpa:444`.
- `firefox` uTLS fingerprint works (plan default); chrome/safari/edge/ios/random also work.

➡ **Mux log token for Task 5.3 assertion:** grep the RU `rdda-singbox` journal for
`inbound multiplex connection` (case-insensitive `mux|multiplex` also matches).

➡ **Hiddify GUI caveat:** the Linux Hiddify AppImage is a Flutter GUI and cannot be driven
headlessly in this environment, so the import-and-click flow itself was not automated. The
protocol is fully de-risked via the identical embedded core; the operator should do a one-time
manual Hiddify import smoke-test before going live.

## CRITICAL extra finding — REALITY handshake dest

`www.microsoft.com` **no longer works** as a REALITY handshake target on 1.13.14: the relayed
TLS handshake never completes (`isHandshakeComplete: false` → `REALITY: processed invalid
connection`), across **all** uTLS fingerprints. Working dests (all PASS):

- `www.apple.com`  ✅ (recommended default)
- `addons.mozilla.org` ✅
- `www.lovelive-anime.jp` ✅
- `gateway.icloud.com` ✅

➡ The `init` default REALITY target and the integration harness must use a working dest
(use `www.apple.com:443`). Unit-test fixtures that only run `sing-box check` (no live
handshake) are unaffected, but should switch to `www.apple.com` for consistency.

## Gate status

Steps 2, 3, 4 all PASS (Step 3 via the WS fallback, now recorded as authoritative).
**Phase 1 is unblocked**, with two mandatory carry-forward corrections:
1. CF hop transport = `ws` (not `httpupgrade`).
2. REALITY dest = `www.apple.com` (not `www.microsoft.com`).
