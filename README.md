<div align="center">

<img src="RDDA.jpg" alt="RDDA mascot — a secret-agent matryoshka opening her coat to reveal the uncensored internet" width="320">

# RDDA — Russian Doll Double Agent

**A 2-hop VPN that looks Russian on the outside and free on the inside.**

*Set it up once. Hand granny a link. Let the DPI keep guessing.*

</div>

---

## What is this?

RDDA is an easy-to-run, highly automated **2-hop anti-censorship VPN** built to slip past
Russia's Roskomnadzor (RKN) — its DPI, active probing, and behavioral analysis. The client
tunnels to a node inside Russia that looks like a perfectly boring HTTPS website, which
tunnels again to a controller node abroad that actually reaches the open internet.

Two dolls. One quietly minds its own business in-country; the other does the real work from
safety. Hence the name.

> **Legitimate use only.** Running a VPN is not illegal in Russia. RDDA exists so a small,
> non-commercial circle — friends, family, the technologically-challenged uncle — can read
> the internet they're already entitled to. Don't be a villain.

## Why it's not *yet another* VPN script

- **Granny-grade client.** Install [Hiddify](https://hiddify.com), paste one link, hit
  Connect. That's the entire user manual.
- **Operator-grade laziness.** Highly automated, self-healing, self-updating. Humans only
  show up to add a friend or put out a fire.
- **Built for the chase.** RKN never stops moving, so neither do we — the obfuscation
  transport is swappable by changing a single config value.
- **Boring on purpose.** Native systemd, plain files, well-known maintained components. No
  Docker maze, no database, no surprises in your logs.
- **Doesn't snitch on itself.** The in-Russia node exposes no management surface and looks
  like a normal web server to anyone poking at it.

> 🟢 **Status: `v0.2.0` released** — the Lane B **single sing-box** data plane
> (VLESS + REALITY + multiplex on the inspected hop, VLESS + WebSocket over
> Cloudflare), verified end-to-end by the multi-host integration harness.
> Still young — expect rough edges. Latest build:
> [**releases/v0.2.0**](https://github.com/KoRORland/rdda/releases/tag/v0.2.0).
> See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for the design.

## How it works (the 30-second version)

```
[You + Hiddify] ──▶ [RU node: looks like a boring website] ══▶ [EU node: the actual exit] ──▶ Internet
```

- The **RU node** greets your client, pretends to be an ordinary HTTPS site, and decides
  where traffic goes (Russian sites stay local; everything else takes the tunnel out).
- The **EU node** is the brains in the safe zone — it hands out client links, keeps the
  state, exits to the real internet, and emails you when something's on fire.

Full design and rationale: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

---

## 🚀 Quickstart — Operators

You need **two Ubuntu 24.04 VPSes** — one in Russia (RU), one abroad (EU).

The installer pulls the latest published release (currently **v0.2.0**, checksum-verified
amd64/arm64 binaries). To pin a specific version instead of tracking latest, add
`--version v0.2.0` to either `bash -s --` line below.

1. **EU node** (SSH is fine here):
   ```bash
   curl -fsSL https://raw.githubusercontent.com/KoRORland/rdda/main/install.sh | sudo bash -s -- eu
   rdda init --ru-host <RU_IP> --eu-host <EU_HOST>
   rdda render eu > /etc/rdda/singbox.json
   chown -R rdda:rdda /etc/rdda && systemctl enable --now rdda-singbox
   ```
2. **RU node** (run from your VPS provider **console** — the installer closes SSH):
   ```bash
   curl -fsSL https://raw.githubusercontent.com/KoRORland/rdda/main/install.sh | sudo bash -s -- ru
   ```
   Then, on EU, `rdda render ru` and copy the output to the RU node's
   `/etc/rdda/singbox.json`; on RU `systemctl enable --now rdda-singbox`.
3. **Add a friend** (on EU) and send them the printed sing-box config privately:
   ```bash
   rdda client add aunt-olga
   ```

See [`deploy/install-eu.md`](deploy/install-eu.md) and [`deploy/install-ru.md`](deploy/install-ru.md) for the detailed walkthrough.

## 📱 Quickstart — Clients (your friends & family)

1. Install **Hiddify** ([hiddify.com](https://hiddify.com)) — Android, Windows, or Linux.
2. Paste the sing-box config the operator sent you into Hiddify as a **new profile/config**.
3. Hit **Connect**. That's it.

---

## Documentation

- [Architecture](docs/ARCHITECTURE.md) — the high-level design.
- `basic design layout.txt` — the original requirements seed.

## License

See [LICENSE](LICENSE).
