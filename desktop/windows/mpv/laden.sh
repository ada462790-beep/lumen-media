#!/usr/bin/env bash
# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
# SPDX-License-Identifier: AGPL-3.0-only
#
# Holt die gepinnte libmpv-2.dll (libmpv.txt), prüft die SHA-256 und legt
# sie in den Zielordner — neben Lumen.exe. Läuft im Ablauf „Windows-App"
# (Git Bash auf windows-latest; braucht curl, sha256sum, 7z, gh).
#
#   bash mpv/laden.sh dist
#
# Ist in libmpv.txt keine Prüfsumme eingetragen, sucht das Skript die Datei
# in der Veröffentlichung TAG, nennt Name und Prüfsumme und macht weiter —
# aber nur auf Arbeitszweigen: Für eine Veröffentlichung (STRENG=1) muss
# alles gepinnt sein.

set -euo pipefail
ziel=${1:?Zielordner fehlt}
hier=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=/dev/null
. <(grep -E '^(TAG|DATEI|SHA256|REZEPT)=' "$hier/libmpv.txt")
repo=shinchiro/mpv-winbuild-cmake
arb=$(mktemp -d)
trap 'rm -rf "$arb"' EXIT

if [ -z "${SHA256:-}" ] || [ -z "${DATEI:-}" ]; then
  if [ "${STRENG:-0}" = 1 ]; then
    echo "::error::libmpv ist nicht gepinnt (desktop/windows/mpv/libmpv.txt) — so wird nichts veröffentlicht."
    exit 1
  fi
  echo "Dateien der Veröffentlichung $TAG:"
  gh release view "$TAG" -R "$repo" --json assets --jq '.assets[].name' | tee "$arb/liste.txt"
  DATEI=$(grep -E '^mpv-dev-x86_64-[0-9]{8}-git-[0-9a-f]+\.7z$' "$arb/liste.txt" | head -n1 || true)
  [ -n "$DATEI" ] || DATEI=$(grep -E '^mpv-dev-x86_64-(gcc-)?[0-9]{8}-git-[0-9a-f]+\.7z$' "$arb/liste.txt" | head -n1 || true)
  [ -n "$DATEI" ] || { echo "::error::Keine mpv-dev-x86_64-Datei in $TAG"; exit 1; }
fi

url="https://github.com/$repo/releases/download/$TAG/$DATEI"
echo "Lade $url"
if ! curl -fsSL --retry 3 --retry-delay 5 -o "$arb/$DATEI" "$url"; then
  echo "::error::libmpv $TAG/$DATEI ist bei $repo nicht mehr zu haben — Pin erneuern (Anleitung in desktop/windows/mpv/libmpv.txt)."
  exit 1
fi
summe=$(sha256sum "$arb/$DATEI" | cut -d' ' -f1)
if [ -z "${SHA256:-}" ]; then
  echo "::warning::libmpv ungepinnt geladen: DATEI=$DATEI SHA256=$summe — in desktop/windows/mpv/libmpv.txt eintragen."
  { echo "### libmpv zum Pinnen"; echo; echo '```'; echo "TAG=$TAG"; echo "DATEI=$DATEI"; echo "SHA256=$summe"; echo '```'; } >> "${GITHUB_STEP_SUMMARY:-/dev/null}"
elif [ "$summe" != "$SHA256" ]; then
  echo "::error::Prüfsumme von $DATEI stimmt nicht: $summe statt $SHA256"
  exit 1
fi

7z e -y -o"$arb/aus" "$arb/$DATEI" libmpv-2.dll >/dev/null
mkdir -p "$ziel"
cp "$arb/aus/libmpv-2.dll" "$ziel/libmpv-2.dll"
ls -l "$ziel/libmpv-2.dll"
echo "libmpv: $TAG $DATEI (SHA-256 $summe, Rezept $REZEPT)"
