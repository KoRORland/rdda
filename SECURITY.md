# Security Policy

RDDA is anti-censorship software. Its users may face real consequences, so this
document states — explicitly — what RDDA defends against, what it does **not**,
how releases are trusted, what is logged, and how to report a vulnerability. If
anything here is out of date relative to the code, the code wins; please report
the discrepancy.

## Threat model

**Adversary.** A nation-state censor (modeled on Russia's Roskomnadzor) that:

- controls network paths in-country (DPI, SNI/IP filtering, active probing,
  behavioral/timing analysis),
- can pressure or compromise infrastructure and service providers,
- may seize a client device or an in-country (RU) node.

**Design response** (see `docs/ARCHITECTURE.md`): a two-hop tunnel where the
in-country node is deliberately dumb and management-surface-free, the exit node
abroad is the source of truth, and a client only ever learns the RU entry — never
the exit.

### What RDDA aims to defend against

- **Passive DPI classification** of the inspected client→RU hop (VLESS + REALITY +
  multiplex breaks the subnet / fingerprint / frequency signals).
- **Active probing** of the RU node — unauthenticated probes are transparently
  proxied to a real borrowed site (REALITY), so the node looks like an ordinary
  HTTPS server and exposes no management surface.
- **Exit-identity exposure from a seized client.** A client config references only
  the RU entry node; a captured phone or QR reveals nothing about the EU exit.
- **Distribution-channel compromise.** Releases are minisign-signed and verified
  before install/update (see below), so a substituted binary is rejected.
- **Fail-closed behavior.** A dropped tunnel disconnects rather than leaking; a
  failed config pull keeps the last-good config; a failed/unsigned update is
  refused, not installed.

### What RDDA does **not** defend against (residual risk)

- **A compromised EU (exit) node.** It is the source of truth and the internet
  exit; if it is owned, traffic egressing there is exposed. Run it somewhere you
  trust.
- **A single maintainer signing key.** Releases are signed by one key — a single
  point of coercion. Multi-party signing is a planned hedge (roadmap #22).
- **Single fronting provider.** The cross-border hop depends on Cloudflare; if CF
  is coerced or blocked in-country, that hop is affected. CDN diversity is on the
  roadmap.
- **Global traffic-correlation / timing analysis** by an adversary observing both
  ends. Padding/pacing defenses are not yet implemented.
- **uTLS fingerprint perfection.** sing-box's maintainers document uTLS as
  imperfect; the design accepts this (clean subnet + REALITY already break the
  chain) and tracks AnyTLS as the future hedge.
- **Endpoint security** on client or operator machines (malware, coercion of the
  operator, a rooted phone).
- **On-device Hiddify enforcement** of the shipped DNS/route hardening — still
  pending a real-client validation (see the v0.4 scope register).

## Release trust & verification

- Releases are signed with [minisign](https://jedisct1.github.io/minisign/). The
  maintainer's public key is:

  ```
  RWRh5VJfv+qkR3I5cQu8ODzMGTEsGZAdKcvOA2hbvQWnw1iYxKbIeuwa
  ```

- `rdda update` verifies a signature over `SHA256SUMS` against this key (embedded
  in the binary) **before** swapping any file, and refuses an unsigned or
  wrong-key release. Automatic updates are **opt-in / off by default**.
- `install.sh` verifies the same signature on every binary it downloads. Verify
  the installer itself before running it (download → `minisign -V` → read → run);
  see the README quickstart. `install.sh` and its `.minisig` are published as
  release assets.
- See `docs/RELEASING.md` for key custody and rotation.

## Logging & data retention

RDDA is built so that "logs that could deanonymize a user" are absent by
construction, not merely disabled:

- **sing-box on both nodes logs at `warn`** — above the `info` level at which
  per-connection logging occurs. No connection, source-IP, or destination logs
  are produced on the data path.
- **The RU node keeps no client-identifying state** beyond what sing-box needs in
  memory to serve clients; it exposes no management surface and pulls its config
  from EU.
- **The EU node** holds the client records (name, UUID, subscription token) it
  needs to issue configs, plus the RU health beat (unit/version state — no user
  data). These are operator state, `0600`, on disk only.
- **The control-channel token travels in an `Authorization` header, not the URL**,
  so it does not leak into access logs or `Referer`.
- Application logs are operational only (service errors to journald) and do not
  include client tokens or names.

If you find a code path that logs user-identifying or connection data, treat it as
a vulnerability and report it.

## Reporting a vulnerability

**Please report privately — do not open a public issue for a security bug.**

Use GitHub's private vulnerability reporting: the **Security → Report a
vulnerability** button on the repository (GitHub Security Advisories). Include a
description, affected version/commit, reproduction steps, and impact.

Because RDDA's users may be at risk, please allow time for a fix and a signed
release before any public disclosure; coordinated disclosure is appreciated.

## Supported versions

RDDA is pre-1.0 and ships as a rolling series of point releases. Only the
**latest published release** receives security fixes; upgrade to it (verifying the
signature) rather than pinning an old version.
