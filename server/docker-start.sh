#!/bin/sh
# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
# SPDX-License-Identifier: AGPL-3.0-only
#
# Startet Lumen im Container — mit den Rechten, die zum Datenordner passen.
#
# Warum es das braucht: Das Abbild lief fest als Benutzer 1000. Der
# Datenordner /data gehörte aber root — bei einem benannten Volume, weil
# Docker ihn so aus dem Abbild übernimmt, und auf einem NAS, weil dort die
# freigegebenen Ordner einem anderen Benutzer gehören. Lumen konnte seine
# Datenbank nicht anlegen, beendete sich, und Docker startete es wieder
# und wieder.
#
# Jetzt startet der Container als root, macht /data für Lumen beschreibbar
# und gibt die Rechte dann ab. Mit PUID und PGID läuft Lumen unter einem
# bestimmten Benutzer — auf einem Synology etwa PUID=1026, PGID=100 —, so
# dass es die Medienordner lesen darf, die diesem Benutzer gehören.
#
# Wer den Container selbst mit `user:` startet, bekommt genau das: dann
# rührt dieses Skript nichts an.
set -e

DATEN="${LUMEN_DATA:-/data}"

if [ "$(id -u)" = "0" ]; then
  PUID="${PUID:-1000}"
  PGID="${PGID:-1000}"

  gruppe_zu() { awk -F: -v g="$1" '$3 == g { print $1; exit }' /etc/group; }

  # Den Benutzer lumen auf PUID/PGID bringen.
  if [ "$(id -u lumen 2>/dev/null)" != "$PUID" ] || [ "$(id -g lumen 2>/dev/null)" != "$PGID" ]; then
    deluser lumen 2>/dev/null || true
    G="$(gruppe_zu "$PGID")"
    if [ -z "$G" ]; then addgroup -g "$PGID" lumen; G=lumen; fi
    adduser -D -H -u "$PUID" -G "$G" lumen
  fi

  # Grafikeinheit fürs Umwandeln: lumen kommt in die Gruppe des Geräts,
  # sonst sieht es /dev/dri nach dem Abgeben der Rechte nicht mehr.
  for geraet in /dev/dri/renderD* /dev/dri/card*; do
    [ -e "$geraet" ] || continue
    GID="$(stat -c %g "$geraet")"
    G="$(gruppe_zu "$GID")"
    if [ -z "$G" ]; then G="dri$GID"; addgroup -g "$GID" "$G"; fi
    addgroup lumen "$G" 2>/dev/null || true
  done

  mkdir -p "$DATEN"
  # Nur wenn nötig, und dann alles darin: Poster und Zwischenspeicher aus
  # einem früheren Lauf als root sollen Lumen nicht im Weg stehen.
  if [ "$(stat -c %u "$DATEN")" != "$PUID" ] || [ "$(stat -c %g "$DATEN")" != "$PGID" ]; then
    chown -R "$PUID:$PGID" "$DATEN"
  fi

  # Über den Namen, nicht die Nummer: so gelten auch die Gruppen von eben.
  exec su-exec lumen /usr/local/bin/lumen "$@"
fi

exec /usr/local/bin/lumen "$@"
