# Releasing RDDA (signed releases)

RDDA releases are signed with [minisign](https://jedisct1.github.io/minisign/).
`rdda update` and `install.sh` verify that signature against the maintainer's
public key **embedded in the binary / installer** before installing anything, so
a substituted binary with an attacker-generated checksum is rejected. A digest
proves only "this is what the release is serving"; the signature proves "the
maintainer signed this." This is the F-1 fix from the v0.5 security scope.

## One-time setup

### 1. Generate the signing keypair

```bash
minisign -G -W -p minisign.pub -s minisign.key
```

`-W` makes the secret key **password-less** — required so CI can sign
non-interactively (there is no TTY to type a password into). The key's secrecy
then rests on GitHub's encrypted-secret storage, not a minisign password.

> **Custody.** `minisign.key` is the single most sensitive artifact in the whole
> system — whoever holds it can publish a release every deployed node will accept
> as root. Generate it on a trusted machine, back it up offline, and do **not**
> commit it. It is a single point of coercion (see the v0.5 scope, review #22);
> multi-party signing is a future hedge.

### 2. Embed the public key

`minisign.pub` is two lines: a comment and the base64 key. Put them where the
verifiers read them:

- **`internal/verify/minisign.pub`** — replace the whole placeholder file with
  your `minisign.pub` (both lines). This is embedded in the binary via `go:embed`
  and used by `rdda update`.
- **`install.sh`** — set `MAINTAINER_PUBKEY` to the **base64 line only** (the
  second line of `minisign.pub`, starting `RW...`).

Commit both. Until they hold a real key:

- `rdda update` **fails closed** — it refuses to install rather than skip
  verification.
- `install.sh` falls back to **checksum-only** (today's behavior) with a warning,
  so installs of existing unsigned releases keep working during the transition.

Verify they match before releasing:

```bash
diff <(tail -n1 internal/verify/minisign.pub) <(minisign -P placeholder 2>/dev/null; grep -o 'RW[A-Za-z0-9+/=]*' install.sh | head -n1)
```

(Simplest check: the base64 line in `install.sh` equals the second line of
`internal/verify/minisign.pub`.)

### 3. Store the secret key in CI

Add a GitHub Actions secret named **`MINISIGN_SECRET_KEY`** whose value is the
**full contents of `minisign.key`** (both lines). The release workflow writes it
to a temp file, signs `SHA256SUMS`, and `shred`s it.

## Cutting a release

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

The `release` workflow then:

1. Runs the integration suite (pre-publish gate).
2. Builds `rdda-linux-{amd64,arm64}` and `SHA256SUMS`.
3. **Signs `SHA256SUMS`** with `MINISIGN_SECRET_KEY` → `SHA256SUMS.minisig`, and
   **self-verifies** it against the embedded `internal/verify/minisign.pub`. A
   key/secret mismatch (or a still-placeholder embedded key) **fails the
   release** rather than publishing an unverifiable asset.
4. Publishes the binaries, `SHA256SUMS`, and `SHA256SUMS.minisig`.
5. Runs the post-publish update smoke.

> The **first** signed release must be built from a tree that already has the
> real key embedded (step 2) — otherwise the signing step fails on the
> placeholder, by design.

## Verifying a release by hand

```bash
minisign -Vm SHA256SUMS -P <base64-public-key> -x SHA256SUMS.minisig
sha256sum -c SHA256SUMS
```

To verify `install.sh` itself before piping it to a shell, download it, confirm
`MAINTAINER_PUBKEY` matches the key you trust (published in the README / release
notes), then run it — this is the download-verify-run path (review F-3).

## Rotating the key

1. Generate a new keypair (step 1).
2. Embed the new public key (step 2) and update `MINISIGN_SECRET_KEY` (step 3).
3. Cut a release. **Caveat:** a node running an old binary verifies against the
   *old* embedded key, so it can `rdda update` to the release signed with the key
   its current binary trusts — plan rotations so there is an overlap release, or
   re-install nodes via `install.sh` with the new `MAINTAINER_PUBKEY`.
