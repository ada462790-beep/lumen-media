# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
#
# Veröffentlicht unter der GNU Affero General Public License v3.0.
# Der vollständige Text steht in der Datei LICENSE.
# SPDX-License-Identifier: AGPL-3.0-only

#!/usr/bin/env python3
"""Hält die Übersetzungen der Web-App vollständig.

    python3 tools/sprache_pruefen.py            # prüfen, Rückgabe 0/1
    python3 tools/sprache_pruefen.py --fehlend  # fehlende en-US-Schlüssel als JSON
    python3 tools/sprache_pruefen.py --eszett   # Vorschlag für de-DE (ss → ß)

Geprüft wird:

  a) Jeder Schlüssel aus tr('…') und texte({…}) steht im englischen
     Katalog; kein Katalog enthält Schlüssel, die nirgends mehr benutzt
     werden; Platzhalter ({name}) stimmen zwischen Schlüssel und
     Übersetzung überein; Mehrzahl-Einträge haben „one" und „other".
  b) Kein deutscher Text steht mehr fest im Code: Literale mit Umlauten
     oder typisch deutschen Wörtern ausserhalb von tr(…). Was bewusst
     fest bleibt, trägt in derselben Zeile den Kommentar „sprache: fest".
     Auch einzelne Wörter neben einem Wert („{n} ungelesen", „ab {n}")
     und in sichtbaren Attributen (aria-label, title) zählen.
  d) Der Server schickt in ok()/writeJSON() keine festen deutschen Sätze;
     dafür gibt es tr(w, …) und den Katalog in server/internal/sprache.
  c) Jedes Wort, das in Deutschland mit ß geschrieben wird („Strasse",
     „gross", „schliessen"), hat einen Eintrag in de-DE – und dort steht
     es nicht mehr mit ss. „dass", „muss", „Fluss" bleiben, wie sie sind.

Warum Schlüssel = deutscher Text: siehe server/cmd/lumen/web/sprache.js.
"""

from __future__ import annotations

import json
import pathlib
import re
import sys

WURZEL = pathlib.Path(__file__).resolve().parent.parent
WEB = WURZEL / "server/cmd/lumen/web"
KATALOGE = WEB / "sprachen"
SPRACHEN = ["de-CH", "de-DE", "de-AT", "en-US", "en-GB"]
# Wer von wem erbt (wie in sprache.js). en-US muss vollständig sein.
ERBT = {"de-DE": "de-CH", "de-AT": "de-DE", "en-GB": "en-US"}

ROT, GRUEN, GELB, AUS = "\033[31m", "\033[32m", "\033[33m", "\033[0m"

# Wortstämme, die in Deutschland und Österreich mit ß stehen. Bewusst nur
# sichere Stämme – „Masse" (Menge) und „Maße" etwa lassen sich ohne
# Zusammenhang nicht unterscheiden und stehen darum nicht hier.
ESZETT = [
    ("gross", "groß"), ("Gross", "Groß"), ("gröss", "größ"), ("Gröss", "Größ"),
    ("liess", "ließ"), ("heiss", "heiß"), ("Heiss", "Heiß"), ("weiss", "weiß"), ("Weiss", "Weiß"),
    ("strasse", "straße"), ("Strasse", "Straße"), ("schliess", "schließ"), ("Schliess", "Schließ"),
    ("fuss", "fuß"), ("Fuss", "Fuß"), ("ausser", "außer"), ("Ausser", "Außer"),
    ("äusser", "äußer"), ("Äusser", "Äußer"), ("aussen", "außen"), ("Aussen", "Außen"),
    ("draussen", "draußen"), ("bloss", "bloß"), ("gemäss", "gemäß"), ("mässig", "mäßig"),
    ("Massnahme", "Maßnahme"), ("massgeb", "maßgeb"), ("spass", "spaß"), ("Spass", "Spaß"),
    ("grüss", "grüß"), ("Grüss", "Grüß"), ("gruss", "gruß"), ("Gruss", "Gruß"),
    ("stoss", "stoß"), ("Stoss", "Stoß"), ("fliess", "fließ"), ("Fliess", "Fließ"),
    ("süss", "süß"), ("reiss", "reiß"), ("Reiss", "Reiß"), ("geniess", "genieß"),
    ("beiss", "beiß"), ("giess", "gieß"), ("schiess", "schieß"), ("Schiess", "Schieß"),
]
ESZETT_MUSTER = re.compile("|".join(re.escape(a) for a, _ in ESZETT))

UMLAUT = re.compile(r"[äöüÄÖÜß„“]")
DEUTSCH = set("""und oder der die das den dem des ist sind nicht kein keine keinen mit für von zu zum zur
ein eine einen einem noch nur schon wird werden bitte auf im in an du dein deine dich dir sich es hier gibt
alle alles bei aus nach vor über unter wie was wer wo wenn dann auch mehr neu neue jetzt gerade erst ab bis
seit ohne sein soll kann muss wurde hat haben geht zurück weiter ja nein gesehen gemerkt gespeichert läuft
fertig gelöscht zuletzt heute gestern Folgen Folge Titel Staffel Minuten Sekunden Abbrechen Speichern
Schliessen Löschen Zurück Weiter Fehler Einstellungen Verwaltung""".split())
# Einzelne Wörter, die neben einem Wert stehen („{n} ungelesen", „ab {n}",
# „{titel} abspielen") – allein sind sie zu kurz für die Regel oben, aber
# in einem Template-String oder einem sichtbaren Attribut verraten sie
# einen deutschen Rest.
EINZELN = set("""ab von bis gesehen ungelesen abspielen festlegen gemischt faul Minuten Minute Sekunden Stunden
Std Min Titel Folgen Folge Staffel Fassungen Fassung Teil Uhr heute gestern morgen neu Neu Alle alle umgewandelt
gelesen Treffer Fotos Foto Alben Interpreten Lieder Listen Liste Bilder Personen Geräte Gerät Bibliotheken
Dateien Datei Sammlungen Filme Film Serien Serie läuft weiter Weiter zurück Zurück hochladen herunterladen
bearbeiten löschen entfernen öffnen schliessen anzeigen ansehen merken gemerkt hinzufügen umbenennen wählen""".split())
# Wörter, die in jeder der fünf Sprachen gleich bleiben dürfen.
NAMEN = {"Lumen", "IMDb", "TMDb", "OMDb", "Emby", "Jellyfin", "OpenSubtitles", "FFmpeg", "GitHub",
         "Chromecast", "AirPlay", "Apple TV", "Android", "Android TV", "Fire TV", "Google TV", "Docker",
         "NVIDIA NVENC", "Intel Quick Sync", "VAAPI (Intel, AMD)", "Rotten Tomatoes", "Mac", "Tablet",
         "PIN", "Info", "Start", "Stereo", "Specials", "Normal", "Genres", "Album", "Alben", "Interpret"}


# --------------------------------------------------------------- Lesen

def literale(src: str):
    """Liefert (zeile, art, text, vorher) für jedes String- und
    Template-Literal; art ist "'" / '"' / "`". Bei Templates ist text der
    statische Teil, Ausdrücke als \0. Kommentare und Regex fallen weg."""
    i, n = 0, len(src)
    tail = ""  # letzte bedeutsame Zeichen ausserhalb von Literalen

    def regex_erlaubt(t: str) -> bool:
        t = t.rstrip()
        return (not t or t[-1] in "(,=:[!&|?{};+-*%<>~^"
                or re.search(r"\b(return|typeof|case|in|of|else|void|throw)$", t) is not None)

    def scan_string(j):
        q = src[j]
        j += 1
        while j < n and src[j] != q and src[j] != "\n":
            j += 2 if src[j] == "\\" else 1
        return j + 1

    def scan_regex(j):
        j += 1
        klasse = False
        while j < n and src[j] != "\n":
            c = src[j]
            if c == "\\":
                j += 2
                continue
            if c == "[":
                klasse = True
            elif c == "]":
                klasse = False
            elif c == "/" and not klasse:
                j += 1
                while j < n and src[j].isalpha():
                    j += 1
                return j
            j += 1
        return j

    out = []

    def scan_template(j, vorher):
        start = j
        j += 1
        text = ""
        while j < n:
            c = src[j]
            if c == "\\":
                text += src[j:j + 2]
                j += 2
                continue
            if c == "`":
                out.append((src.count("\n", 0, start) + 1, "`", text, vorher))
                return j + 1
            if c == "$" and src[j + 1:j + 2] == "{":
                text += "\0"
                j = scan_code(j + 2, bis_klammer=True) + 1
                continue
            text += c
            j += 1
        return j

    def scan_code(j, bis_klammer=False):
        nonlocal tail
        tiefe = 0
        lokal = ""
        while j < n:
            c = src[j]
            if src.startswith("//", j):
                j = src.find("\n", j)
                j = n if j < 0 else j
                continue
            if src.startswith("/*", j):
                j = src.index("*/", j) + 2
                continue
            if c in "'\"":
                e = scan_string(j)
                out.append((src.count("\n", 0, j) + 1, c, src[j + 1:e - 1], lokal[-40:]))
                lokal += "s"
                j = e
                continue
            if c == "`":
                j = scan_template(j, lokal[-40:])
                lokal += "s"
                continue
            if c == "/" and regex_erlaubt(lokal[-40:]):
                j = scan_regex(j)
                lokal += "r"
                continue
            if bis_klammer:
                if c == "{":
                    tiefe += 1
                elif c == "}":
                    if tiefe == 0:
                        return j
                    tiefe -= 1
            lokal += c
            j += 1
        return j

    scan_code(0)
    return out


def ist_rest(s: str) -> bool:
    """Ein Lauf aus einem Template-String oder sichtbaren Attribut, der
    neben einem Wert steht: schon ein einziges deutsches Wort zählt."""
    s = re.sub(r"\{\w+\}", "", s).replace("\0", " ")
    s = re.sub(r"<[^>]*>", " ", s)
    # Klassennamen („pl-knopf--an", „haken--an") sind kein Text.
    s = re.sub(r"(?<![\wÄÖÜäöüß])[a-zäöü][\wäöü]*(?:-+[\wäöü]+)+", " ", s)
    if re.search(r'[;{}\[\]]|="|^\s*[-\w]+:', s) or (re.search(r"[=&?/]", s) and " " not in s.strip()):
        return False
    return any(w in EINZELN or w in DEUTSCH for w in re.findall(r"[A-Za-zÄÖÜäöüß]+", s))


def ist_text(s: str) -> bool:
    s = re.sub(r"\{\w+\}", "", s).replace("\0", " ").strip()
    s = re.sub(r"<[^>]*>", " ", s)
    if s in NAMEN or not re.search(r"[A-Za-zÄÖÜäöüß]{3,}", s):
        return False
    if re.fullmatch(r"[\w.\-/:#?=&%,+*()\[\]|$@ ]*", s) and not UMLAUT.search(s) and " " not in s.strip():
        return False
    # CSS, Selektoren und Attribute sind kein Text.
    if re.search(r'[;{}\[\]]|="', s) and not UMLAUT.search(s):
        return False
    woerter = re.findall(r"[A-Za-zÄÖÜäöüß]+", s)
    if UMLAUT.search(s) and not re.search(r"[;{}]", s):
        return True
    return any(w in DEUTSCH for w in woerter) and " " in s


# Attribute, deren Wert man sieht oder hört.
ATTRIBUTE = ("aria-label", "title", "placeholder", "alt", "aria-description", "aria-roledescription",
             "aria-valuetext", "label", "data-laeuft")


def textlaeufe(text: str) -> list[str]:
    """Zerlegt ein Literal in das, was man sieht: Text zwischen Tags und
    die Werte sichtbarer Attribute. Ohne Tags ist es ein einziger Lauf.
    Ausdrücke (\0) trennen Läufe."""
    if not re.search(r"<[A-Za-z/!]|^[^<]*>|=\"", text):
        return text.split("\0")
    out, lauf, i, n = [], "", 0, len(text)
    in_tag = text.lstrip().startswith(("\"", ">")) is False and False
    # Beginnt das Literal mitten in einem Tag (z. B. '">…' nach einem
    # Ausdruck), steht vor dem ersten '>' noch Tag-Inhalt.
    erstes_lt, erstes_gt = text.find("<"), text.find(">")
    if erstes_gt >= 0 and (erstes_lt < 0 or erstes_gt < erstes_lt):
        in_tag = True
    while i < n:
        c = text[i]
        if in_tag:
            m = re.match(r"\s(" + "|".join(map(re.escape, ATTRIBUTE)) + r')="([^"]*)"', text[i:])
            if m:
                out.extend(m.group(2).split("\0"))
                i += m.end()
                continue
            if c == ">":
                in_tag = False
            i += 1
            continue
        if c == "<" and i + 1 < n and re.match(r"[A-Za-z/!]", text[i + 1]):
            out.append(lauf)
            lauf = ""
            in_tag = True
            i += 1
            continue
        if c == "\0":
            out.append(lauf)
            lauf = ""
        else:
            lauf += c
        i += 1
    out.append(lauf)
    return out


def schluessel_aus_code():
    """Alle Schlüssel: tr('…') und Werte in texte({ … })."""
    benutzt: dict[str, str] = {}
    verdaechtig = []
    for datei in sorted(WEB.glob("*.js")):
        if datei.name == "sprache.js":
            continue
        src = datei.read_text(encoding="utf-8")
        zeilen = src.split("\n")
        # texte({ … }): Bereiche merken
        texte_bereiche = []
        for m in re.finditer(r"texte\(\{", src):
            tiefe, j = 0, m.end() - 1
            while j < len(src):
                if src[j] == "{":
                    tiefe += 1
                elif src[j] == "}":
                    tiefe -= 1
                    if tiefe == 0:
                        break
                j += 1
            texte_bereiche.append((src.count("\n", 0, m.start()) + 1, src.count("\n", 0, j) + 1))
        for zeile, art, text, vorher in literale(src):
            if art != "`":
                text = text.replace("\\'", "'").replace('\\"', '"')
            im_tr = re.search(r"\btr\(\s*$", vorher) is not None
            im_texte = any(a <= zeile <= b for a, b in texte_bereiche) and re.search(r":\s*$", vorher)
            if im_tr or im_texte:
                if "\0" in text:
                    verdaechtig.append((datei.name, zeile, "tr() mit ${…} im Schlüssel", text))
                    continue
                benutzt.setdefault(text, f"{datei.name}:{zeile}")
                continue
            if "sprache: fest" in zeilen[zeile - 1]:
                continue
            laeufe = textlaeufe(text)
            for t in laeufe:
                if ist_text(t):
                    verdaechtig.append((datei.name, zeile, "fester Text", t.strip()[:90]))
                    break
            else:
                # Im Template neben einem Wert oder in einem Attribut, das
                # man sieht oder hört: schon ein Wort ist zu viel.
                # 'von ' + name: ein Wort mit Leerzeichen am Rand wird angehängt.
                if (art == "`" and "\0" in text) or len(laeufe) > 1 or (text.strip() and text != text.strip()):
                    for t in laeufe:
                        if ist_rest(t):
                            verdaechtig.append((datei.name, zeile, "deutsches Wort neben einem Wert", t.strip()[:90]))
                            break
    roh = (WEB / "index.html").read_text(encoding="utf-8")
    html = re.sub(r"<!--.*?-->", "", roh, flags=re.S)
    for t in re.findall(r">([^<]+)<", html):
        if ist_text(t) and "sprache: fest" not in roh:
            verdaechtig.append(("index.html", 0, "fester Text", t.strip()[:90]))
    return benutzt, verdaechtig


def katalog(id_: str) -> dict:
    p = KATALOGE / f"{id_}.json"
    return json.loads(p.read_text(encoding="utf-8")) if p.exists() else {}


def platzhalter(s) -> set[str]:
    if isinstance(s, dict):
        return set().union(*(platzhalter(v) for v in s.values()))
    return set(re.findall(r"\{(\w+)\}", s))


def ohne_zusammenhang(k: str) -> str:
    """„Musik|{n} Titel" → „{n} Titel" (siehe sprache.js)."""
    return re.sub(r"^[\w-]+\|", "", k)


def eszett(text: str) -> str:
    for a, b in ESZETT:
        text = text.replace(a, b)
    return text


# --------------------------------------------------------------- Prüfen

def main() -> int:
    benutzt, verdaechtig = schluessel_aus_code()
    kat = {s: katalog(s) for s in SPRACHEN}

    if "--fehlend" in sys.argv:
        fehlend = {k: "" for k in sorted(benutzt) if k not in kat["en-US"]}
        print(json.dumps(fehlend, ensure_ascii=False, indent=2))
        return 0
    if "--eszett" in sys.argv:
        vorschlag = {}
        for k in sorted(benutzt):
            quelle = kat["de-CH"].get(k, ohne_zusammenhang(k))
            if isinstance(quelle, dict):
                neu = {f: eszett(v) for f, v in quelle.items()}
            else:
                neu = eszett(quelle)
            if neu != quelle:
                vorschlag[k] = neu
        print(json.dumps(vorschlag, ensure_ascii=False, indent=2))
        return 0

    fehler = 0

    def melde(text):
        nonlocal fehler
        fehler += 1
        print(f"  {ROT}✗{AUS} {text}")

    print(f"{len(benutzt)} Schlüssel in der Web-App")

    # a) Vollständig, keine Waisen, Platzhalter
    for k in sorted(benutzt):
        if k not in kat["en-US"]:
            melde(f"en-US fehlt: {k!r} ({benutzt[k]})")
    for s in SPRACHEN:
        for k, v in kat[s].items():
            if k not in benutzt:
                melde(f"{s}: überzählig (nirgends benutzt): {k!r}")
                continue
            if isinstance(v, dict) and not {"one", "other"} <= set(v):
                melde(f"{s}: Mehrzahl ohne one/other: {k!r}")
            if platzhalter(v) - platzhalter(k):
                melde(f"{s}: unbekannter Platzhalter in {k!r}: {sorted(platzhalter(v) - platzhalter(k))}")
            if platzhalter(k) - platzhalter(v) - {"n"} and not isinstance(v, dict):
                melde(f"{s}: Platzhalter fehlt in der Übersetzung von {k!r}: {sorted(platzhalter(k) - platzhalter(v))}")

    # b) Feste Texte
    for datei, zeile, art, text in verdaechtig:
        melde(f"{datei}:{zeile}: {art}: {text!r}")

    # d) Server: Texte, die über ok()/writeJSON() an ein Gerät gehen,
    # laufen über tr(w, …) – ein fester deutscher Satz in einer Antwort
    # landet sonst wörtlich im Toast einer englischen Oberfläche. Kennungen
    # ohne Leerzeichen („läuft") sind Codes und dürfen bleiben.
    for datei in sorted((WURZEL / "server/internal/api").glob("*.go")):
        if datei.name.endswith("_test.go"):
            continue
        for n, zeile in enumerate(datei.read_text(encoding="utf-8").splitlines(), 1):
            if not re.search(r"\b(ok|writeJSON)\(w,", zeile) or "sprache: fest" in zeile:
                continue
            for wert in re.findall(r'"\w+":\s*"([^"]*)"', zeile):
                if " " in wert.strip() and re.search(r"[A-Za-zÄÖÜäöüß]{3,}", wert):
                    melde(f"server/internal/api/{datei.name}:{n}: fester Text in einer Antwort: {wert!r} – tr(w, …)")

    # c) ß für Deutschland und Österreich
    for k in sorted(benutzt):
        quelle = kat["de-CH"].get(k, ohne_zusammenhang(k))
        texte = quelle.values() if isinstance(quelle, dict) else [quelle]
        if any(ESZETT_MUSTER.search(t) for t in texte):
            if k not in kat["de-DE"]:
                melde(f"de-DE fehlt (ß): {k!r}")
    for s in ("de-DE", "de-AT"):
        for k, v in kat[s].items():
            for t in (v.values() if isinstance(v, dict) else [v]):
                if ESZETT_MUSTER.search(t):
                    melde(f"{s}: noch mit ss: {t!r}")

    if fehler:
        print(f"\n{ROT}✗ {fehler} Fund(e).{AUS}")
        return 1
    print(f"{GRUEN}✓ Alle Texte übersetzt, nichts fest verdrahtet.{AUS}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
