#!/usr/bin/env python3
# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
#
# Veröffentlicht unter der GNU Affero General Public License v3.0.
# Der vollständige Text steht in der Datei LICENSE.
# SPDX-License-Identifier: AGPL-3.0-only

"""Richtet einen frisch gestarteten Lumen-Server für den Simulator-Ablauf ein.

    python3 tools/simulator/einrichten.py http://127.0.0.1:8097 /tmp/lumen-sim/medien

Legt über die Schnittstelle an, was die Apps danach vorfinden sollen:
den Besitzer (Name und Passwort aus LUMEN_TEST_NAME/LUMEN_TEST_PASSWORT),
die Bibliotheken „Filme" und „Serien" aus bibliothek.sh, wartet, bis der
Durchlauf fertig ist, und setzt Fortschritt für einen Film und eine Folge,
damit „Weiterschauen" etwas zeigt.

Die Rotten-Tomatoes-Werte kommen sonst nur über OMDb; hier schreibt sie
`tomaten.sql` direkt in die Datenbank (nach dem Anhalten des Servers),
siehe simulator.yml. Dieses Skript gibt die Kennungen dafür aus.

Nur Standardbibliothek — es läuft auf dem Ubuntu- wie auf dem Mac-Rechner.
"""

from __future__ import annotations

import json
import os
import sys
import time
import urllib.error
import urllib.request

BASIS = sys.argv[1].rstrip("/") if len(sys.argv) > 1 else "http://127.0.0.1:8097"
MEDIEN = sys.argv[2] if len(sys.argv) > 2 else "/tmp/lumen-sim/medien"
NAME = os.environ.get("LUMEN_TEST_NAME", "Admin")
PASSWORT = os.environ.get("LUMEN_TEST_PASSWORT", "lumen-test-2026")
TOKEN = ""


def rufe(methode: str, pfad: str, daten=None, erwarte_fehler=False):
    url = BASIS + "/api/v1" + pfad
    roh = json.dumps(daten).encode() if daten is not None else None
    anf = urllib.request.Request(url, data=roh, method=methode)
    anf.add_header("Content-Type", "application/json")
    anf.add_header("Accept-Language", "de-CH")
    if TOKEN:
        anf.add_header("Authorization", "Bearer " + TOKEN)
    try:
        with urllib.request.urlopen(anf, timeout=30) as antw:
            text = antw.read().decode() or "null"
            return json.loads(text)
    except urllib.error.HTTPError as e:
        text = e.read().decode(errors="replace")
        if erwarte_fehler:
            return {"_status": e.code, "_text": text}
        raise SystemExit(f"{methode} {pfad}: {e.code} {text}")


def warte_auf_server():
    for _ in range(120):
        try:
            return rufe("GET", "/hello")
        except Exception:  # noqa: BLE001 — Server startet noch
            time.sleep(1)
    raise SystemExit("Server antwortet nicht")


def main():
    global TOKEN
    hallo = warte_auf_server()
    print("hello:", json.dumps(hallo)[:300])

    antw = rufe("POST", "/setup", {
        "name": NAME, "password": PASSWORT, "sprache": "de-CH",
        "deviceName": "Simulator-Einrichtung", "deviceKind": "browser",
    }, erwarte_fehler=True)
    if "_status" in antw:
        # Schon eingerichtet (zweiter Lauf mit demselben Datenordner).
        profile = rufe("GET", "/profiles")
        karte = next(p for p in (profile if isinstance(profile, list) else profile.get("profiles", []))
                     if p.get("name") == NAME)
        antw = rufe("POST", "/unlock", {"profileId": karte["id"], "secret": PASSWORT,
                                        "deviceName": "Simulator-Einrichtung", "deviceKind": "browser"})
    TOKEN = antw["token"]
    print("angemeldet als", NAME)

    vorhanden = {b.get("name") for b in (rufe("GET", "/admin/libraries") or [])}
    libs = []
    for name, art, ordner in (("Filme", "movie", "Filme"), ("Serien", "series", "Serien")):
        if name in vorhanden:
            continue
        lib = rufe("POST", "/admin/libraries", {
            "name": name, "kind": art, "path": os.path.join(MEDIEN, ordner), "language": "de-CH"})
        libs.append(lib)
        print("Bibliothek", name, lib.get("id"))
    for lib in rufe("GET", "/admin/libraries") or []:
        rufe("POST", f"/admin/libraries/{lib['id']}/scan", {}, erwarte_fehler=True)

    # Warten, bis alle Titel da sind: 8 Filme (Avatar zählt einmal) und 2 Serien.
    filme = serien = []
    for _ in range(180):
        filme = rufe("GET", "/items?kind=movie&limit=200") or []
        serien = rufe("GET", "/items?kind=series&limit=200") or []
        if len(filme) >= 9 and len(serien) >= 2:
            break
        time.sleep(2)
    print(f"{len(filme)} Filme, {len(serien)} Serien")
    for f in filme:
        print("  Film:", f.get("title"), f.get("year"), f.get("id"))
    if len(filme) < 9 or len(serien) < 2:
        print("WARNUNG: nicht alle Titel erkannt", file=sys.stderr)
    # Noch etwas Ruhe für Spuren, Bilder und Filmreihen.
    time.sleep(10)

    nach_titel = {f.get("title"): f for f in filme}
    # Weiterschauen: Interstellar zur Hälfte, Dark S01E02 angefangen.
    if "Interstellar" in nach_titel:
        rufe("POST", f"/items/{nach_titel['Interstellar']['id']}/progress",
             {"positionMs": 70_000, "durationMs": 150_000})
    for s in serien:
        if s.get("title") != "Dark":
            continue
        staffeln = rufe("GET", f"/items/{s['id']}/children") or []
        if isinstance(staffeln, dict):
            staffeln = staffeln.get("items") or staffeln.get("children") or []
        for st in staffeln:
            folgen = rufe("GET", f"/items/{st['id']}/children") if st.get("kind") == "season" else [st]
            if isinstance(folgen, dict):
                folgen = folgen.get("items") or folgen.get("children") or []
            for fo in folgen or []:
                if fo.get("episodeNo") == 1:
                    rufe("POST", f"/items/{fo['id']}/finished", {}, erwarte_fehler=True)
                if fo.get("episodeNo") == 2:
                    rufe("POST", f"/items/{fo['id']}/progress", {"positionMs": 12_000, "durationMs": 30_000})
                    print("Fortschritt: Dark", fo.get("title"))

    # Rotten Tomatoes: frisch, faul — als SQL für danach.
    tomaten = {"Der Pate": 97, "The Dark Knight": 94, "Inception": 87,
               "Batman Begins": 85, "Matrix": 83, "Catwoman": 9, "Avatar – Aufbruch nach Pandora": 81}
    with open(os.environ.get("TOMATEN_SQL", "tomaten.sql"), "w", encoding="utf-8") as sql:
        for titel, prozent in tomaten.items():
            if titel in nach_titel:
                sql.write(f"UPDATE item SET rt_wertung = {prozent} WHERE id = '{nach_titel[titel]['id']}';\n")

    home = rufe("GET", "/home")
    print("Startseite:", json.dumps(home, ensure_ascii=False)[:1500])


if __name__ == "__main__":
    main()
