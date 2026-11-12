#!/bin/sh
# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
# SPDX-License-Identifier: AGPL-3.0-only
#
# Sammelt den Quelltext aller GPL- und LGPL-Pakete, die in einem
# Lumen-Abbild stecken (FFmpeg, BusyBox, x264, x265 …), in ein Archiv.
# Die GPL verlangt, dass dieser Quelltext mit dem Abbild angeboten wird;
# Alpine löscht alte Fassungen irgendwann, darum legen wir ihn selbst ab.
#
# Aufruf: tools/gpl_quellen.sh ABBILD FASSUNG AUSGABE.tar
# Braucht docker (mit arm64-Emulation) und git. Läuft in GitHub Actions.

set -eu
ABBILD=$1
FASSUNG=$2
AUSGABE=$3
ALPINE_ZWEIG=3.22-stable
MIRROR=https://distfiles.alpinelinux.org/distfiles/v3.22

ARB=$(mktemp -d)
mkdir -p "$ARB/quellen"

# 1. Welche Pakete stecken drin — für beide Plattformen.
for p in linux/amd64 linux/arm64; do
  docker run --rm --platform "$p" --entrypoint apk "$ABBILD" list --installed
done | sort -u > "$ARB/alle-pakete.txt"
docker run --rm --platform linux/amd64 --entrypoint cat "$ABBILD" /etc/alpine-release \
  > "$ARB/alpine-release.txt"

# Zeile: name-1.2.3-r0 x86_64 {herkunft} (Lizenz) [installed]
# Behalten: alles, dessen Lizenz GPL nennt (auch LGPL). Ausgabe: herkunft fassung
awk '
  /\(.*GPL.*\) \[installed\]/ {
    n = split($1, t, "-"); fassung = t[n-1] "-" t[n]
    match($0, /\{[^}]+\}/); herkunft = substr($0, RSTART+1, RLENGTH-2)
    print herkunft, fassung
  }' "$ARB/alle-pakete.txt" | sort -u > "$ARB/gpl-herkunft.txt"
echo "GPL/LGPL-Herkünfte:"; cat "$ARB/gpl-herkunft.txt"

# 2. Die Baurezepte (APKBUILD und Patches) aus Alpines aports.
git clone -q --depth 1 --filter=blob:none --sparse -b "$ALPINE_ZWEIG" \
  https://gitlab.alpinelinux.org/alpine/aports.git "$ARB/aports"
pfade=$(awk '{print "main/" $1; print "community/" $1}' "$ARB/gpl-herkunft.txt" | sort -u)
# shellcheck disable=SC2086
git -C "$ARB/aports" sparse-checkout set --no-cone $pfade
git -C "$ARB/aports" rev-parse HEAD > "$ARB/aports-commit.txt"

# 3. Die Quellarchive holen — abuild lädt zuerst von Alpines Spiegel, dann
# vom Herausgeber, und prüft die Prüfsummen aus dem Rezept.
cat > "$ARB/holen.sh" <<'SKRIPT'
set -eu
apk add -q --no-progress abuild
while read -r herkunft fassung; do
  for repo in main community; do
    d=/arb/aports/$repo/$herkunft
    [ -f "$d/APKBUILD" ] || continue
    ziel=/arb/quellen/$herkunft
    mkdir -p "$ziel"
    mkdir -p "$ziel/rezept"
    cp -R "$d/." "$ziel/rezept/"
    ( cd "$d"
      . ./APKBUILD
      rezept="$pkgver-r$pkgrel"
      [ "$rezept" = "$fassung" ] || echo "$herkunft: Abbild $fassung, Rezept $rezept" >> /arb/abweichungen.txt
      SRCDEST="$ziel" abuild -F fetch && SRCDEST="$ziel" abuild -F verify ) \
      || echo "FEHLT: $herkunft" >> /arb/fehlt.txt
    break
  done
done < /arb/gpl-herkunft.txt
exit 0
SKRIPT
docker run --rm -e DISTFILES_MIRROR="$MIRROR" -v "$ARB:/arb" alpine:3.22 sh /arb/holen.sh
sudo chown -R "$(id -u):$(id -g)" "$ARB" 2>/dev/null || true

# 4. Liesmich und Archiv.
{
  echo "Lumen $FASSUNG — Quelltext der GPL- und LGPL-Pakete im Docker-Abbild"
  echo "Abbild: $ABBILD"
  echo "Alpine Linux $(cat "$ARB/alpine-release.txt"), aports $ALPINE_ZWEIG @ $(cat "$ARB/aports-commit.txt")"
  echo
  echo "Je Paket: rezept/ (APKBUILD und Patches von Alpine) und die Quellarchive."
  echo "Bauen lässt es sich mit Alpines abuild genau wie dort beschrieben."
  echo "Die vollständige Paketliste beider Plattformen: alle-pakete.txt."
  if [ -s "$ARB/abweichungen.txt" ]; then
    echo; echo "Hinweis: Bei diesen Paketen ist Alpines Rezept inzwischen neuer als das Abbild."
    echo "Die passende ältere Fassung findet sich in der Geschichte von aports:"
    cat "$ARB/abweichungen.txt"
  fi
  if [ -s "$ARB/fehlt.txt" ]; then
    echo; echo "Konnte nicht geladen werden (bitte bei den Lumen-Autoren melden, wir senden es zu):"
    cat "$ARB/fehlt.txt"
  fi
} > "$ARB/LIESMICH.txt"

tar -C "$ARB" -cf "$AUSGABE" LIESMICH.txt alle-pakete.txt gpl-herkunft.txt quellen
ls -lh "$AUSGABE"
cat "$ARB/LIESMICH.txt"
