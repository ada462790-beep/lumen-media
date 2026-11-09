#!/usr/bin/env python3
# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
#
# Veröffentlicht unter der GNU Affero General Public License v3.0.
# Der vollständige Text steht in der Datei LICENSE.
# SPDX-License-Identifier: AGPL-3.0-only

"""Die Sprachen der Android-Apps prüfen.

    python3 tools/sprache_android_pruefen.py            # nur prüfen
    python3 tools/sprache_android_pruefen.py --schreiben # values-de-rCH neu abschreiben

Was geprüft wird:

  1. Feste Texte im Kotlin-Code von app, tv und lumen-texte: alles, was
     nach einem Satz für Menschen aussieht, gehört nach strings.xml. In
     lumen-core gehören Sätze nach Texte.kt (Tx, TxN). Eine Zeile, die
     bewusst festen Text trägt, endet mit dem Kommentar `// sprache: fest`.
  2. Vollständigkeit: jeder Text aus values/ (Quelle, de-CH) steht in
     values-de (de-DE) und values-en (en-US); values-de-rAT und
     values-en-rGB enthalten nur Abweichungen und nichts, was es in der
     Quelle nicht gibt.
  3. Platzhalter: %1$s, %2$d … stimmen in jeder Sprache mit der Quelle
     überein; Mehrzahlen haben „one" und „other".
  4. values-de-rCH ist eine genaue Abschrift von values (ohne das, was
     nicht übersetzt wird). Sie ist nötig, weil ein Gerät auf de-CH sonst
     bei values-de landete — mit ß.
  5. Die Vorgaben in lumen-core (Texte.kt) sind dieselben wie in
     lumen-texte/src/main/res/values/kern.xml.
  6. Schreibweise: kein ß in de-CH; in de-DE und de-AT keine Schweizer
     Formen („gross", „heisst" …); in en-GB keine amerikanischen
     („favorite", „color" …) und umgekehrt.

Ohne Fehler endet das Skript mit „Sprachen vollständig." und 0.
"""

from __future__ import annotations

import pathlib
import re
import sys

WURZEL = pathlib.Path(__file__).resolve().parent.parent
ANDROID = WURZEL / "android"
MODULE = ("app", "tv", "lumen-texte")
TEXTDATEIEN = ("strings.xml", "kern.xml", "sprache.xml")

ROT, GRUEN, AUS = "\033[31m", "\033[32m", "\033[0m"

# Feste Texte, die so bleiben dürfen: Markennamen, Schlüssel fürs System.
ERLAUBT = {"Lumen", "Android", "Android TV", "Chromecast", "AndroidKeyStore",
           "DEPRECATION", "UNCHECKED_CAST", "Dolby Vision", "Mac"}
MARKE = "// sprache: fest"

# Schweizer Schreibung, die in Deutschland und Österreich ß hat. Bewusst
# eine Liste statt „ss → ß": dass, muss, Passwort, Schlüssel bleiben.
SCHWEIZ_SS = r"\b(gross\w*|grösse\w*|grössen\w*|heisst|heissen|ausser\w*|aussen|liess\w*|weiss\w*|" \
             r"schliess\w*|strasse\w*|fuss\w*|spass|gemäss|bloss|draussen|giess\w*|stoss\w*|fliess\w*|" \
             r"regelmässig\w*|mass\b|masse\b|massstab\w*|geniess\w*|äusser\w*)\b"
US = r"\b(favorite\w*|colors?\b|colored|recogniz\w*|organiz\w*|personaliz\w*|center\b|canceled|gray)\b"
GB = r"\b(favourite\w*|colours?\b|recognis\w*|organis\w*|personalis\w*|centre\b|cancelled|grey|licence)\b"


# --------------------------------------------------------------- Lesen

def eintraege(datei: pathlib.Path) -> dict[str, tuple[str, object, bool]]:
    """name -> (art, wert, übersetzbar); art 's' oder 'p' (Mehrzahl: dict)."""
    s = datei.read_text(encoding="utf-8")
    out: dict[str, tuple[str, object, bool]] = {}
    for m in re.finditer(r'<string name="(\w+)"([^>]*)>(.*?)</string>', s, re.S):
        out[m.group(1)] = ("s", m.group(3), 'translatable="false"' not in m.group(2))
    for m in re.finditer(r'<plurals name="(\w+)"[^>]*>(.*?)</plurals>', s, re.S):
        formen = dict(re.findall(r'<item quantity="(\w+)">(.*?)</item>', m.group(2), re.S))
        out[m.group(1)] = ("p", formen, True)
    return out


def platzhalter(wert) -> list[str]:
    texte = wert.values() if isinstance(wert, dict) else [wert]
    alle = set()
    for t in texte:
        alle |= set(re.findall(r"%(\d+)\$([sd])", t))
    return sorted(alle)


def ordner(modul: str, name: str) -> pathlib.Path:
    return ANDROID / modul / "src/main/res" / name


# --------------------------------------------------------------- 1. feste Texte

def kotlin_literale(src: str):
    """(zeilennummer, inhalt) jeder Zeichenkette, Verkettungen zusammengefasst."""
    i, n = 0, len(src)
    roh = []

    def ende(i: int) -> int:
        if src.startswith('"""', i):
            j = src.index('"""', i + 3)
            while src.startswith('"', j + 3):
                j += 1
            return j + 3
        j = i + 1
        while True:
            c = src[j]
            if c == "\\":
                j += 2
                continue
            if c == '"':
                return j + 1
            if c == "$" and src[j + 1] == "{":
                tiefe, k = 0, j + 1
                while True:
                    ch = src[k]
                    if ch == '"':
                        k = ende(k)
                        continue
                    if ch == "{":
                        tiefe += 1
                    elif ch == "}":
                        tiefe -= 1
                        if tiefe == 0:
                            break
                    k += 1
                j = k + 1
                continue
            j += 1

    while i < n:
        if src.startswith("//", i):
            k = src.find("\n", i)
            i = n if k < 0 else k
            continue
        if src.startswith("/*", i):
            i = src.index("*/", i) + 2
            continue
        if src[i] == "'":
            m = re.match(r"'(?:\\u[0-9a-fA-F]{4}|\\.|[^'\\])'", src[i:])
            if m:
                i += m.end()
                continue
        if src[i] == '"':
            e = ende(i)
            roh.append((i, e, src[i:e]))
            i = e
            continue
        i += 1

    gruppen = []
    for s, e, t in roh:
        if gruppen and re.fullmatch(r"\s*\+\s*", src[gruppen[-1][1]:s]):
            gruppen[-1] = (gruppen[-1][0], e, gruppen[-1][2] + [t])
        else:
            gruppen.append((s, e, [t]))
    for s, e, teile in gruppen:
        if any(t.startswith('"""') for t in teile):
            continue
        yield src.count("\n", 0, s) + 1, "".join(t[1:-1] for t in teile), src.count("\n", 0, e) + 1


def sieht_nach_text_aus(text: str, kern: bool) -> bool:
    sichtbar = re.sub(r"\$\{.*?\}|\$\w+", "", text).strip()
    if sichtbar in ERLAUBT or not re.search(r"[A-Za-zÄÖÜäöüß]{2,}", sichtbar):
        return False
    woerter = [w for w in re.findall(r"[A-Za-zÄÖÜäöüß]+", sichtbar) if len(w) > 1]
    if re.fullmatch(r"[a-z][A-Za-z0-9]*", sichtbar):
        return False      # ein Bezeichner wie „skipIntro"
    if re.search(r"[ÄÖÜäöüß]", sichtbar):
        return True
    if kern:
        if "${" in text:
            return False  # zusammengesetzte Pfade und Muster, keine Sätze
        # lumen-core: Methoden, Kopfzeilen, Codecs — erst ab drei Wörtern ein Satz.
        return len(woerter) >= 3 and " " in sichtbar
    if re.fullmatch(r"[a-z0-9_.:/\-|?&=%#,+${}()\[\] ]*", sichtbar):
        return False      # Schlüssel, Pfade, Muster
    return bool(re.search(r"[A-Z][a-z]", sichtbar)) or " " in sichtbar


def feste_texte() -> list[str]:
    fehler = []
    dateien = []
    for modul in ("app", "tv", "lumen-texte"):
        dateien += [(p, False) for p in (ANDROID / modul / "src/main/kotlin").rglob("*.kt")]
    for p in (ANDROID / "lumen-core/src/main/kotlin").rglob("*.kt"):
        if p.name not in ("Texte.kt", "Sprachen.kt", "Json.kt"):
            dateien.append((p, True))
    for p, kern in sorted(dateien):
        src = p.read_text(encoding="utf-8")
        zeilen = src.splitlines()
        for nr, text, bis in kotlin_literale(src):
            if not sieht_nach_text_aus(text, kern):
                continue
            if any(MARKE in zeilen[z - 1] for z in range(nr, bis + 1)):
                continue
            ort = "Texte.kt (Tx)" if kern else "strings.xml"
            fehler.append(f"{p.relative_to(WURZEL)}:{nr}: fester Text „{text[:60]}“ — gehört nach {ort}")
    print(f"  {len(dateien)} Kotlin-Dateien auf feste Texte durchgesehen")
    return fehler


# --------------------------------------------------------------- 2.–4. Ressourcen

def abschrift(quelle: pathlib.Path) -> str:
    """values-de-rCH: die Quelle ohne Unübersetzbares, mit eigenem Kopf."""
    s = quelle.read_text(encoding="utf-8")
    s = re.sub(r'\s*<string name="\w+"[^>]*translatable="false"[^>]*>.*?</string>', "", s)
    s = re.sub(r"<!--.*?-->", "<!-- Lumen — eigener Streaming-Server fürs Heimnetz. AGPL-3.0-only.\n"
               "     Deutsch (Schweiz): Abschrift von values/" + quelle.name + ", erzeugt mit\n"
               "     tools/sprache_android_pruefen.py mit dem Schalter „schreiben“.\n"
               "     Nicht von Hand ändern. -->",
               s, count=1, flags=re.S)
    # Lint kennt nur die deutsche Rechtschreibung mit ß und hielte jedes
    # „heisst" für einen Tippfehler.
    s = s.replace("<resources>", '<resources xmlns:tools="http://schemas.android.com/tools" tools:ignore="Typos">', 1)
    return s


def ressourcen(schreiben: bool) -> list[str]:
    fehler = []
    geprueft = 0
    for modul in MODULE:
        basis = ordner(modul, "values")
        if not basis.is_dir():
            continue
        quelle: dict[str, tuple] = {}
        for datei in TEXTDATEIEN:
            if (basis / datei).exists():
                quelle.update(eintraege(basis / datei))
                ziel = ordner(modul, "values-de-rCH") / datei
                soll = abschrift(basis / datei)
                if schreiben:
                    ziel.parent.mkdir(exist_ok=True)
                    ziel.write_text(soll, encoding="utf-8")
                elif not ziel.exists() or ziel.read_text(encoding="utf-8") != soll:
                    fehler.append(f"{ziel.relative_to(WURZEL)}: weicht von values/{datei} ab "
                                  f"— tools/sprache_android_pruefen.py --schreiben")
        uebersetzbar = {k: v for k, v in quelle.items() if v[2]}

        for name, voll in (("values-de-rCH", True), ("values-de", True), ("values-en", True),
                           ("values-de-rAT", False), ("values-en-rGB", False)):
            o = ordner(modul, name)
            sprache: dict[str, tuple] = {}
            for d in sorted(o.glob("*.xml")) if o.is_dir() else []:
                sprache.update(eintraege(d))
            for k in sorted(set(uebersetzbar) - set(sprache)) if voll else []:
                fehler.append(f"{modul}/{name}: „{k}“ fehlt")
            for k, (art, wert, _) in sorted(sprache.items()):
                geprueft += 1
                if k not in uebersetzbar:
                    fehler.append(f"{modul}/{name}: „{k}“ gibt es in values nicht"
                                  + (" (nicht übersetzbar)" if k in quelle else ""))
                    continue
                q_art, q_wert, _ = uebersetzbar[k]
                if art != q_art:
                    fehler.append(f"{modul}/{name}: „{k}“ ist hier {art}, in values {q_art}")
                    continue
                if platzhalter(wert) != platzhalter(q_wert):
                    fehler.append(f"{modul}/{name}: „{k}“ hat andere Platzhalter als values")
                if art == "p" and set(wert) != {"one", "other"}:
                    fehler.append(f"{modul}/{name}: Mehrzahl „{k}“ braucht one und other")
                texte = list(wert.values()) if isinstance(wert, dict) else [wert]
                for t in texte:
                    if name == "values-de-rCH" and "ß" in t:
                        fehler.append(f"{modul}/{name}: „{k}“ — ß gibt es in der Schweiz nicht")
                    if name in ("values-de", "values-de-rAT"):
                        m = re.search(SCHWEIZ_SS, t, re.I)
                        if m:
                            fehler.append(f"{modul}/{name}: „{k}“ — Schweizer Schreibung „{m.group(0)}“")
                    if name == "values-en-rGB" and re.search(US, t, re.I):
                        fehler.append(f"{modul}/{name}: „{k}“ — amerikanische Schreibung")
                    if name == "values-en" and re.search(GB, t, re.I):
                        fehler.append(f"{modul}/{name}: „{k}“ — britische Schreibung")
        for d in TEXTDATEIEN:
            if (basis / d).exists():
                for k, (art, wert, _) in eintraege(basis / d).items():
                    for t in (wert.values() if isinstance(wert, dict) else [wert]):
                        if "ß" in t:
                            fehler.append(f"{modul}/values: „{k}“ — ß gibt es in der Schweiz nicht")
    print(f"  {geprueft} übersetzte Texte gegen die Quelle gehalten")
    return fehler


# --------------------------------------------------------------- 5. lumen-core

def kern_gleich() -> list[str]:
    fehler = []
    src = (ANDROID / "lumen-core/src/main/kotlin/org/lumen/core/Texte.kt").read_text(encoding="utf-8")
    res = eintraege(ordner("lumen-texte", "values") / "kern.xml")

    def kt(s: str) -> str:
        return s.replace('\\$', '$').replace('\\"', '"')

    def xml(s: str) -> str:
        s = s.strip()
        if s.startswith('"') and s.endswith('"'):
            s = s[1:-1]
        return (s.replace("\\'", "'").replace('\\"', '"').replace("&lt;", "<")
                 .replace("&gt;", ">").replace("&amp;", "&"))

    tx = re.search(r"enum class Tx\(.*?\{(.*?)\n\}", src, re.S).group(1)
    txn = re.search(r"enum class TxN\(.*?\{(.*?)\n\}", src, re.S).group(1)
    namen = set()
    for name, wert in re.findall(r'^\s+([A-Z_0-9]+)\("((?:[^"\\]|\\.)*)"\),', tx, re.M):
        k = "kern_" + name.lower()
        namen.add(k)
        if k not in res:
            fehler.append(f"kern.xml: {k} fehlt (Tx.{name})")
        elif xml(res[k][1]) != kt(wert):
            fehler.append(f"kern.xml: {k} weicht von Tx.{name} ab")
    for name, eins, mehr in re.findall(r'^\s+([A-Z_0-9]+)\("((?:[^"\\]|\\.)*)", "((?:[^"\\]|\\.)*)"\),', txn, re.M):
        k = "kern_" + name.lower()
        namen.add(k)
        if k not in res:
            fehler.append(f"kern.xml: {k} fehlt (TxN.{name})")
        elif {q: xml(v) for q, v in res[k][1].items()} != {"one": kt(eins), "other": kt(mehr)}:
            fehler.append(f"kern.xml: {k} weicht von TxN.{name} ab")
    for k in sorted(set(res) - namen):
        fehler.append(f"kern.xml: {k} gehört zu keiner Kennung in Texte.kt")
    uebersetzer = (ANDROID / "lumen-texte/src/main/kotlin/org/lumen/texte/KernUebersetzer.kt").read_text(encoding="utf-8")
    for k in sorted(namen):
        if f".{k}" not in uebersetzer:
            fehler.append(f"KernUebersetzer.kt: {k} wird nicht zugeordnet")
    print(f"  {len(namen)} Kennungen aus lumen-core mit kern.xml verglichen")
    return fehler


def main() -> int:
    schreiben = "--schreiben" in sys.argv[1:]
    print("Feste Texte")
    fehler = feste_texte()
    print("Ressourcen")
    fehler += ressourcen(schreiben)
    print("lumen-core")
    fehler += kern_gleich()
    print()
    if fehler:
        for f in fehler:
            print(f"{ROT}✗{AUS} {f}")
        print(f"\n{ROT}{len(fehler)} Befund(e).{AUS}")
        return 1
    print(f"{GRUEN}✓ Sprachen vollständig.{AUS}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
