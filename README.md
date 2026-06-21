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

> ⚠️ **Status: pre-release. This is MVP `v0.1` — actively under construction.**
> Things will move, break, and occasionally work brilliantly. See
> [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for the design.

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

> _Coming soon. The whole point is that this part is short. Reserved for the
> "spin up both nodes in a few commands" walkthrough._

<!-- TODO(v0.1): operator quickstart
  1. Provision EU node (Ubuntu 24.04 VPS, behind Cloudflare)
  2. Provision RU node (Ubuntu 24.04 VPS)
  3. rdda client add <name>  → hand out the link
-->

## 📱 Quickstart — Clients (your friends & family)

> _Coming soon. Reserved for the "install Hiddify, paste link, Connect" three-liner._

<!-- TODO(v0.1): client quickstart — install Hiddify, paste subscription URL, Connect -->

---

## Documentation

- [Architecture](docs/ARCHITECTURE.md) — the high-level design.
- `basic design layout.txt` — the original requirements seed.

## License

See [LICENSE](LICENSE).
