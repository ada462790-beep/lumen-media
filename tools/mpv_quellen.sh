#!/bin/sh
# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
# SPDX-License-Identifier: AGPL-3.0-only
#
# Legt den Quelltext der libmpv-2.dll, die im Installer von Lumen für
# Windows steckt, als Datei für die Veröffentlichung bereit. Die DLL ist
# selbst gebaut (.github/workflows/libmpv.yml); derselbe Lauf hat dabei
# libmpv-quellen.tar.xz gepackt — genau die gebauten Quellbäume (Tag und
# Commit je Paket aus desktop/windows/mpv/bau/pakete.txt), das Bau-Skript
# und das Verzeichnis der Werkzeuge. Dieses Archiv wird hier nur geholt,
# gegen QUELLEN_SHA256 aus desktop/windows/mpv/libmpv.txt geprüft und
# umbenannt. Die GPL verlangt, dass dieser Quelltext mit der DLL angeboten
# wird.
#
# Aufruf: tools/mpv_quellen.sh FASSUNG AUSGABE.tar.xz
# Braucht gh (GH_TOKEN) und sha256sum. Läuft in GitHub Actions (quellen.yml).

set -eu
FASSUNG=$1
AUSGABE=$2
WURZEL=$(cd "$(dirname "$0")/.." && pwd)
TAG='' LAUF='' QUELLEN_SHA256=''
eval "$(grep -E '^(TAG|LAUF|QUELLEN_SHA256)=[A-Za-z0-9._-]*$' "$WURZEL/desktop/windows/mpv/libmpv.txt")"
: "${QUELLEN_SHA256:?QUELLEN_SHA256 fehlt — libmpv ist nicht gepinnt (desktop/windows/mpv/libmpv.txt)}"
REPO=${GITHUB_REPOSITORY:-ada462790-beep/lumen-media}

ARB=$(mktemp -d)
trap 'rm -rf "$ARB"' EXIT
if [ -n "$TAG" ]; then
  gh release download "$TAG" -R "$REPO" -p libmpv-quellen.tar.xz -D "$ARB"
  HERKUNFT="Veröffentlichung $TAG"
elif [ -n "$LAUF" ]; then
  echo "Achtung: libmpv nur als Lauf $LAUF gepinnt (Artefakt verfällt nach 90 Tagen)." >&2
  gh run download "$LAUF" -R "$REPO" -n libmpv -D "$ARB"
  HERKUNFT="Lauf $LAUF"
else
  echo "Weder TAG noch LAUF in desktop/windows/mpv/libmpv.txt" >&2
  exit 1
fi
SUMME=$(sha256sum "$ARB/libmpv-quellen.tar.xz" | cut -d' ' -f1)
if [ "$SUMME" != "$QUELLEN_SHA256" ]; then
  echo "Prüfsumme des Quellenarchivs ($HERKUNFT) stimmt nicht: $SUMME statt $QUELLEN_SHA256" >&2
  exit 1
fi
mv "$ARB/libmpv-quellen.tar.xz" "$AUSGABE"
echo "Lumen $FASSUNG — libmpv-Quellen aus $HERKUNFT: $AUSGABE ($(du -h "$AUSGABE" | cut -f1), SHA-256 $SUMME)"
xz -dc "$AUSGABE" | tar -xOf - LIESMICH.txt
