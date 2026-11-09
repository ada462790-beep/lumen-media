# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
#
# Veröffentlicht unter der GNU Affero General Public License v3.0.
# Der vollständige Text steht in der Datei LICENSE.
# SPDX-License-Identifier: AGPL-3.0-only

#!/usr/bin/env python3
"""Hält Server und Apple-App gegeneinander.

Der Apple-Code lässt sich nur auf einem Mac übersetzen. Zwei Fehlerarten
fallen dort aber erst zur Laufzeit auf — und dann als leerer Bildschirm:

  1. Die App ruft einen Pfad auf, den der Server nicht hat.
  2. Die App erwartet ein JSON-Feld, das anders heisst.

Beides lässt sich ohne Übersetzer prüfen: der Server ist die Wahrheit, und
diese Datei vergleicht, was die App davon annimmt.

    python3 tools/schnittstelle_pruefen.py

Rückgabe 0, wenn alles zusammenpasst, sonst 1.
"""

from __future__ import annotations

import json
import pathlib
import xml.etree.ElementTree as ET
import re
import sys

WURZEL = pathlib.Path(__file__).resolve().parent.parent
SERVER = WURZEL / "server"
APPLE = WURZEL / "apple"
ANDROID = WURZEL / "android"

GRUEN, ROT, GELB, AUS = "\033[32m", "\033[31m", "\033[33m", "\033[0m"


# --------------------------------------------------------------- Pfade

def server_routen() -> set[tuple[str, str]]:
    """Die Routen, die der Server anbietet — aus seiner Routentabelle."""
    quelle = (SERVER / "internal/api/api.go").read_text(encoding="utf-8")
    routen = set()
    muster = re.compile(r'mux\.Handle(?:Func)?\(\s*"(GET|POST|PUT|PATCH|DELETE) ([^"]+)"')
    for methode, pfad in muster.findall(quelle):
        routen.add((methode, pfad.replace("/api/v1", "", 1)))
    return routen


CLIENTS = {
    "Swift": APPLE / "LumenKit/Sources/LumenKit/APIClient.swift",
    "Kotlin": ANDROID / "lumen-core/src/main/kotlin/org/lumen/core/ApiClient.kt",
}


def app_aufrufe(datei):
    """Was eine App aufruft - aus ihrem Client, mit Zeilennummer."""
    aufrufe = []
    einfach = re.compile(r'\b(?:get|getJson|url)\(\s*"([^"]+)"')
    mit_methode = re.compile(
        r'\b(?:send|request)\(\s*"(GET|POST|PUT|PATCH|DELETE)"\s*,\s*"([^"]+)"')

    for nr, zeile in enumerate(datei.read_text(encoding="utf-8").splitlines(), 1):
        if zeile.lstrip().startswith("//"):
            continue
        for methode, pfad in mit_methode.findall(zeile):
            aufrufe.append((methode, pfad, nr))
        for pfad in einfach.findall(zeile):
            if pfad.startswith("/"):
                aufrufe.append(("GET", pfad, nr))
    return aufrufe


def vereinheitlichen(pfad):
    """Platzhalter in Swift, Kotlin und im Server meinen dasselbe."""
    pfad = re.sub(r'\\\(([^)]+)\)', '{}', pfad)        # Swift
    pfad = re.sub(r'\$\{[^}]+\}', '{}', pfad)            # Kotlin ${...}
    pfad = re.sub(r'\$[A-Za-z_][A-Za-z0-9_.]*', '{}', pfad)  # Kotlin $name
    pfad = re.sub(r'\{[^}]+\}', '{}', pfad)             # Server
    return pfad


# Was eine App schon benutzt, der Server aber erst im parallelen Zweig
# bekommt — mit dem Grund. Solange der Server es nicht hat, steht es gelb
# da statt rot; sobald er es hat, prüft der gewöhnliche Weg mit, und der
# Eintrag hier kann weg. Nie für Tippfehler: nur für angekündigte
# Schnittstellen, deren Form feststeht.
ANGEKUENDIGT_PFADE = {
    # Sprache des Servers (Verwaltung, nur Mac); Arbeit „sprache".
    ("PUT", "/admin/sprache"): "Sprache des Servers",
}
ANGEKUENDIGT_FELDER = {
    # GET /hello: Vorgabe und Angebot der Sprachen; Arbeit „sprache".
    ("Hello", "sprache"), ("Hello", "sprachen"),
}


def pfade_pruefen():
    routen = {(m, vereinheitlichen(p)) for m, p in server_routen()}
    fehler = []
    print(f"  {len(routen)} Routen im Server")

    for name, datei in CLIENTS.items():
        if not datei.exists():
            continue
        benutzt = set()
        for methode, pfad, nr in app_aufrufe(datei):
            schluessel = (methode, vereinheitlichen(pfad))
            benutzt.add(schluessel)
            if schluessel not in routen and (methode, pfad) in ANGEKUENDIGT_PFADE:
                print(f"    {GELB}angekündigt, im Server noch nicht da:{AUS} {methode} {pfad} "
                      f"({ANGEKUENDIGT_PFADE[(methode, pfad)]})")
                continue
            if schluessel not in routen:
                fehler.append(
                    f"{datei.name}:{nr}: die {name}-App ruft {methode} {pfad} auf - "
                    f"diesen Pfad hat der Server nicht")
        print(f"  {name}: {len(benutzt)} davon benutzt")
        ungenutzt = sorted(f"{m} {p}" for m, p in routen - benutzt)
        if ungenutzt:
            print(f"    {GELB}noch nicht drin:{AUS} " + ", ".join(ungenutzt[:5])
                  + (f" ... (+{len(ungenutzt) - 5})" if len(ungenutzt) > 5 else ""))
    return fehler


# --------------------------------------------------------------- Felder

def go_felder() -> dict[str, set[str]]:
    """Die JSON-Namen aus den Go-Strukturen, nach Struktur gruppiert."""
    felder: dict[str, set[str]] = {}
    for datei in sorted(SERVER.rglob("*.go")):
        if "vendor" in datei.parts or datei.name.endswith("_test.go"):
            continue
        quelle = datei.read_text(encoding="utf-8")
        for block in re.finditer(r'type\s+(\w+)\s+struct\s*\{(.*?)\n\}', quelle, re.S):
            name, rumpf = block.group(1), block.group(2)
            namen = set()
            for tag in re.finditer(r'`[^`]*json:"([^"]+)"', rumpf):
                feld = tag.group(1).split(",")[0]
                if feld and feld != "-":
                    namen.add(feld)
            if namen:
                felder.setdefault(name, set()).update(namen)
    return felder


def swift_felder() -> dict[str, tuple[set[str], int]]:
    """Die Eigenschaften der Swift-Strukturen, mit Zeilennummer."""
    quelle = (APPLE / "LumenKit/Sources/LumenKit/Models.swift").read_text(encoding="utf-8")
    zeilen = quelle.splitlines()
    ergebnis: dict[str, tuple[set[str], int]] = {}

    for block in re.finditer(
            r'public struct (\w+): [^{]*\{(.*?)\n\}', quelle, re.S):
        name, rumpf = block.group(1), block.group(2)
        nr = quelle[:block.start()].count("\n") + 1
        namen = set()
        for eigenschaft in re.finditer(
                r'public let (\w+)\s*:', rumpf):
            namen.add(eigenschaft.group(1))
        if namen:
            ergebnis[name] = (namen, nr)
    _ = zeilen
    return ergebnis


def kotlin_felder() -> dict[str, tuple[set[str], int]]:
    """Die JSON-Namen, die die Kotlin-Modelle nachschlagen.

    Kotlin liest jedes Feld selbst heraus - j["name"]. Ein Tippfehler darin
    gibt kein Fehlschlagen, sondern still null: die App sieht dann einfach
    nie, was der Server sagt. Genau das soll hier auffallen.
    """
    quelle = (ANDROID / "lumen-core/src/main/kotlin/org/lumen/core/Models.kt"
              ).read_text(encoding="utf-8")
    ergebnis: dict[str, tuple[set[str], int]] = {}

    for block in re.finditer(r'data class (\w+)\((.*?)\n\}', quelle, re.S):
        name, rumpf = block.group(1), block.group(2)
        nr = quelle[:block.start()].count("\n") + 1
        namen = {m.group(1) for m in re.finditer(r'j\["(\w+)"\]', rumpf)}
        if namen:
            ergebnis[name] = (namen, nr)
    return ergebnis


# Welche Swift-Struktur welcher Go-Struktur entspricht. Nur was hier steht,
# wird geprüft — alles andere sind Hilfsstrukturen ohne Gegenstück.
PAARE = {
    "Account": "Account",
    "Limits": "Limits",
    "Hello": "helloBody",
    "Item": ["Item", "view", "listenEintrag"],
    # Filmreihen und Listen
    "Reihe": "reiheAntwort",
    "Liste": ["Liste", "listeAntwort"],
    "ListenPerson": "ListenPerson",
    "MediaFile": "MediaFile",
    # Spuren, Kapitel und der Vorspann — was der Player zum Wählen und
    # Überspringen braucht.
    "Track": "Track",
    "Chapter": "Chapter",
    "Vorspann": "Vorspann",
    # Die Suche: Personen aus der Besetzung und die Deutung der Eingabe.
    "PersonTreffer": "PersonTreffer",
    "Deutung": "Deutung",
    "Wish": "Wish",
    # Die Apps bekommen nicht die blosse Bibliothek, sondern die Ansicht
    # darauf: samt Stand des Durchlaufs und der Auskunft, ob der Ordner
    # noch da ist.
    "Library": ["Library", "bibliothekAnsicht"],
    "ScanFortschritt": "Fortschritt",
    "LiveSession": "AdminSession",
    "ServerLoad": "Load",
    "LogLine": "LogLine",
    "Device": "Device",
    "PlaybackPlan": "Plan",
    "SharedRating": None,   # anonyme Struktur im Server, von Hand geprüft
    # Spuren, Kapitel und der Vorspann einer Serie
    "Track": "Track",
    "Chapter": "Chapter",
    "Vorspann": "Vorspann",
    # Die Suche: Personen und Deutung sind Strukturen, die Antwort selbst
    # ist im Server eine map — darum ohne Gegenstück.
    "PersonTreffer": "PersonTreffer",
    "Deutung": "Deutung",
    "Suchergebnis": None,
    # Neuigkeiten: die Einträge sind eine Struktur, die Antwort eine map.
    "Neuigkeit": "Neuigkeit",
    "Neuigkeiten": None,
    # Die Besetzung auf der Titelseite — im Server eine Struktur im Handler.
    "Mitwirkender": "person",
    # Das Raster ist im Server eine map.
    "ItemSeite": None,
    # Verwaltung auf dem Mac: Aufräumen, Reihen, Konten, Einladungen.
    "Duplikat": "Duplicate",
    "Aufraeumen": None,
    "ReiheZeile": "zeile",
    "KontoEintrag": ["Account", "entry"],
    "Einladung": "Invite",
    # Dieselbe Besetzung in Kotlin; Name und Rolle wie store.Person.
    "Besetzung": ["Person", "person"],
    # Eine Seite des Rasters (GET /items?seite=1) ist im Server eine map —
    # die Titel darin sind die Ansicht oben.
    "Seite": None,
}


def felder_pruefen() -> list[str]:
    go = go_felder()
    fehler = []
    geprueft = 0

    for datei, modelle in (("Models.swift", swift_felder()),
                           ("Models.kt", kotlin_felder())):
        for app_name, go_name in PAARE.items():
            if go_name is None or app_name not in modelle:
                continue
            namen = set()
            for n in (go_name if isinstance(go_name, list) else [go_name]):
                namen |= go.get(n, set())
            if not namen:
                fehler.append(f"{datei}: zu {app_name} fand sich keine Go-Struktur "
                              f"namens {go_name}")
                continue

            eigene, nr = modelle[app_name]
            geprueft += 1
            unbekannt = sorted(eigene - namen)
            angekuendigt = [f for f in unbekannt if (app_name, f) in ANGEKUENDIGT_FELDER]
            if angekuendigt and datei == "Models.swift":
                print(f"    {GELB}angekündigt, im Server noch nicht da:{AUS} {app_name}."
                      + f", {app_name}.".join(angekuendigt))
            unbekannt = [f for f in unbekannt if (app_name, f) not in ANGEKUENDIGT_FELDER]
            if unbekannt:
                fehler.append(
                    f"{datei}:{nr}: {app_name} erwartet Felder, die der Server "
                    f"nicht schickt: {', '.join(unbekannt)}")

    print(f"  {geprueft} Strukturen gegen den Server gehalten")
    return fehler


# --------------------------------------------------------------- Bauform

def swift_dateien() -> list[pathlib.Path]:
    return sorted(APPLE.rglob("*.swift"))


def kotlin_dateien() -> list[pathlib.Path]:
    # Auch die .kts-Bauskripte: dort steht Kotlin, und eine Klammer zu viel
    # merkt man sonst erst, wenn Gradle startet — also frühestens auf einer
    # Maschine mit Android-SDK.
    return sorted(list(ANDROID.rglob("*.kt")) + list(ANDROID.rglob("*.kts")))


def ohne_text(quelle: str) -> str:
    """Zeichenketten, Zeichen und Kommentare entfernen.

    Ohne das zaehlt eine Klammer in einem Text mit - und Json.kt, das
    ausgerechnet ueber Klammern parst, waere immer unausgeglichen.
    Die Reihenfolge ist wichtig: Zeichenliterale zuerst, sonst haelt ein
    einzelnes Anfuehrungszeichen in '"' den Rest der Datei fuer Text.
    """
    # Zeichenliterale: 'x', '\n', '\u00fc'
    quelle = re.sub(r"'(?:\\u[0-9a-fA-F]{4}|\\.|[^'\\])'", "' '", quelle)
    # Dreifache Anfuehrungszeichen (Kotlin)
    quelle = re.sub(r'"""', '\x00', quelle)
    quelle = re.sub(r'\x00.*?\x00', '""', quelle, flags=re.S)
    quelle = quelle.replace('\x00', '"')
    # Swift-Rohtext #"..."#
    quelle = re.sub(r'#"(?:[^"]|"(?!#))*"#', '""', quelle, flags=re.S)
    # Gewoehnliche Zeichenketten
    quelle = re.sub(r'"(?:[^"\\\n]|\\.)*"', '""', quelle)
    # Kommentare
    quelle = re.sub(r'/\*.*?\*/', '', quelle, flags=re.S)
    quelle = re.sub(r'//[^\n]*', '', quelle)
    return quelle


def bauform_pruefen():
    """Was ein Uebersetzer sofort faende - und wir fuer Swift nicht haben.

    Geprueft wird nur, was sich ohne Uebersetzer verlaesslich sagen laesst:
    ausgeglichene Klammern und dass jede Ansicht einen Rumpf hat. Fuer
    lumen-core ist das ohnehin nur ein Vorbote - dort entscheidet kotlinc.
    """
    fehler = []
    geprueft = 0

    for datei in swift_dateien() + kotlin_dateien():
        quelle = datei.read_text(encoding="utf-8")
        geprueft += 1
        ohne = ohne_text(quelle)

        for auf, zu, name in [("{", "}", "geschweifte"), ("(", ")", "runde"),
                              ("[", "]", "eckige")]:
            diff = ohne.count(auf) - ohne.count(zu)
            if diff:
                fehler.append(
                    f"{datei.relative_to(WURZEL)}: {abs(diff)} {name} Klammer(n) "
                    f"{'zu viel' if diff > 0 else 'zu wenig'}")

        # Jede SwiftUI-Ansicht braucht einen Rumpf.
        if datei.suffix == ".swift":
            for ansicht in re.finditer(r'struct (\w+)[^{\n]*\bView\b[^{\n]*\{', quelle):
                name = ansicht.group(1)
                if "var body" not in quelle[ansicht.end():ansicht.end() + 4000]:
                    fehler.append(
                        f"{datei.relative_to(WURZEL)}: {name} ist eine View ohne var body")

    print(f"  {geprueft} Swift- und Kotlin-Dateien auf Bauform geprueft")
    return fehler


# --------------------------------------------------------------- Lauf

def main() -> int:
    if not SERVER.exists() or not APPLE.exists():
        print("server/ oder apple/ nicht gefunden", file=sys.stderr)
        return 2

    print("Pfade")
    fehler = pfade_pruefen()
    print("Felder")
    fehler += felder_pruefen()
    print("Bauform")
    fehler += bauform_pruefen()
    print("Ressourcen")
    fehler += android_ressourcen()
    fehler += xml_pruefen()
    fehler += apple_kataloge()
    fehler += bauskripte_pruefen()

    print()
    if fehler:
        for f in fehler:
            print(f"{ROT}✗{AUS} {f}")
        print(f"\n{ROT}{len(fehler)} Abweichung(en).{AUS}")
        return 1

    print(f"{GRUEN}✓ App und Server passen zusammen.{AUS}")
    return 0


# --------------------------------------------------------------- Ressourcen

def android_ressourcen() -> list[str]:
    """Jeder R.-Verweis und jeder @-Verweis im Manifest muss etwas finden.

    Das ist die Sorte Fehler, die ohne Uebersetzer unsichtbar bleibt: ein
    R.mipmap.ic_launcher, zu dem keine Datei gehoert, faellt erst beim
    Bauen auf — oder schlimmer, beim Starten auf dem Geraet.
    """
    fehler = []
    geprueft = 0

    # lumen-texte bringt die Übersetzungen aus lumen-core und die Wahl der
    # Sprache mit; dessen eigene Verweise gehen gegen dessen eigenes res.
    for modul in ("app", "tv", "lumen-texte"):
        res = ANDROID / modul / "src/main/res"
        if not res.is_dir():
            continue

        # Was es gibt: Dateinamen ohne Endung, plus alles, was in den
        # values-XML-Dateien definiert wird.
        vorhanden: dict[str, set[str]] = {}
        for ordner in res.iterdir():
            if not ordner.is_dir():
                continue
            typ = ordner.name.split("-")[0]       # drawable-xhdpi -> drawable
            for datei in ordner.iterdir():
                if datei.is_file():
                    vorhanden.setdefault(typ, set()).add(datei.stem)

        for datei in res.rglob("values*/*.xml"):
            text = datei.read_text(encoding="utf-8")
            for m in re.finditer(r'<(string|plurals|color|style|dimen|bool|integer)\s+name="([^"]+)"', text):
                vorhanden.setdefault(m.group(1), set()).add(m.group(2))

        # Was benutzt wird: aus Kotlin und aus dem Manifest.
        quellen = list((ANDROID / modul / "src/main/kotlin").rglob("*.kt"))
        manifest = ANDROID / modul / "src/main/AndroidManifest.xml"
        benutzt: list[tuple[str, str, pathlib.Path, int]] = []

        for q in quellen:
            for nr, zeile in enumerate(q.read_text(encoding="utf-8").splitlines(), 1):
                # Nur das eigene R: android.R und Bibliotheken
                # (androidx.media3.ui.R …) bringen ihre Ressourcen selbst mit.
                for m in re.finditer(r'(?<![\w.])R\.(\w+)\.(\w+)', ohne_text(zeile)):
                    benutzt.append((m.group(1), m.group(2), q, nr))

        if manifest.exists():
            for nr, zeile in enumerate(manifest.read_text(encoding="utf-8").splitlines(), 1):
                for m in re.finditer(r'"@(?:android:)?(\w+)/(\w+)"', zeile):
                    if "android:" in m.group(0):
                        continue
                    benutzt.append((m.group(1), m.group(2), manifest, nr))
        # Auch die XML-Dateien untereinander: ein adaptives Symbol zeigt
        # auf Vordergrund und Hintergrund.
        for q in res.rglob("*.xml"):
            if "/values" in str(q):
                continue
            for nr, zeile in enumerate(q.read_text(encoding="utf-8").splitlines(), 1):
                for m in re.finditer(r'"@(?:android:)?(\w+)/(\w+)"', zeile):
                    if "android:" in m.group(0):
                        continue
                    benutzt.append((m.group(1), m.group(2), q, nr))

        for typ, name, wo, nr in benutzt:
            geprueft += 1
            # mipmap und drawable duerfen sich gegenseitig vertreten.
            moeglich = {typ}
            if typ in ("mipmap", "drawable"):
                moeglich |= {"mipmap", "drawable"}
            if not any(name in vorhanden.get(t, set()) for t in moeglich):
                fehler.append(f"{wo.relative_to(WURZEL)}:{nr}: {typ}/{name} gibt es nicht")

    print(f"  {geprueft} Android-Ressourcenverweise nachgeschlagen")
    return fehler


def bauskripte_pruefen() -> list[str]:
    """Dateien, bei denen die erste Zeile etwas bedeutet.

    Package.swift ist der Fall, der uns erwischt hat: SwiftPM liest die
    Zeile `// swift-tools-version:` bevor es irgendetwas anderes anschaut.
    Steht sie nicht ganz oben, gilt das Paket als uralt, `platforms:` als
    unbekannt, und das Paket laesst sich gar nicht erst lesen. Die
    Lizenzkopfzeile davor hatte genau das bewirkt — und so etwas sieht
    man beim Lesen nicht, weil die Datei voellig in Ordnung aussieht.
    """
    fehler = []
    geprueft = 0

    for datei in APPLE.rglob("Package.swift"):
        geprueft += 1
        erste = datei.read_text(encoding="utf-8").splitlines()[:1]
        if not erste or not erste[0].startswith("// swift-tools-version:"):
            fehler.append(
                f"{datei.relative_to(WURZEL)}:1: die erste Zeile muss "
                f"'// swift-tools-version:' sein, sie lautet aber "
                f"{erste[0][:40]!r}" if erste else
                f"{datei.relative_to(WURZEL)}: die Datei ist leer")

    print(f"  {geprueft} Bauskript(e) auf die erste Zeile geprueft")
    return fehler


def xml_pruefen() -> list[str]:
    """Jede XML-Datei muss sich lesen lassen.

    Klingt selbstverstaendlich, ist es nicht: ein Kommentar zwischen zwei
    Attributen sieht beim Lesen voellig harmlos aus und ist trotzdem
    ungueltig. Genau das ist beim Begruenden einer Lint-Ausnahme passiert.
    Gradle haette es gemeldet — aber erst auf einer Maschine mit
    Android-SDK, also fruehestens eine Viertelstunde spaeter.
    """
    fehler = []
    geprueft = 0

    for datei in sorted(ANDROID.rglob("*.xml")):
        if "/build/" in str(datei):
            continue
        geprueft += 1
        try:
            ET.parse(datei)
        except ET.ParseError as e:
            fehler.append(f"{datei.relative_to(WURZEL)}: kein gueltiges XML — {e}")

    print(f"  {geprueft} XML-Dateien gelesen")
    return fehler


def apple_kataloge() -> list[str]:
    """Jede Datei, die eine Contents.json nennt, muss danebenliegen.

    Die Kataloge sind von Hand geschrieben. Ein Tippfehler darin faellt
    sonst erst auf, wenn Xcode das Symbol nicht findet und die App ohne
    eines im Laden landet.
    """
    fehler = []
    geprueft = 0

    for inhalt in APPLE.rglob("Contents.json"):
        try:
            daten = json.loads(inhalt.read_text(encoding="utf-8"))
        except json.JSONDecodeError as e:
            fehler.append(f"{inhalt.relative_to(WURZEL)}: kein gueltiges JSON — {e}")
            continue

        for eintrag in daten.get("images", []) + daten.get("assets", []) + daten.get("layers", []):
            name = eintrag.get("filename")
            if not name:
                continue
            geprueft += 1
            if not (inhalt.parent / name).exists():
                fehler.append(
                    f"{inhalt.relative_to(WURZEL)}: {name} fehlt im Ordner")

    # Umgekehrt: ein Bild, das in keiner Contents.json steht, wird nie
    # eingebaut — stiller als ein Fehler und genauso falsch.
    for bild in APPLE.rglob("*.png"):
        inhalt = bild.parent / "Contents.json"
        if not inhalt.exists():
            fehler.append(f"{bild.relative_to(WURZEL)}: daneben fehlt eine Contents.json")
            continue
        if f'"{bild.name}"' not in inhalt.read_text(encoding="utf-8"):
            fehler.append(f"{bild.relative_to(WURZEL)}: in der Contents.json nicht genannt")

    print(f"  {geprueft} Eintraege in Apple-Katalogen nachgeschlagen")
    return fehler


if __name__ == "__main__":
    sys.exit(main())
