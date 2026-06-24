# Multi-Host Integration Harness — Design

**Status:** approved decisions (2026-06-25); supersedes the single-host `test/integration/run.sh` approach for the v0.2 Cloudflare-fronted path.
**Builds on:** v0.2 Workstream A (CF keystone). Replaces the single-host `/etc/rdda-ru` + nginx + `--dir`-juggling harness.

## Why

The single-host harness ran both nodes on one box with two state dirs, `--dir`
overrides, an nginx stand-in, and jq port surgery. That ceremony hid a real
production bug (the pull unit wrote a file xray never read) and does not exercise
the deployment as operators run it. The real test is **separate hosts running the
exact same operator commands**, with success defined as the RU client reaching the
open internet *through the EU exit*.

## Decisions (locked)

- **Orchestration:** `systemd-nspawn` containers on a single GitHub Actions Linux
  runner. Each container has its **own real systemd** and runs the **real installer
  + units** — preserving the v0.1 "real deployment" property. No Docker. No VMs.
- **Cloudflare in CI:** a **transparent CF-edge stand-in**. CI cannot authenticate
  to Cloudflare, so a local "edge" host plays Cloudflare's network. The EU/RU node
  commands are identical to production; only DNS (`/etc/hosts`) points the CF
  hostname at the stand-in edge, and `cloudflared.service` is swapped for the
  stand-in's reverse-tunnel client.
- **Green criterion:** the **ru-client fetches a real external resource and the exit
  is the EU node** — proven by an internet-target reachable *only* via EU.

## Topology

One runner, root via sudo, a host bridge `br-rdda` (e.g. `10.8.0.0/24`), and these
nspawn containers:

```
[ru-client] → [ru-node] → [cf-edge] → [eu-node] → [internet-target]
 10.8.0.40    10.8.0.30   10.8.0.20   10.8.0.10    10.8.0.50
```

- **eu-node (10.8.0.10):** the controller + exit. Runs the real EU flow: `rdda init`
  (with `--eu-host eu-node`, `--ru-host ru-node`, and `--cf-tunnel-host`/`--cf-sub-host`
  set to the CF stand-in hostnames), `rdda render eu > /etc/rdda/xray.json` (loopback,
  `security:none` under CF), `rdda serve` (loopback sub server). **Zero inbound** — it
  dials **out** to cf-edge via the reverse-tunnel client (the cloudflared stand-in).
  Its `freedom` outbound is the only route to **internet-target**.
- **cf-edge (10.8.0.20):** stands in for Cloudflare. Terminates TLS on the CF
  hostname (`:443`) with a self-signed cert and forwards inbound to eu-node's xray
  via the reverse tunnel eu-node established outbound. Reproduces cloudflared's
  defining property: eu-node has no listening port; reachability is established by
  eu-node dialing out.
- **ru-node (10.8.0.30):** the in-country entry. Real RU flow: installs the real
  `rdda-xray.service`; gets its config via `rdda pull` from the sub server through
  cf-edge (or the documented bootstrap), so its tunnel outbound dials the CF
  hostname (resolved to cf-edge) over XHTTP/TLS.
- **ru-client (10.8.0.40):** the friend's device. `rdda render client --uuid <tester>`
  → xray with a SOCKS inbound → dials ru-node via REALITY. Drives the probe.
- **internet-target (10.8.0.50):** the "open internet." A plain HTTP server serving
  a known body. Routed so it is reachable **only from eu-node** (e.g. only eu-node
  has a route/firewall path to `10.8.0.50`; ru-node and ru-client cannot reach it
  directly). If the client can fetch it, traffic provably exited via EU.

## The exit-via-EU proof (why a bare external curl is insufficient)

All containers egress through the runner's single public IP (NAT), so an IP-echo
service returns the same address regardless of which node exits — a direct RU exit
would look identical to an EU exit. The **internet-target reachable only via EU**
removes that ambiguity: the client obtaining the target's body is positive proof the
path was client → ru-node → cf-edge → eu-node → target. A secondary real-egress curl
(the runner has internet) is kept for realism but is not the load-bearing assertion.

## Reverse-tunnel stand-in for cloudflared

To preserve "EU has zero inbound," eu-node runs a reverse-tunnel **client** that
dials **out** to cf-edge (e.g. `ssh -R` or `chisel`), exposing eu-node's loopback
xray on cf-edge. cf-edge runs the TLS terminator (nginx/stunnel) on `:443` for the
CF hostname → the reverse-tunnel endpoint → eu-node loopback xray. In production this
single unit (`cloudflared.service`) is the only thing that differs; every `rdda`
command on the nodes is identical to what the test runs.

## Same-commands principle

The three real hosts run the documented operator commands verbatim (the
`deploy/install-*.md` flows). The only test-only substitutions are infrastructure the
operator never touches: the nspawn provisioning, the `/etc/hosts` CF-hostname
mapping, the self-signed edge cert, and swapping `cloudflared.service` for the
reverse-tunnel stand-in. No `--dir`, no per-node command divergence.

## Green / Definition of done

1. All node services reach `active` on their respective hosts (real units, real users).
2. eu-node has **no** inbound listener (reachable only via cf-edge).
3. **ru-client fetches internet-target's known body through the tunnel** → exit is EU.
4. `rdda pull` on ru-node lands a newly-added EU client with no manual `render ru`.
5. The harness uses the exact operator commands on each host; differences are
   infrastructure-only.

## Out of scope

Real Cloudflare (needs account/secret; covered by the documented manual
verification). Multi-runner / cloud VMs. IPv6 path.
```
