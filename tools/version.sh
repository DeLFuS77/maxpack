#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
# A package's version from the git reference: a vX.Y.Z tag gives X.Y.Z (without the
# "v"), and anything else gives dev-<commit>. It also gives the numeric part (X.Y.Z), which is what
# the Windows installer, MaxPack.exe's version resource and macOS's Info.plist accept.
#   version.sh <tag|branch> <name of the reference> <commit sha>
# Prints two lines, VERSION=... and NUMBERS=..., ready to append to GITHUB_ENV.
set -euo pipefail

type="${1:?missing the reference type}"
name="${2:?missing the reference name}"
sha="${3:?missing the commit}"

version="dev-${sha:0:7}"
if [ "${type}" = "tag" ]; then
  version="${name#v}"
fi

numbers="0.0.0"
if [[ "${version}" =~ ^([0-9]+\.[0-9]+\.[0-9]+)(-[0-9A-Za-z.-]+)?$ ]]; then
  numbers="${BASH_REMATCH[1]}"
fi
echo "VERSION=${version}"
echo "NUMBERS=${numbers}"
