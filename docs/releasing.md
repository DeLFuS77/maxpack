<!-- SPDX-License-Identifier: GPL-3.0-or-later -->
# Releasing MaxPack for the PC

What the owner does for each release. The CI builds and drafts; the signature is made by hand, on the
owner's computer, with a key the CI never has.

## Each release

1. **The version.** The `[Unreleased]` part of `CHANGELOG.md` becomes the version's. The packages take
   their version from the tag (`tools/version.sh`): nothing else is bumped by hand.
2. **The tag.** On the commit of `main` that is released:
   `git tag -a vX.Y.Z -m "MaxPack X.Y.Z"`, then `git push origin refs/tags/vX.Y.Z` (never `--tags`).
3. **The draft.** The CI (`.github/workflows/build.yml`, job `release`) builds the seven packages, writes
   their `SHA256SUMS`, attests each file and leaves everything in a **draft** release.
4. **Sign the draft, and publish it.** From the `gui` folder of this repository (the one the tag was made
   in), with `gh` signed in:

   ```
   go run ./tools/releasesign X.Y.Z
   ```

   It does, in this order, and stops at the first thing that does not hold (nothing is signed then):
   - checks that the tag `vX.Y.Z` here and on GitHub point to the same commit;
   - downloads the files of the draft and checks each against `SHA256SUMS`, and that the list and the
     release name the same files;
   - checks with `gh attestation verify` that each was built by `.github/workflows/build.yml` from the
     commit of the tag, on GitHub's runners;
   - asks for the private release key, which is not shown as it is typed or pasted, and is never printed,
     written nor kept;
   - signs `SHA256SUMS` as minisign does (`ED`), with the trusted comment exactly `MaxPack X.Y.Z`, and
     checks the signature with the release keys the window is built with;
   - adds `SHA256SUMS.minisig` to the draft and asks whether to publish it (`-publish` publishes without
     asking; otherwise it stays a draft until you publish it).

   `-published` signs a release that was published unsigned, and `-replace` puts a new signature in place
   of the one a release has. Anyone can check a release with the minisign command and either public key:
   `minisign -Vm SHA256SUMS -P <key>`, which prints `Trusted comment: MaxPack X.Y.Z`.
5. **Before a release needs it:** `go run ./tools/releasesign -check` asks for a private key and says
   whether it is one the window is built with (and which), without signing or sending anything.

What the window does with it (Settings › Internet › **Look for new versions**, off by default): it offers a
release newer than itself only when the release has `SHA256SUMS` and `SHA256SUMS.minisig`, the signature
is valid for one of the release keys built into it (both the signature and its trusted comment), and the
trusted comment is `MaxPack` and the version of the release's tag, so the signed list of an older release is
never offered under a newer tag. On Windows it then downloads the installer and checks its SHA-256 against
that list before it offers **Update**; the installer stays in the settings folder (`updates/`), is used
again at the next start while it is whole, and goes once the window is up to date. A release published
without `SHA256SUMS.minisig`, or signed with another key or without the version, is never offered: users
would update by hand.

## The release keys

- They are the author's release keys, the same that sign the releases of NXPress: their public keys are in
  `gui/internal/update/release.pub`, built into the window, as minisign writes them.
  - **`64300306F44900F2`**, the main key, signs every release.
  - **`7090921881F567B0`**, the spare key, is kept apart from the first. It signs the releases if the first
    is lost; the window takes either.
- The private keys are kept in a password manager, each as its key ID and Ed25519 seed in base64 (the form
  `tools/releasesign` asks for). They never go into the repository, a CI secret, a file or a log.
- **Changing a key:** a window checks with the keys it was built with. Put the new public key in
  `release.pub` and ship that version signed with a key the windows already out know; the releases after it
  can use the new key. If both keys were lost, the windows already out cannot be offered anything newer:
  say so in the notes of the version with new keys, which users install by hand.
