<!-- SPDX-License-Identifier: GPL-3.0-or-later -->
# Releasing MaxPack for the PC

What the owner does for each release, and once for the key that signs them. The CI builds and drafts; the
signature is made by hand, offline, with a key the CI never has.

## Each release

1. **The version.** The `[Unreleased]` part of `CHANGELOG.md` becomes the version's. The packages take
   their version from the tag (`tools/version.sh`): nothing else is bumped by hand.
2. **The tag.** On the commit of `main` that is released:
   `git tag -a vX.Y.Z -m "MaxPack X.Y.Z"`, then `git push origin refs/tags/vX.Y.Z` (never `--tags`).
3. **The draft.** The CI (`.github/workflows/build.yml`, job `release`) builds the seven packages, writes
   their `SHA256SUMS`, attests each file and leaves everything in a **draft** release.
4. **Check the draft.** Download `SHA256SUMS` and the packages, then:
   - `sha256sum -c SHA256SUMS`;
   - `gh attestation verify <file> -R DeLFuS77/maxpack` for each file.
5. **Sign `SHA256SUMS`, offline.** On the computer that holds the secret key (not the CI, and not a
   machine that builds), in the folder of the `SHA256SUMS` just checked:

   ```
   minisign -Sm SHA256SUMS
   ```

   It asks for the key's password and writes `SHA256SUMS.minisig`. Check it with the public key:
   `minisign -Vm SHA256SUMS -P <the public key>`.
6. **Upload the signature to the draft**, before publishing it:
   `gh release upload vX.Y.Z SHA256SUMS.minisig -R DeLFuS77/maxpack`. Do not upload a `SHA256SUMS` of your
   own: the signed one must be the one in the draft, byte for byte.
7. **Publish.** `gh release edit vX.Y.Z -R DeLFuS77/maxpack --notes-file <notes> --draft=false --latest`.

What the window does with it (Settings › Internet › **Look for new versions**, off by default): it offers a
release newer than itself only when the release has `SHA256SUMS` and `SHA256SUMS.minisig` and the signature
is valid for the key built into it (both the signature and its trusted comment). On Windows it then
downloads the installer and checks its SHA-256 against that list before it offers **Update**. A release
published without `SHA256SUMS.minisig`, or signed with another key, is never offered: users would update by
hand. Signing a release after it was published works too (the window looks once each time it starts), but
the signature should be there before it is.

## Once: the key

1. **Make it** on the offline computer: `minisign -G -p maxpack.pub -s maxpack.key`, with a password. Keep
   `maxpack.key` there, with a backup on another offline medium. It never goes into the repository, a
   CI secret or any other online place.
2. **Build it into the window:** the second line of `maxpack.pub` (it starts with `RW`) goes into
   `updateKey` in `gui/update.go`. A build with `updateKey` empty, as the ones before it, says in Settings
   that looking for new versions is not available in that build, and never goes on the internet. Add it
   to the README too, so anyone can check a `SHA256SUMS` by hand with `minisign -Vm SHA256SUMS -P <key>`.
3. **Changing the key** later: a window checks with the key it was built with. Sign the release that
   brings the new key with the old one; the releases after it, with the new one.
4. **If the key is lost**, the windows already out cannot be offered anything newer: say so in the release
   notes of the version with a new key, which users install by hand.
