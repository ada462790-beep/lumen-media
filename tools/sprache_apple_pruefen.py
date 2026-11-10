#!/usr/bin/env python3
# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
#
# Veröffentlicht unter der GNU Affero General Public License v3.0.
# Der vollständige Text steht in der Datei LICENSE.
# SPDX-License-Identifier: AGPL-3.0-only

"""Hält die Texte der Apple-Apps und ihre String-Kataloge zusammen.

Xcode zieht die Texte beim Bauen selbst aus dem Code — aber nur auf einem
Mac, und ein Text, der still Deutsch bleibt, fällt erst auf, wenn jemand
die App auf Englisch öffnet. Dieses Skript prüft ohne Übersetzer:

  1. Jeder Text, den Swift übersetzen lässt (Text("…"), Button("…"),
     String(localized: …) …), steht im passenden Katalog —
     apple/Sprache/Localizable.xcstrings für die Apps,
     apple/LumenKit/Sources/LumenKit/Resources/Localizable.xcstrings
     für LumenKit — und ist in allen fünf Sprachen übersetzt.
  2. Kein deutscher Satz steht fest verdrahtet ausserhalb davon (etwa
     als gewöhnlicher String, den Text(…) dann wörtlich zeigt).
  3. Die Kataloge sind gültiges JSON, und Platzhalter wie %@ und %lld
     stimmen in jeder Übersetzung mit dem Schlüssel überein.

    python3 tools/sprache_apple_pruefen.py           # prüfen
    python3 tools/sprache_apple_pruefen.py --liste   # alle Schlüssel zeigen

Rückgabe 0, wenn alles passt, sonst 1.

Die Schlüssel bildet das Skript so, wie Xcode es tut: aus "\\(n) Filme"
wird "%lld Filme", wenn n eine ganze Zahl ist, sonst "%@ Filme". Den Typ
rät es aus dem Code (Deklaration, .count …) — liegt es daneben, meldet es
den Schlüssel als fehlend, und Xcode legt ihn beim nächsten Bauen an.
"""

from __future__ import annotations

import json
import pathlib
import re
import sys

WURZEL = pathlib.Path(__file__).resolve().parent.parent
APPLE = WURZEL / "apple"

SPRACHEN = ["de-CH", "de", "de-AT", "en", "en-GB"]
QUELLE = "de-CH"
# Im Katalog steht „en" als Quellsprache: Xcode legt für die Quellsprache
# keinen eigenen Ordner mit Mehrzahlregeln an, Schweizer Deutsch braucht ihn
# aber („1 Film", nicht „1 Filme"). Die Schlüssel bleiben Schweizer Text.
KATALOG_QUELLE = "en"

# Wo die Texte liegen und in welchen Katalog sie gehören.
BEREICHE = {
    "App": {
        "ordner": [APPLE / "Lumen", APPLE / "LumenTV"],
        "katalog": APPLE / "Sprache/Localizable.xcstrings",
    },
    "LumenKit": {
        "ordner": [APPLE / "LumenKit/Sources/LumenKit"],
        "katalog": APPLE / "LumenKit/Sources/LumenKit/Resources/Localizable.xcstrings",
    },
}

GRUEN, ROT, GELB, AUS = "\033[32m", "\033[31m", "\033[33m", "\033[0m"

# Aufrufe, deren erstes (unbenanntes) Argument SwiftUI als
# LocalizedStringKey nimmt, wenn dort ein Literal steht.
SCHLUESSEL_AUFRUFE = {
    "Text", "Label", "Button", "Toggle", "Picker", "Section", "TextField",
    "SecureField", "Stepper", "Menu", "Link", "NavigationLink", "DatePicker",
    "ProgressView", "Tab", "ShareLink", "GroupBox", "DisclosureGroup",
    "CommandMenu", "WindowGroup", "Window", "ContentUnavailableView",
    "LabeledContent", "ColorPicker", "ControlGroup", "LocalizedStringKey",
    "Settings", "PhotosPicker", "Gauge",
    ".navigationTitle", ".navigationSubtitle", ".help", ".accessibilityLabel",
    ".accessibilityHint", ".accessibilityValue", ".alert", ".confirmationDialog",
    ".badge", ".fileImporter", ".accessibilityAction",
}
# Benannte Argumente, die ebenfalls übersetzt werden.
SCHLUESSEL_LABELS = {"prompt", "titleKey", "named", "localized"}
# Aufrufe mit String.LocalizationValue: String(localized: …).
WERT_AUFRUFE = {"String", "AttributedString", "LocalizedStringResource"}
# Eigene Hilfen, die einen String.LocalizationValue oder LocalizedStringKey
# nehmen (Name → Argument-Label oder None fürs erste).
EIGENE = {
    "Leer": None, "Hinweiszeile": None, "Leerzustand": None,
}

# Wo ein Literal nie ein sichtbarer Text ist.
TECHNISCHE_LABELS = {
    "systemImage", "systemName", "forKey", "forHTTPHeaderField", "id",
    "named", "withExtension", "forResource", "ofType", "identifier",
    "subsystem", "category", "key", "value_technisch", "scheme", "path",
    "contentType", "type", "kind", "image", "symbol", "suite",
}


# ------------------------------------------------------------- Swift lesen

class Literal:
    """Ein Zeichenketten-Literal samt Zusammenhang."""

    def __init__(self, datei, zeile, teile, roh, kontext):
        self.datei = datei
        self.zeile = zeile
        self.teile = teile          # [("text", "…") | ("ausdruck", "…")]
        self.roh = roh
        self.kontext = kontext      # "schluessel" | "wert" | "frei"

    @property
    def text(self) -> str:
        """Der Text mit den Ausdrücken in \\( ), wie er im Code steht."""
        return "".join(t if art == "text" else f"\\({t})" for art, t in self.teile)

    @property
    def hat_ausdruck(self) -> bool:
        return any(art == "ausdruck" for art, _ in self.teile)


ESCAPES = {"n": "\n", "t": "\t", "\"": "\"", "\\": "\\", "'": "'", "0": "\0", "r": "\r"}


def literal_lesen(q: str, i: int):
    """Liest ein "…"-Literal ab q[i] == '"'. Gibt (teile, ende) zurück."""
    assert q[i] == '"'
    i += 1
    teile = []
    puffer = []
    while i < len(q):
        c = q[i]
        if c == '"':
            if puffer:
                teile.append(("text", "".join(puffer)))
            return teile, i + 1
        if c == "\n":
            break
        if c == "\\":
            n = q[i + 1]
            if n == "(":
                if puffer:
                    teile.append(("text", "".join(puffer)))
                    puffer = []
                # Ausdruck bis zur passenden Klammer, Literale darin überspringen.
                tiefe = 1
                j = i + 2
                while j < len(q) and tiefe:
                    if q[j] == '"':
                        _, j = literal_lesen(q, j)
                        continue
                    if q[j] == "(":
                        tiefe += 1
                    elif q[j] == ")":
                        tiefe -= 1
                    j += 1
                teile.append(("ausdruck", q[i + 2:j - 1].strip()))
                i = j
                continue
            if n == "u":
                m = re.match(r"\\u\{([0-9a-fA-F]+)\}", q[i:])
                if m:
                    puffer.append(chr(int(m.group(1), 16)))
                    i += len(m.group(0))
                    continue
            puffer.append(ESCAPES.get(n, n))
            i += 2
            continue
        puffer.append(c)
        i += 1
    if puffer:
        teile.append(("text", "".join(puffer)))
    return teile, i


def literale(datei: pathlib.Path) -> list[Literal]:
    """Alle Literale einer Datei mit ihrem Zusammenhang.

    Ein kleiner Zustandsautomat statt eines echten Parsers: er kennt
    Kommentare, Rohtext (#"…"#), Klammern samt aufgerufenem Namen und das
    Argument-Label davor. Das genügt, um zu sagen, ob ein Literal als
    LocalizedStringKey oder String.LocalizationValue gemeint ist.
    """
    q = datei.read_text(encoding="utf-8")
    ergebnis = []
    # Stapel: (zeichen, aufruf, label, bereichsart)
    stapel = [("{", None, None, None, 0)]
    # Rümpfe, die LocalizedStringKey liefern: var x: LocalizedStringKey {
    i = 0
    zeile = 1
    arg_start = True
    label = None
    letzte_woerter = []
    n = len(q)

    def wort_vor(pos):
        m = re.search(r"(\.?[A-Za-z_][A-Za-z0-9_]*)\s*$", q[:pos])
        return m.group(1) if m else None

    while i < n:
        c = q[i]
        if c == "\n":
            zeile += 1
            i += 1
            continue
        if q.startswith("//", i):
            j = q.find("\n", i)
            i = n if j < 0 else j
            continue
        if q.startswith("/*", i):
            j = q.find("*/", i)
            zeile += q[i:j].count("\n")
            i = j + 2
            continue
        if c == "#" and re.match(r'#+"', q[i:]):
            m = re.match(r'(#+)"', q[i:])
            zu = '"' + m.group(1)
            j = q.find(zu, i + len(m.group(0)))
            zeile += q[i:j].count("\n")
            i = j + len(zu)
            continue
        if c == "'":
            i += 1
            continue
        if c == '"':
            teile, ende = literal_lesen(q, i)
            roh = q[i:ende]
            kontext = kontext_bestimmen(q, i, stapel)
            ergebnis.append(Literal(datei, zeile, teile, roh, kontext))
            i = ende
            continue
        if c in "([{":
            aufruf = None
            art = None
            if c == "(":
                aufruf = wort_vor(i)
            if c == "{":
                kopf = q[max(0, q.rfind("\n", 0, i - 200 if i > 200 else 0)):i]
                kopf = q[q.rfind("\n", 0, i) + 1:i]
                if re.search(r"(->|:)\s*LocalizedStringKey\s*$", kopf.strip()) or \
                        re.search(r"(->|:)\s*LocalizedStringKey\s*\{?$", kopf.strip()):
                    art = "schluessel"
                elif re.search(r"(->|:)\s*String\.LocalizationValue\s*$", kopf.strip()):
                    art = "wert"
                elif re.match(r"(\}\s*)?(switch|if|else|guard|case|default)\b", kopf.strip()) \
                        or kopf.strip() in ("", "}"):
                    # switch, if, else gehören zum Rumpf darum herum: ein
                    # `var name: LocalizedStringKey { switch self { … } }`
                    # liefert auch aus dem switch heraus Schlüssel.
                    art = stapel[-1][3] if stapel[-1][0] == "{" else None
            stapel.append((c, aufruf, label_vor(q, i), art, i))
            i += 1
            continue
        if c in ")]}":
            if len(stapel) > 1:
                stapel.pop()
            i += 1
            continue
        i += 1
    return ergebnis


def label_vor(q: str, pos: int):
    """Das Argument-Label unmittelbar vor pos, etwa `prompt:` oder None."""
    davor = q[:pos]
    # Zurück bis zum Anfang des Arguments: ( , oder Zeilenanfang einer Liste
    tiefe = 0
    k = len(davor) - 1
    while k >= 0:
        ch = davor[k]
        if ch in ")]}":
            tiefe += 1
        elif ch in "([{":
            if tiefe == 0:
                break
            tiefe -= 1
        elif ch == "," and tiefe == 0:
            break
        k -= 1
    stueck = davor[k + 1:]
    m = re.match(r"\s*([A-Za-z_][A-Za-z0-9_]*)\s*:(?!:)", stueck)
    return m.group(1) if m else None


def argument_index(q: str, start: int, pos: int) -> int:
    """Wie viele Kommas auf oberster Ebene zwischen start und pos liegen."""
    tiefe = 0
    anzahl = 0
    k = start + 1
    while k < pos:
        ch = q[k]
        if ch == '"':
            _, k = literal_lesen(q, k)
            continue
        if ch in "([{":
            tiefe += 1
        elif ch in ")]}":
            tiefe -= 1
        elif ch == "," and tiefe == 0:
            anzahl += 1
        k += 1
    return anzahl


def argument_text(q: str, start: int, pos: int) -> str:
    """Das ganze Argument, in dem pos steht (ohne Label)."""
    # Anfang: letztes Komma auf oberster Ebene vor pos, sonst start.
    tiefe = 0
    anfang = start + 1
    k = start + 1
    while k < pos:
        ch = q[k]
        if ch == '"':
            _, k = literal_lesen(q, k)
            continue
        if ch in "([{":
            tiefe += 1
        elif ch in ")]}":
            tiefe -= 1
        elif ch == "," and tiefe == 0:
            anfang = k + 1
        k += 1
    # Ende: nächstes Komma oder schliessende Klammer auf oberster Ebene.
    tiefe = 0
    k = anfang
    while k < len(q):
        ch = q[k]
        if ch == '"':
            _, k = literal_lesen(q, k)
            continue
        if ch in "([{":
            tiefe += 1
        elif ch in ")]}":
            if tiefe == 0:
                break
            tiefe -= 1
        elif ch == "," and tiefe == 0:
            break
        k += 1
    return q[anfang:k]


def nur_literale(arg: str) -> bool:
    """Ob ein Ausdruck nur aus Literalen besteht — auch über ?: hinweg."""
    stuecke, k = [], 0
    while k < len(arg):
        if arg[k] == '"':
            _, ende = literal_lesen(arg, k)
            stuecke.append('"L"')
            k = ende
            continue
        stuecke.append(arg[k])
        k += 1
    ohne = "".join(stuecke)
    # Klammern um den ganzen Ausdruck weg
    ohne = ohne.strip()
    while ohne.startswith("(") and ohne.endswith(")"):
        ohne = ohne[1:-1].strip()
    # "a" + b ist ein String, kein Schlüssel.
    if "+" in ohne:
        return False
    ohne = ohne.replace("??", "§§")
    if "?" not in ohne:
        if "§§" in ohne:
            return False
        return ohne == '"L"' or not ohne.startswith('"') or ohne.count('"L"') == 0
    # bedingung ? a : b — die Zweige zählen, nicht die Bedingung.
    zweige = ohne.split("?", 1)[1]
    if "§§" in zweige:
        return False
    teile = [t.strip().strip("()").strip() for t in re.split(r"\s:\s|\s:$|:\s", zweige)]
    return all(t == '"L"' or (t.count("?") and nur_literale(t)) for t in teile if t)


def kontext_bestimmen(q, pos, stapel) -> str:
    """Ob das Literal an pos übersetzt wird: schluessel, wert oder frei."""
    # Ein Vergleich ist nie ein Text: art == "movie", case "pin":
    zeilenanfang = q.rfind("\n", 0, pos) + 1
    davor = q[zeilenanfang:pos]
    if re.search(r"(==|!=|\bcase|\bcontains\()\s*$", davor):
        return "frei"
    # Durch blosse Klammern (Ternär, Gruppierung) hindurchschauen.
    k = len(stapel) - 1
    while k > 0 and stapel[k][0] == "(" and stapel[k][1] is None:
        k -= 1
    oben = stapel[k]
    zeichen, aufruf = oben[0], oben[1]
    if zeichen == "{":
        art = oben[3]
        if art:
            # Nur auf oberster Ebene des Rumpfs (return / case …: "…").
            return art
        # let x: LocalizedStringKey = "…"
        zeilenanfang = q.rfind("\n", 0, pos) + 1
        if re.search(r":\s*LocalizedStringKey\s*=\s*$", q[zeilenanfang:pos]):
            return "schluessel"
        if re.search(r":\s*String\.LocalizationValue\s*=\s*$", q[zeilenanfang:pos]):
            return "wert"
        return "frei"
    if zeichen != "(" or aufruf is None:
        return "frei"
    start = oben[4]
    label = label_vor(q, pos)
    index = argument_index(q, start, pos)
    if aufruf in WERT_AUFRUFE:
        if label == "localized":
            return "wert"
        return "frei"
    if aufruf in SCHLUESSEL_AUFRUFE:
        if label is None and index == 0:
            # Text(bedingung ? "läuft" : dauer) ist ein String, kein
            # Schlüssel — SwiftUI zeigt ihn wörtlich. Nur wenn jeder Zweig
            # ein Literal ist, wird übersetzt.
            if not nur_literale(argument_text(q, start, pos)):
                return "frei"
            return "schluessel"
        if label in SCHLUESSEL_LABELS:
            return "schluessel"
        # .alert("…", isPresented:) — Titel an erster Stelle; Text(…, tableName)
        return "frei"
    if aufruf in EIGENE:
        if EIGENE[aufruf] == label and (label is not None or index == 0):
            return "schluessel"
    if label in {"prompt"}:
        return "schluessel"
    return "frei"


# ------------------------------------------------------------- Schlüssel

TYPEN = (("Int64", "%lld"), ("Int32", "%d"), ("Int", "%lld"), ("UInt64", "%llu"),
         ("UInt", "%llu"), ("Double", "%lf"), ("Float", "%f"), ("CGFloat", "%lf"),
         ("TimeInterval", "%lf"), ("String", "%@"), ("Substring", "%@"))

_GLOBAL: dict[str, set[str]] = {}


def global_typen() -> dict[str, set[str]]:
    """Eigenschaften aller Swift-Dateien mit ihrem Typ: name → {%lld, …}."""
    if _GLOBAL:
        return _GLOBAL
    for datei in APPLE.rglob("*.swift"):
        if "Tests" in datei.parts:
            continue
        q = datei.read_text(encoding="utf-8")
        for name, typ in re.findall(r"\b(?:let|var)\s+(\w+)\s*:\s*(\w+)", q):
            for t, k in TYPEN:
                if typ == t:
                    _GLOBAL.setdefault(name, set()).add(k)
    return _GLOBAL


def typ_raten(ausdruck: str, quelle: str, tiefe: int = 0) -> str:
    """%lld, %lf oder %@ — wie Xcode den Platzhalter bildet.

    Xcode kennt den Typ; hier wird er geraten: aus der Deklaration in
    derselben Datei, sonst aus einer gleichnamigen Eigenschaft irgendwo in
    apple/, aus `?? 0`, `x + 1`, `.count`, `Int(…)`. Was sich nicht klären
    lässt, gilt als %@ (Text).
    """
    a = ausdruck.strip()
    if "specifier:" in a:
        m = re.search(r'specifier:\s*"([^"]+)"', a)
        return m.group(1) if m else "%@"
    if "format:" in a or ".formatted(" in a or a.startswith(("Text(", "Image(", "String(")) \
            or a.startswith('"') or ".joined(" in a or ".name(" in a:
        return "%@"
    if re.fullmatch(r"-?\d+", a) or a.endswith(".count") or re.match(r"Int(64|32)?\(", a):
        return "%lld"
    if re.match(r"Double\(", a):
        return "%lf"
    m = re.fullmatch(r"(.+?)\s*\?\?\s*(-?\d+)", a)
    if m:
        return "%lld" if typ_raten(m.group(1), quelle, tiefe + 1) in ("%lld", "%@") else "%@"
    m = re.fullmatch(r"(.+?)\s*[-+*/%]\s*(\d+|[A-Za-z_][\w.]*)", a)
    if m and not '"' in a:
        return typ_raten(m.group(1), quelle, tiefe + 1)
    name = a.split(".")[-1].rstrip("?!")
    if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", name):
        return "%@"
    # 1. Deklaration in derselben Datei
    for typ, kennung in TYPEN:
        if re.search(rf"\b{name}\s*:\s*{typ}\??(?![\w.])", quelle):
            return kennung
    if re.search(rf"\b(let|var)\s+{name}\s*=\s*(-?\d+\b(?!\.)|Int(64|32)?\(|[^\n]*\.count\s*$)", quelle, re.M):
        return "%lld"
    if re.search(rf"\b(let|var)\s+{name}\s*=\s*Double\(", quelle):
        return "%lf"
    # ForEach(1...5, id: \.self) { n in — eine Zahl aus einem Bereich
    if re.search(rf"\d+\s*\.\.[.<]\s*\w+[^{{}}\n]*\{{\s*{name}\s+in\b", quelle):
        return "%lld"
    # 2. let x = …, if let x = … oder „, x = …": dem Ursprung folgen
    if tiefe < 4:
        m = re.search(rf"(?:\b(?:let|var)|,)\s+{name}\s*=\s*([^,{{\n]+?)\s*(?:,|\{{|\belse\b|$)", quelle, re.M)
        if m and m.group(1).strip() != name:
            return typ_raten(m.group(1), quelle, tiefe + 1)
    # 3. gleichnamige Eigenschaft irgendwo in apple/
    arten = global_typen().get(name, set())
    if len(arten) == 1:
        return next(iter(arten))
    return "%@"


def schluessel(lit: Literal) -> str:
    """Der Katalogschlüssel, wie Xcode ihn aus dem Literal bildet."""
    if not lit.hat_ausdruck:
        return "".join(t for _, t in lit.teile)
    quelle = lit.datei.read_text(encoding="utf-8")
    stuecke = []
    for art, t in lit.teile:
        if art == "text":
            stuecke.append(t.replace("%", "%%"))
        else:
            stuecke.append(typ_raten(t, quelle))
    return "".join(stuecke)


# ------------------------------------------------------------- Deutsch?

DEUTSCH = re.compile(r"[äöüÄÖÜß]|\b(der|die|das|und|nicht|ist|ein|eine|noch|"
                     r"mit|für|von|zu|auf|dein|deine|wird|kein|keine|alle|"
                     r"hier|schon|nur|oder|wie)\b")


def sieht_sichtbar_aus(text: str, klein_als_wert: bool = False) -> bool:
    """Grob: ist das ein Text für Menschen statt eines Bezeichners?"""
    t = text.strip()
    if len(t) < 2 or not re.search(r"[A-Za-zÄÖÜäöü]{2}", t):
        return False
    if t.startswith(("/", "http", "#", "lumen", "org.", "com.", "application/",
                     "image/", "video/", "text/", "audio/", "%")):
        return False
    if klein_als_wert and re.fullmatch(r"[a-zäöüß]{2,}", t) and t not in KENNUNGEN:
        return True
    if re.fullmatch(r"[a-z0-9_.\-:/]+", t):           # kennung, sf.symbol, pfad
        return False
    if re.fullmatch(r"[A-Za-z0-9_\-]+\.[a-z0-9]+", t):  # Datei.ext
        return False
    if re.fullmatch(r"[A-Z][A-Za-z]*-[A-Za-z\-]+", t):  # HTTP-Kopfzeile
        return False
    if " " in t or DEUTSCH.search(t):
        return True
    # Einzelwort mit Grossbuchstaben am Anfang: "Filme", "Dunkel"
    return bool(re.fullmatch(r"[A-ZÄÖÜ][a-zäöüß]+", t))


# Kleingeschriebene Einzelwörter sind meist Kennungen („movie", „pin") —
# als Ergebnis eines switch oder return aber oft sichtbar („offen", „aus").
# Diese hier sind Kennungen, die so vorkommen.
KENNUNGEN = {
    "movie", "series", "season", "episode", "poster", "backdrop", "frame",
    "none", "pin", "password", "totp", "aus", "an", "und", "de", "en", "tv",
    "film", "voll", "verwalten", "schauen", "zeit", "phone", "tablet",
    "desktop", "browser", "web", "direct", "open", "working", "done",
    "declined", "title", "year", "added", "all", "info", "warn", "error",
    "debug", "auto", "dunkel", "hell", "gesehen", "merkliste", "neu",
    "folgen", "reihe", "umgebung", "ok", "fehler", "light", "dark", "hls",
    "mp", "mkv", "avi", "mov", "image", "bild", "farbe", "figur",
    # SF-Symbole und Schlüssel, die so im Code stehen
    "iphone", "ipad", "desktopcomputer", "globe", "house", "magnifyingglass",
    "bookmark", "heart", "bell", "gearshape", "textformat", "clock",
    "taeglich", "player", "code", "profil", "untertitel",
    # Sortierung: SF-Symbole und die Richtung, wie der Server sie versteht
    "calendar", "star", "timer", "eye", "shuffle", "asc", "desc",
}
# Wo ein kleingeschriebenes Literal als Wert dasteht.
WERT_STELLE = re.compile(r"(\breturn|\?|\s:|\bcase\b[^\n]*:|\bdefault:|[^=!<>]=|\.append\()\s*$")

# Literale, die absichtlich nicht übersetzt werden: Eigennamen, Formate.
ABSICHTLICH = {
    "Lumen", "Lumen TV", "AGPL-3.0", "GNU AGPL v3", "Apple TV", "SharePlay",
    "TMDb", "OMDb", "TVmaze", "OpenSubtitles", "Rotten Tomatoes", "IMDb",
    "Emby", "Jellyfin", "Plex", "HDR", "Dolby Vision", "HLS", "ffmpeg",
    "Authenticator", "Lumen auf dem Mac",
}

# Zusammenhänge, in denen ein Literal nie sichtbar ist.
TECHNIK_ZEILE = re.compile(
    r"(print\(|Logger|log\.|os_log|fatalError\(|assert|precondition|"
    r"debugDescription|forKey:|forHTTPHeaderField|systemName:|systemImage:|"
    r"Image\(\"|UserDefaults|@AppStorage|\.appendingPathComponent|URL\(string:|"
    r"NSPredicate|CodingKeys|case \w+ = \"|accessibilityIdentifier|"
    r"Notification\.Name|DispatchQueue\(label|keyboardShortcut|"
    r"\.font\(\.custom|Font\.custom|withName:|"
    r"// sprache: fest)")


def zeile_von(lit: Literal) -> str:
    return lit.datei.read_text(encoding="utf-8").splitlines()[lit.zeile - 1]


def swift_dateien(bereich):
    for ordner in bereich["ordner"]:
        yield from sorted(ordner.rglob("*.swift"))


# ------------------------------------------------------------- Katalog

def katalog_lesen(pfad: pathlib.Path):
    if not pfad.exists():
        return None, f"{pfad.relative_to(WURZEL)} fehlt"
    try:
        return json.loads(pfad.read_text(encoding="utf-8")), None
    except json.JSONDecodeError as e:
        return None, f"{pfad.relative_to(WURZEL)}: kein gültiges JSON — {e}"


PLATZHALTER = re.compile(r"%(?:\d+\$)?(?:lld|llu|ld|lu|lf|d|u|f|@|\.\d+f|\.\d+lf)")


def platzhalter(text: str) -> list[str]:
    return sorted(re.sub(r"\d+\$", "", p) for p in PLATZHALTER.findall(text.replace("%%", "")))


def uebersetzungen(eintrag, sprache):
    """Alle Texte einer Sprache in einem Eintrag (auch Mehrzahl-Varianten)."""
    lok = (eintrag.get("localizations") or {}).get(sprache)
    if not lok:
        return []
    texte = []

    def sammeln(knoten):
        if "stringUnit" in knoten:
            texte.append(knoten["stringUnit"].get("value", ""))
        for gruppe in (knoten.get("variations") or {}).values():
            for unter in gruppe.values():
                sammeln(unter)
        for sub in (knoten.get("substitutions") or {}).values():
            sammeln(sub)
    sammeln(lok)
    return texte


def pruefen(liste: bool = False) -> int:
    fehler, warnungen = [], []
    for name, bereich in BEREICHE.items():
        katalog, problem = katalog_lesen(bereich["katalog"])
        if problem:
            fehler.append(problem)
            katalog = {"strings": {}}
        if katalog.get("sourceLanguage") not in (KATALOG_QUELLE, None):
            fehler.append(f"{name}: Quellsprache ist {katalog.get('sourceLanguage')}, "
                          f"erwartet {KATALOG_QUELLE}")
        eintraege = katalog.get("strings", {})
        benutzt = {}
        fest = []
        for datei in swift_dateien(bereich):
            for lit in literale(datei):
                if lit.kontext in ("schluessel", "wert"):
                    s = schluessel(lit)
                    if s.strip() and re.search(r"[A-Za-zÄÖÜäöü]", s):
                        benutzt.setdefault(s, lit)
                    continue
                text = lit.text
                # Nur was wörtlich dasteht, zählt — nicht die Ausdrücke in \( ).
                woertlich = "".join(t for art, t in lit.teile if art == "text")
                z = zeile_von(lit)
                spalte = z.find(lit.roh)
                als_wert = spalte >= 0 and bool(WERT_STELLE.search(z[:spalte]))
                if not sieht_sichtbar_aus(woertlich, als_wert) or text.strip() in ABSICHTLICH:
                    continue
                z = zeile_von(lit)
                if TECHNIK_ZEILE.search(z):
                    continue
                fest.append(lit)

        print(f"{name}")
        print(f"  {len(benutzt)} übersetzbare Texte im Code, "
              f"{len(eintraege)} Einträge im Katalog")
        if liste:
            for s, lit in sorted(benutzt.items()):
                print(f"    {s!r}  ({lit.datei.relative_to(WURZEL)}:{lit.zeile})")

        for s, lit in sorted(benutzt.items(), key=lambda x: (str(x[1].datei), x[1].zeile)):
            ort = f"{lit.datei.relative_to(WURZEL)}:{lit.zeile}"
            e = eintraege.get(s)
            if e is None:
                fehler.append(f"{ort}: fehlt im Katalog: {s!r}")
                continue
            if e.get("shouldTranslate") is False:
                continue
            for sprache in SPRACHEN:
                texte = uebersetzungen(e, sprache)
                if not texte:
                    if sprache == QUELLE:
                        continue          # der Schlüssel ist der Text
                    fehler.append(f"{ort}: nicht übersetzt ({sprache}): {s!r}")
                    continue
                for t in texte:
                    # Mehrzahl-Varianten dürfen die Zahl weglassen („Eine Folge“).
                    if "variations" in (e["localizations"][sprache]):
                        continue
                    if platzhalter(t) != platzhalter(s):
                        fehler.append(f"{ort}: Platzhalter passen nicht ({sprache}): "
                                      f"{s!r} → {t!r}")

        # Einträge, die niemand mehr benutzt — nur ein Hinweis; Xcode
        # markiert sie selbst als „stale".
        alt = sorted(set(eintraege) - set(benutzt))
        if alt:
            warnungen.append(f"{name}: {len(alt)} Katalogeinträge ohne Fundstelle "
                             f"(von Xcode extrahiert oder veraltet), z. B. {alt[:3]}")

        for lit in fest:
            fehler.append(f"{lit.datei.relative_to(WURZEL)}:{lit.zeile}: fest verdrahtet: "
                          f"{lit.text!r}")

    print()
    for w in warnungen:
        print(f"{GELB}!{AUS} {w}")
    if fehler:
        for f in fehler:
            print(f"{ROT}✗{AUS} {f}")
        print(f"\n{ROT}{len(fehler)} Abweichung(en).{AUS}")
        return 1
    print(f"{GRUEN}✓ Alle Texte der Apple-Apps sind in allen fünf Sprachen da.{AUS}")
    return 0


if __name__ == "__main__":
    sys.exit(pruefen(liste="--liste" in sys.argv))
