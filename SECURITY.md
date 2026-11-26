# Security policy

## Reporting a vulnerability

Please **do not open a public issue** for a security problem.

Report it privately with GitHub security advisories:

1. Go to the [Security tab of this repository](https://github.com/DeLFuS77/maxpack/security).
2. Press **Report a vulnerability**
   ([direct link](https://github.com/DeLFuS77/maxpack/security/advisories/new)).
3. Describe what you found, how to reproduce it, which version is affected and what an attacker could do.

You can write in English or Spanish. You will get an answer as soon as possible; this is a small
project maintained by one person, so please allow some days. Once the problem is fixed, the advisory is
published and you are credited if you want to be.

If the problem is in another repository, report it there the same way:
[maxpack-ps5](https://github.com/DeLFuS77/maxpack-ps5/security) (MaxPack for the console) or
[ampr-emu-ng](https://github.com/DeLFuS77/ampr-emu-ng/security) (the runtime).

## Supported versions

Only the latest release receives fixes.

## What to keep in mind

- **The console page listens on your local network.** MaxPack for the console serves its page on
  port 8095 of the PS5, with no password. Anyone on the same network can open it, so use it only on a
  network you trust and do not expose that port to the internet. The page only accepts requests that
  come through the console's own IP and from the page itself, and it refuses paths outside the
  drives it scans, but it is not a substitute for a trusted network. **Stop MaxPack** (on the page)
  when you are not using it.
- **MaxPack never writes to the source game** and does not touch keys, encryption or DRM.
- **The programs are not signed** (Windows and macOS warn you the first time). Download MaxPack only
  from the [Releases](https://github.com/DeLFuS77/maxpack/releases) of this repository and compare the
  SHA-256 of the file with `SHA256SUMS`, which is on the same page. The releases signed by the author also
  have `SHA256SUMS.minisig`, made offline with a key the CI never has ([docs/releasing.md](docs/releasing.md)).
- **Looking for new versions** (Settings › Internet, off by default) goes only to GitHub over HTTPS, and
  offers a version only when its `SHA256SUMS` is signed with one of the release keys built into MaxPack
  (`gui/internal/update/release.pub`: the main one and a spare); the installer
  it downloads on Windows must have the SHA-256 that list gives, checked again right before it runs.
- Only use `.p5z` files and game dumps that you made yourself or trust. `maxpack verify` checks that a
  `.p5z` is whole; it cannot tell you whether a game is safe to run.

## Out of scope

Vulnerabilities in the console's firmware, in the jailbreak or in other homebrew (ShadowMountPlus,
Kraken Mount, the ELF loader), and weaknesses that need physical access to your PC or your console.
Report those to their own authors.
