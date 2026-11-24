#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
# La versión de un paquete a partir de la referencia de git: de una etiqueta vX.Y.Z sale X.Y.Z (sin la
# «v»), y de cualquier otra cosa, dev-<commit>. También da la parte numérica (X.Y.Z), que es la que
# aceptan el instalador de Windows, el recurso de versión de MaxPack.exe y el Info.plist de macOS.
#   version.sh <tag|branch> <nombre de la referencia> <sha del commit>
# Imprime dos líneas, VERSION=... y NUMBERS=..., listas para añadir a GITHUB_ENV.
set -euo pipefail

type="${1:?falta el tipo de referencia}"
name="${2:?falta el nombre de la referencia}"
sha="${3:?falta el commit}"

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
