#!/usr/bin/env bash
# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
# SPDX-License-Identifier: AGPL-3.0-only
#
# Holt die gepinnte, selbst gebaute libmpv-2.dll (libmpv.txt), prüft die
# SHA-256 und legt sie in den Zielordner — neben Lumen.exe. Läuft im Ablauf
# „Windows-App" (Git Bash auf windows-latest; braucht sha256sum und gh).
#
#   bash mpv/laden.sh dist
#
# Quelle ist die Vorab-Veröffentlichung TAG im eigenen Repository oder,
# solange es die noch nicht gibt, das Artefakt „libmpv" des Laufs LAUF von
# libmpv.yml. Für eine Veröffentlichung (STRENG=1) muss TAG mit Prüfsummen
# gepinnt sein. Ist nichts gepinnt, nimmt das Skript auf Arbeitszweigen den
# letzten erfolgreichen Lauf von libmpv.yml auf demselben Zweig und nennt
# die Werte zum Eintragen.

set -euo pipefail
ziel=${1:?Zielordner fehlt}
hier=$(cd "$(dirname "$0")" && pwd)
TAG='' LAUF='' SHA256='' QUELLEN_SHA256=''
# shellcheck source=/dev/null
. <(grep -E '^(TAG|LAUF|SHA256|QUELLEN_SHA256)=[A-Za-z0-9._-]*$' "$hier/libmpv.txt")
repo=${GITHUB_REPOSITORY:-ada462790-beep/lumen-media}
arb=$(mktemp -d)
trap 'rm -rf "$arb"' EXIT

if [ "${STRENG:-0}" = 1 ] && { [ -z "$TAG" ] || [ -z "$SHA256" ] || [ -z "$QUELLEN_SHA256" ]; }; then
  echo "::error::libmpv ist nicht als Veröffentlichung gepinnt (TAG, SHA256, QUELLEN_SHA256 in desktop/windows/mpv/libmpv.txt) — so wird nichts veröffentlicht."
  exit 1
fi

if [ -n "$TAG" ]; then
  echo "Lade libmpv-2.dll von der Veröffentlichung $TAG"
  if ! gh release download "$TAG" -R "$repo" -p libmpv-2.dll -D "$arb"; then
    echo "::error::libmpv: Veröffentlichung $TAG (oder libmpv-2.dll daran) fehlt in $repo."
    exit 1
  fi
  herkunft="Veröffentlichung $TAG"
else
  if [ -z "$LAUF" ]; then
    zweig=${GITHUB_HEAD_REF:-${GITHUB_REF_NAME:-}}
    [ -n "$zweig" ] || { echo "::error::libmpv ist nicht gepinnt und kein Zweig bekannt."; exit 1; }
    LAUF=$(gh run list -R "$repo" -w libmpv.yml -b "$zweig" -s success -L 1 --json databaseId --jq '.[0].databaseId // empty')
    [ -n "$LAUF" ] || { echo "::error::libmpv ist nicht gepinnt, und auf $zweig gibt es keinen erfolgreichen Lauf von libmpv.yml."; exit 1; }
    echo "::warning::libmpv ungepinnt: nehme den letzten erfolgreichen libmpv-Lauf $LAUF auf $zweig."
  fi
  echo "Lade das Artefakt libmpv des Laufs $LAUF"
  if ! gh run download "$LAUF" -R "$repo" -n libmpv -D "$arb"; then
    echo "::error::libmpv: Artefakt des Laufs $LAUF nicht (mehr) zu haben — neu bauen oder als Veröffentlichung pinnen (desktop/windows/mpv/libmpv.txt)."
    exit 1
  fi
  herkunft="Lauf $LAUF"
fi

summe=$(sha256sum "$arb/libmpv-2.dll" | cut -d' ' -f1)
if [ -z "$SHA256" ]; then
  q=$(grep ' libmpv-quellen.tar.xz$' "$arb/SHA256SUMS" 2> /dev/null | cut -d' ' -f1 || true)
  echo "::warning::libmpv ungepinnt geladen: LAUF=$LAUF SHA256=$summe QUELLEN_SHA256=$q — in desktop/windows/mpv/libmpv.txt eintragen."
  { echo "### libmpv zum Pinnen"; echo; echo '```'; echo "LAUF=$LAUF"; echo "SHA256=$summe"; echo "QUELLEN_SHA256=$q"; echo '```'; } >> "${GITHUB_STEP_SUMMARY:-/dev/null}"
elif [ "$summe" != "$SHA256" ]; then
  echo "::error::Prüfsumme von libmpv-2.dll ($herkunft) stimmt nicht: $summe statt $SHA256"
  exit 1
fi

mkdir -p "$ziel"
cp "$arb/libmpv-2.dll" "$ziel/libmpv-2.dll"
ls -l "$ziel/libmpv-2.dll"
echo "libmpv: selbst gebaut, $herkunft (SHA-256 $summe)"
