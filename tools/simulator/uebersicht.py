#!/usr/bin/env python3
# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
#
# Veröffentlicht unter der GNU Affero General Public License v3.0.
# Der vollständige Text steht in der Datei LICENSE.
# SPDX-License-Identifier: AGPL-3.0-only

"""Schreibt die Übersicht eines Rundgangs für $GITHUB_STEP_SUMMARY.

    python3 tools/simulator/uebersicht.py "Android — Handy" fotos/android-handy

Liest bericht.json (Android) oder bericht-*.txt (Apple) und zählt die
Fotos. Gibt Markdown auf stdout aus.
"""

from __future__ import annotations

import glob
import json
import os
import sys

titel = sys.argv[1]
ordner = sys.argv[2]

fotos = sorted(glob.glob(os.path.join(ordner, "**", "*.png"), recursive=True))
print(f"## {titel}\n")
print(f"{len(fotos)} Bildschirmfoto(s) im Artefakt.\n")

zeilen: list[str] = []
abstuerze: list[str] = []
for pfad in sorted(glob.glob(os.path.join(ordner, "**", "bericht.json"), recursive=True)):
    try:
        daten = json.load(open(pfad, encoding="utf-8"))
    except (OSError, ValueError):
        continue
    for s in daten.get("schritte", []):
        zeichen = "✅" if s.get("ok") else "⚠️"
        zeilen.append(f"| {zeichen} | `{s.get('foto')}` | {s.get('notiz', '')} |")
    abstuerze += daten.get("abstuerze", [])
for pfad in sorted(glob.glob(os.path.join(ordner, "**", "bericht-*.txt"), recursive=True)):
    for z in open(pfad, encoding="utf-8", errors="replace").read().splitlines():
        if not z.strip():
            continue
        ok = z.startswith("[ok]")
        teile = z[4:].strip().split(" ", 1)
        zeilen.append(f"| {'✅' if ok else '⚠️'} | `{teile[0]}` | {teile[1] if len(teile) > 1 else ''} |")

if zeilen:
    print("| | Foto | Notiz |\n|---|---|---|")
    print("\n".join(zeilen))
else:
    print("Kein Bericht — der Rundgang ist vermutlich nicht angelaufen (Protokoll ansehen).")
if abstuerze:
    print("\n**Abstürze laut logcat:**\n")
    print("```")
    print("\n".join(abstuerze[:20]))
    print("```")
print()
