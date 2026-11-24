#!/bin/sh
# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
# SPDX-License-Identifier: AGPL-3.0-only
#
# Sammelt den Quelltext der libmpv-2.dll, die im Installer von Lumen für
# Windows steckt (desktop/windows/mpv/libmpv.txt), in ein Archiv: das
# Bau-Rezept von shinchiro beim gepinnten Commit, mpv beim Commit aus dem
# Dateinamen und jede Bibliothek, die das Rezept einbaut — mit festem Tag,
# wenn das Rezept einen nennt, sonst der letzte Commit des Bautags (das
# Rezept baut solche Bibliotheken vom jeweils neuesten Stand). Die GPL
# verlangt, dass dieser Quelltext mit der DLL angeboten wird.
#
# Aufruf: tools/mpv_quellen.sh FASSUNG AUSGABE.tar
# Braucht git und curl. Läuft in GitHub Actions (quellen.yml). NUR="mpv
# ffmpeg" beschränkt auf einzelne Pakete (zum Ausprobieren).

set -eu
FASSUNG=$1
AUSGABE=$2
WURZEL=$(cd "$(dirname "$0")/.." && pwd)
eval "$(grep -E '^(TAG|DATEI|SHA256|REZEPT)=[A-Za-z0-9._-]*$' "$WURZEL/desktop/windows/mpv/libmpv.txt")"
: "${TAG:?TAG fehlt}" "${REZEPT:?REZEPT fehlt}" "${DATEI:?DATEI fehlt — libmpv ist nicht gepinnt}"
MPV=$(printf '%s' "$DATEI" | sed -n 's/.*-git-\([0-9a-f]\{7,40\}\)\.7z$/\1/p')
[ -n "$MPV" ] || { echo "Kein mpv-Commit im Dateinamen: $DATEI" >&2; exit 1; }
BIS="$(printf '%s' "$TAG" | cut -c1-4)-$(printf '%s' "$TAG" | cut -c5-6)-$(printf '%s' "$TAG" | cut -c7-8)T23:59:59Z"

ARB=$(mktemp -d)
mkdir -p "$ARB/quellen" "$ARB/git"
git clone -q https://github.com/shinchiro/mpv-winbuild-cmake.git "$ARB/rezept"
git -C "$ARB/rezept" checkout -q "$REZEPT"
git -C "$ARB/rezept" archive --prefix=mpv-winbuild-cmake/ -o "$ARB/quellen/mpv-winbuild-cmake-$REZEPT.tar.gz" "$REZEPT"

: > "$ARB/liste.txt"
: > "$ARB/fehlt.txt"
for f in "$ARB"/rezept/packages/*.cmake; do
  name=$(basename "$f" .cmake)
  if [ -n "${NUR:-}" ] && ! printf ' %s ' "$NUR" | grep -q " $name "; then continue; fi
  repo=$(sed -n 's/^[[:space:]]*GIT_REPOSITORY[[:space:]]\{1,\}\([^[:space:])]*\).*/\1/p' "$f" | head -n1)
  tag=$(sed -n 's/^[[:space:]]*GIT_TAG[[:space:]]\{1,\}\([^[:space:])]*\).*/\1/p' "$f" | head -n1)
  url=$(sed -n 's/^[[:space:]]*URL[[:space:]]\{1,\}\([^[:space:])]*\).*/\1/p' "$f" | head -n1)
  if [ -n "$repo" ]; then
    d="$ARB/git/$name"
    if ! git clone -q --filter=blob:none --no-checkout "$repo" "$d" 2>/dev/null; then
      echo "$name $repo (nicht erreichbar)" >> "$ARB/fehlt.txt"; continue
    fi
    if [ "$name" = mpv ]; then
      commit=$(git -C "$d" rev-parse "$MPV^{commit}")
    elif [ -n "$tag" ]; then
      commit=$(git -C "$d" rev-parse "$tag^{commit}" 2>/dev/null || true)
    else
      commit=$(git -C "$d" rev-list -1 --before="$BIS" HEAD)
    fi
    if [ -z "$commit" ]; then echo "$name $repo (kein Commit)" >> "$ARB/fehlt.txt"; continue; fi
    git -C "$d" archive --prefix="$name/" -o "$ARB/quellen/$name-$commit.tar.gz" "$commit"
    echo "$name $repo $commit${tag:+ (Tag $tag)}" >> "$ARB/liste.txt"
    rm -rf "$d"
  elif [ -n "$url" ]; then
    if curl -fsSL --retry 3 -o "$ARB/quellen/$(basename "$url")" "$url"; then
      echo "$name $url" >> "$ARB/liste.txt"
    else
      echo "$name $url (nicht erreichbar)" >> "$ARB/fehlt.txt"
    fi
  else
    echo "$name (keine Quelle im Rezept erkannt)" >> "$ARB/fehlt.txt"
  fi
done

{
  echo "Lumen $FASSUNG — Quelltext der libmpv-2.dll im Installer von Lumen für Windows"
  echo "libmpv: https://github.com/shinchiro/mpv-winbuild-cmake/releases/tag/$TAG ($DATEI, SHA-256 $SHA256)"
  echo "Bau-Rezept: shinchiro/mpv-winbuild-cmake @ $REZEPT (mpv-winbuild-cmake-$REZEPT.tar.gz)"
  echo "mpv: $MPV"
  echo "Bibliotheken ohne festen Tag: letzter Commit bis $BIS (Bautag)."
  echo
  echo "Pakete (Name, Quelle, Commit):"
  cat "$ARB/liste.txt"
  if [ -s "$ARB/fehlt.txt" ]; then echo; echo "Nicht gesammelt:"; cat "$ARB/fehlt.txt"; fi
} > "$ARB/quellen/LIESMICH.txt"
tar -C "$ARB" -cf "$AUSGABE" quellen
echo "Archiv: $AUSGABE ($(du -h "$AUSGABE" | cut -f1))"
cat "$ARB/quellen/LIESMICH.txt"
rm -rf "$ARB"
