# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
#
# Veröffentlicht unter der GNU Affero General Public License v3.0.
# Der vollständige Text steht in der Datei LICENSE.
# SPDX-License-Identifier: AGPL-3.0-only

#!/usr/bin/env python3
"""Jede Ansicht der Web-App hat eine Adresse.

    python3 tools/adressen_pruefen.py     # Rückgabe 0/1

Eine Ansicht ohne Eintrag in ADRESSEN (app.js) bekommt beim Öffnen keine
eigene Adresse: Der Browser zeigt weiter die alte, F5 landet woanders,
ein Lesezeichen führt ins Leere. Geprüft wird:

  a) Jede Ansicht, die render() zeichnet (case '…' und die Ansichten der
     Mediathek-Module musik.js und fotos.js), steht in ADRESSEN – oder
     ausdrücklich in OHNE_ADRESSE (Anmelden, Player, Zwischenschritte).
  b) ADRESSEN nennt keine Ansicht, die es nicht gibt, und keine Adresse
     zweimal.
  c) Jede Ansicht in MIT_KENNUNG hat eine Adresse.
  d) Jedes data-go="…" in der Web-App zeigt auf eine Ansicht, die es gibt.
"""

from __future__ import annotations

import pathlib
import re
import sys

WURZEL = pathlib.Path(__file__).resolve().parent.parent
WEB = WURZEL / "server/cmd/lumen/web"
ROT, GRUEN, AUS = "\033[31m", "\033[32m", "\033[0m"


def block(src: str, anfang: str) -> str:
    """Der Text von anfang bis zur passenden schliessenden Klammer."""
    i = src.index(anfang)
    j = src.index("[" if anfang.rstrip().endswith("[") else "{", i)
    oeffnen, schliessen = src[j], "]" if src[j] == "[" else "}"
    tiefe = 0
    for k in range(j, len(src)):
        if src[k] == oeffnen:
            tiefe += 1
        elif src[k] == schliessen:
            tiefe -= 1
            if tiefe == 0:
                return src[j:k + 1]
    raise ValueError(anfang)


def main() -> int:
    app = (WEB / "app.js").read_text(encoding="utf-8")
    fehler: list[str] = []

    render = app[app.index("function render()"):]
    render = render[:render.index("\n}\n")]
    ansichten = set(re.findall(r"case '(\w+)':", render))
    for modul in ("musik.js", "fotos.js"):
        src = (WEB / modul).read_text(encoding="utf-8")
        ansichten |= set(re.findall(r"^  (\w+): *\{", block(src, "export const ansichten = {"), re.M))

    adressen = re.findall(r"\['(\w+)', '([\w/]+)'\]", block(app, "const ADRESSEN = ["))
    ohne = set(re.findall(r"'(\w+)'", block(app, "const OHNE_ADRESSE = [")))
    kennung = set(re.findall(r"'(\w+)'", block(app, "const MIT_KENNUNG = [")))
    mit = {v for v, _ in adressen}

    for v in sorted(ansichten - mit - ohne):
        fehler.append(f"Ansicht „{v}“ hat keine Adresse (ADRESSEN oder OHNE_ADRESSE in app.js)")
    for v in sorted((mit | ohne) - ansichten):
        fehler.append(f"„{v}“ steht in ADRESSEN/OHNE_ADRESSE, render() kennt die Ansicht aber nicht")
    for v in sorted(mit & ohne):
        fehler.append(f"„{v}“ steht in ADRESSEN und in OHNE_ADRESSE")
    pfade = [p for _, p in adressen]
    for p in sorted({p for p in pfade if pfade.count(p) > 1}):
        fehler.append(f"Adresse „#/{p}“ ist zweimal vergeben")
    for v in sorted(kennung - mit):
        fehler.append(f"„{v}“ steht in MIT_KENNUNG, hat aber keine Adresse")

    for datei in sorted(WEB.glob("*.js")):
        for n, zeile in enumerate(datei.read_text(encoding="utf-8").splitlines(), 1):
            for ziel in re.findall(r'data-go="(\w+)"', zeile):
                if ziel not in ansichten:
                    fehler.append(f"{datei.name}:{n}: data-go=\"{ziel}\" – diese Ansicht gibt es nicht")

    if fehler:
        for f in fehler:
            print(f"{ROT}✗{AUS} {f}")
        return 1
    print(f"{GRUEN}✓{AUS} {len(ansichten)} Ansichten, {len(mit)} mit Adresse, {len(ohne)} bewusst ohne")
    return 0


if __name__ == "__main__":
    sys.exit(main())
