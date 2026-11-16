#!/usr/bin/env python3
# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
#
# Veröffentlicht unter der GNU Affero General Public License v3.0.
# Der vollständige Text steht in der Datei LICENSE.
# SPDX-License-Identifier: AGPL-3.0-only

"""Ein Rundgang durch die Android-Apps im Emulator — mit Bildschirmfotos.

    python3 tools/simulator/android_rundgang.py handy  fotos/android-handy
    python3 tools/simulator/android_rundgang.py tv     fotos/android-tv

Läuft im Simulator-Ablauf (.github/workflows/simulator.yml) neben einem
gestarteten Emulator und einem Lumen-Server auf dem Rechner (für den
Emulator unter 10.0.2.2 erreichbar). Kein Testcode in der App: Das Skript
bedient sie wie ein Mensch — über `adb shell input` — und findet, worauf
es tippen soll, im Bildschirmaufbau von `uiautomator dump`. Am Fernseher
geht es wie mit der Fernbedienung: Pfeiltasten, bis der Fokus auf dem
Ziel liegt, dann OK.

Ein Schritt, der sein Ziel nicht findet, bricht den Rundgang nicht ab: es
gibt trotzdem ein Foto, und im Bericht steht, was fehlte. Neben jedem Foto
liegt eine .txt-Datei mit den Texten, die auf dem Bildschirm standen — so
lässt sich ein Schritt verbessern, ohne das Bild zu öffnen.

Umgebung: SERVER (10.0.2.2:8097), LUMEN_TEST_NAME, LUMEN_TEST_PASSWORT,
ANDROID_SERIAL (optional, wie bei adb).
"""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import time
import xml.etree.ElementTree as ET
from dataclasses import dataclass

ART = sys.argv[1] if len(sys.argv) > 1 else "handy"
AUS = sys.argv[2] if len(sys.argv) > 2 else f"fotos/android-{ART}"
TV = ART == "tv"
PAKET = "org.lumen.tv" if TV else "org.lumen.app"
SERVER = os.environ.get("SERVER", "10.0.2.2:8097")
NAME = os.environ.get("LUMEN_TEST_NAME", "Admin")
PASSWORT = os.environ.get("LUMEN_TEST_PASSWORT", "lumen-test-2026")

os.makedirs(AUS, exist_ok=True)
BERICHT: list[dict] = []
NR = [0]


# ---------------------------------------------------------------- adb

def adb(*args: str, timeout: int = 60, binaer: bool = False):
    try:
        r = subprocess.run(["adb", *args], capture_output=True, timeout=timeout)
    except subprocess.TimeoutExpired:
        return b"" if binaer else ""
    return r.stdout if binaer else r.stdout.decode("utf-8", "replace")


def sh(befehl: str, timeout: int = 60) -> str:
    return adb("shell", befehl, timeout=timeout)


def taste(*codes: str, pause: float = 0.6):
    for c in codes:
        sh(f"input keyevent {c}")
        time.sleep(pause)


def tippen_bei(x: int, y: int):
    sh(f"input tap {x} {y}")


def eingeben(text: str):
    # `input text` kennt keine Leerzeichen; %s ist das Leerzeichen.
    sicher = text.replace("\\", "\\\\").replace(" ", "%s").replace("'", "\\'") \
                 .replace("&", "\\&").replace("(", "\\(").replace(")", "\\)")
    sh(f"input text '{sicher}'")
    time.sleep(0.8)


def tastatur_offen() -> bool:
    aus = sh("dumpsys input_method")
    return "mInputShown=true" in aus or "mIsInputViewShown=true" in aus


def tastatur_zu():
    """Die Bildschirmtastatur schliessen, falls sie offen ist — sie liegt
    sonst über den Knöpfen, und ein Tippen träfe eine Taste."""
    if tastatur_offen():
        sh("input keyevent KEYCODE_BACK")
        time.sleep(1)


def bildschirm() -> tuple[int, int]:
    m = re.search(r"(\d+)x(\d+)", sh("wm size"))
    return (int(m.group(1)), int(m.group(2))) if m else (1080, 1920)


BREITE, HOEHE = 0, 0


def wischen(richtung: str = "hoch"):
    """Am Telefon: den Inhalt nach oben schieben (= weiter nach unten sehen)."""
    x = BREITE // 2
    if richtung == "hoch":
        sh(f"input swipe {x} {int(HOEHE * 0.75)} {x} {int(HOEHE * 0.30)} 350")
    else:
        sh(f"input swipe {x} {int(HOEHE * 0.30)} {x} {int(HOEHE * 0.75)} 350")
    time.sleep(1.2)


# ---------------------------------------------------------------- Bildschirmaufbau

@dataclass
class Knoten:
    text: str
    desc: str
    klasse: str
    paket: str
    x1: int
    y1: int
    x2: int
    y2: int
    fokus: bool
    klickbar: bool
    gewaehlt: bool

    @property
    def mitte(self) -> tuple[int, int]:
        return (self.x1 + self.x2) // 2, (self.y1 + self.y2) // 2

    @property
    def flaeche(self) -> int:
        return max(0, self.x2 - self.x1) * max(0, self.y2 - self.y1)

    def enthaelt(self, x: int, y: int) -> bool:
        return self.x1 <= x <= self.x2 and self.y1 <= y <= self.y2

    @property
    def beschriftung(self) -> str:
        return self.text or self.desc


LETZTER_AUFBAU: list[Knoten] = []


def aufbau(versuche: int = 3) -> list[Knoten]:
    global LETZTER_AUFBAU
    for _ in range(versuche):
        aus = sh("uiautomator dump /sdcard/lumen-ui.xml", timeout=25)
        if "dumped" in aus:
            roh = adb("exec-out", "cat", "/sdcard/lumen-ui.xml", binaer=True)
            try:
                wurzel = ET.fromstring(roh)
            except ET.ParseError:
                time.sleep(1)
                continue
            knoten = []
            for n in wurzel.iter("node"):
                m = re.match(r"\[(-?\d+),(-?\d+)\]\[(-?\d+),(-?\d+)\]", n.get("bounds", ""))
                if not m:
                    continue
                knoten.append(Knoten(
                    text=n.get("text", "") or "", desc=n.get("content-desc", "") or "",
                    klasse=n.get("class", ""), paket=n.get("package", ""),
                    x1=int(m.group(1)), y1=int(m.group(2)), x2=int(m.group(3)), y2=int(m.group(4)),
                    fokus=n.get("focused") == "true", klickbar=n.get("clickable") == "true",
                    gewaehlt=n.get("selected") == "true" or n.get("checked") == "true"))
            LETZTER_AUFBAU = knoten
            return knoten
        time.sleep(1.5)
    LETZTER_AUFBAU = []
    return []


def finde(muster: str, knoten: list[Knoten] | None = None, bereich: str = "",
          nur_sichtbar: bool = True) -> Knoten | None:
    """Der beste Treffer für ein Muster (regulärer Ausdruck, ohne Gross/klein)."""
    knoten = aufbau() if knoten is None else knoten
    rx = re.compile(muster, re.I)
    # Nur in der eigenen App suchen — sonst träfe „Home" den Launcher.
    knoten = [k for k in knoten if k.paket in (PAKET, "")]
    treffer = [k for k in knoten if (k.text and rx.search(k.text)) or (k.desc and rx.search(k.desc))]
    if nur_sichtbar:
        treffer = [k for k in treffer if k.flaeche > 0 and k.y1 < HOEHE and k.x1 < BREITE and k.y2 > 0]
    if bereich == "unten":
        treffer = [k for k in treffer if k.mitte[1] > HOEHE * 0.80] or treffer
        treffer.sort(key=lambda k: -k.mitte[1])
    elif bereich == "oben":
        treffer = [k for k in treffer if k.mitte[1] < HOEHE * 0.25] or treffer
        treffer.sort(key=lambda k: k.mitte[1])
    elif bereich == "links":
        treffer = [k for k in treffer if k.mitte[0] < BREITE * 0.25] or treffer
        treffer.sort(key=lambda k: k.mitte[0])
    else:
        # Genaue Treffer vor Teiltreffern, kleine (das Wort selbst) vor
        # grossen (die ganze Zeile, in der es vorkommt).
        treffer.sort(key=lambda k: (0 if rx.fullmatch(k.beschriftung.strip()) else 1, k.flaeche))
    return treffer[0] if treffer else None


def dialoge_weg(knoten: list[Knoten] | None = None):
    """Systemdialoge des Emulators („Pixel Launcher isn't responding")
    wegklicken — sie gehören nicht zu Lumen und verdecken alles."""
    knoten = aufbau() if knoten is None else knoten
    if any(re.search(r"isn.t responding|reagiert nicht", k.beschriftung) for k in knoten):
        for k in knoten:
            if re.fullmatch(r"Wait|Warten", k.beschriftung.strip()):
                print("   Systemdialog weggeklickt", flush=True)
                tippen_bei(*k.mitte)
                time.sleep(1.5)
                return True
    return False


def warte(muster: str, sek: float = 20, bereich: str = "") -> Knoten | None:
    ende = time.time() + sek
    while time.time() < ende:
        dialoge_weg(LETZTER_AUFBAU)
        k = finde(muster, bereich=bereich)
        if k:
            return k
        time.sleep(1.5)
    return None


def textfelder(knoten: list[Knoten] | None = None) -> list[Knoten]:
    knoten = aufbau() if knoten is None else knoten
    return [k for k in knoten if k.klasse.endswith("EditText") and k.flaeche > 0]


# ---------------------------------------------------------------- Fernbedienung

RICHTUNG = {"hoch": "KEYCODE_DPAD_UP", "runter": "KEYCODE_DPAD_DOWN",
            "links": "KEYCODE_DPAD_LEFT", "rechts": "KEYCODE_DPAD_RIGHT"}


def fokussiert(knoten: list[Knoten]) -> Knoten | None:
    f = [k for k in knoten if k.fokus and k.flaeche > 0]
    # Das innerste fokussierte Element.
    return min(f, key=lambda k: k.flaeche) if f else None


def fokussiere(muster: str, max_schritte: int = 30, bereich: str = "") -> bool:
    """Mit den Pfeiltasten zum Ziel, wie mit der Fernbedienung."""
    letzte = None
    gleich = 0
    for _ in range(max_schritte):
        knoten = aufbau()
        ziel = finde(muster, knoten, bereich=bereich)
        if not ziel:
            return False
        f = fokussiert(knoten)
        if f is None:
            taste("KEYCODE_DPAD_DOWN", pause=0.5)
            continue
        zx, zy = ziel.mitte
        if f.enthaelt(zx, zy) or ziel.enthaelt(*f.mitte):
            return True
        fx, fy = f.mitte
        dx, dy = zx - fx, zy - fy
        # Erst in die Zeile, dann in der Zeile zum Ziel.
        if abs(dy) > max(40, (f.y2 - f.y1) // 2):
            r = "runter" if dy > 0 else "hoch"
        else:
            r = "rechts" if dx > 0 else "links"
        stand = (f.x1, f.y1, f.x2, f.y2)
        if stand == letzte:
            gleich += 1
            if gleich >= 3:
                # Der Fokus bewegt sich nicht mehr: eine Querrichtung probieren.
                r = {"runter": "rechts", "hoch": "links", "rechts": "runter", "links": "hoch"}[r]
                gleich = 0
        else:
            gleich = 0
        letzte = stand
        taste(RICHTUNG[r], pause=0.5)
    return False


# ---------------------------------------------------------------- Schritte

def foto(name: str, notiz: str = "", ok: bool = True):
    NR[0] += 1
    datei = f"{NR[0]:02d}-{name}"
    time.sleep(0.8)
    png = adb("exec-out", "screencap", "-p", binaer=True, timeout=30)
    with open(os.path.join(AUS, datei + ".png"), "wb") as f:
        f.write(png)
    knoten = aufbau()
    vorne = {k.paket for k in knoten if k.paket}
    if knoten and PAKET not in vorne and not im_vordergrund():
        ok = False
        notiz = (notiz + " " if notiz else "") + f"App nicht im Vordergrund ({', '.join(sorted(vorne))})"
    with open(os.path.join(AUS, datei + ".txt"), "w", encoding="utf-8") as f:
        for k in knoten:
            if k.beschriftung:
                zeichen = ("*" if k.fokus else " ") + ("+" if k.gewaehlt else " ")
                f.write(f"{zeichen} [{k.x1},{k.y1},{k.x2},{k.y2}] {k.beschriftung!r}\n")
    BERICHT.append({"foto": datei + ".png", "schritt": name, "ok": ok, "notiz": notiz})
    print(f"{time.strftime('%H:%M:%S')} [{'ok' if ok else '--'}] {datei} {notiz}", flush=True)


def im_vordergrund() -> bool:
    aus = sh("dumpsys window | grep -E 'mCurrentFocus|mFocusedApp'")
    return PAKET in aus


def app_starten():
    sh(f"monkey -p {PAKET} -c android.intent.category.{'LEANBACK_LAUNCHER' if TV else 'LAUNCHER'} 1")
    time.sleep(5)


def sicherstellen():
    """Hat ein „Zurück" zu viel die App verlassen: wieder hinein."""
    if not im_vordergrund():
        print("   App war nicht mehr vorne — neu geöffnet", flush=True)
        app_starten()


def waehle(muster: str, bereich: str = "", warten: float = 8) -> bool:
    """Ein Element öffnen: tippen am Telefon, Fokus und OK am Fernseher."""
    sicherstellen()
    k = warte(muster, warten, bereich=bereich)
    if not k and not bereich:
        # Vielleicht weiter unten oder oben: scrollen und noch einmal suchen.
        # Am Fernseher braucht es mehr Drücke: erst durch die Wahlpillen
        # über einem Raster, dann Reihe um Reihe.
        schritte = ["hoch"] * 8 + ["runter"] * 12 if TV else ["hoch"] * 3 + ["runter"] * 6
        for richtung in schritte:
            if TV:
                taste("KEYCODE_DPAD_DOWN" if richtung == "hoch" else "KEYCODE_DPAD_UP")
            else:
                wischen(richtung)
            k = finde(muster)
            if k:
                break
    if not k:
        print(f"   nicht gefunden: {muster}", flush=True)
        return False
    if TV:
        if fokussiere(muster, bereich=bereich):
            taste("KEYCODE_DPAD_CENTER", pause=1.5)
            return True
        print(f"   Fokus kam nicht an: {muster} — tippe stattdessen", flush=True)
        k = finde(muster, bereich=bereich) or k
    tippen_bei(*k.mitte)
    time.sleep(1.5)
    return True


def schritt(name: str, muster: str, bereich: str = "", warten_danach: float = 3,
            notiz: str = "") -> bool:
    ok = waehle(muster, bereich=bereich)
    time.sleep(warten_danach)
    foto(name, notiz or ("" if ok else f"nicht gefunden: {muster}"), ok)
    return ok


def zurueck(n: int = 1):
    taste(*(["KEYCODE_BACK"] * n), pause=1.2)


def suche_nach_unten(muster: str, max_wischer: int = 5) -> bool:
    """Am Telefon nach unten scrollen, bis das Ziel zu sehen ist."""
    for _ in range(max_wischer):
        if finde(muster):
            return True
        if TV:
            taste("KEYCODE_DPAD_DOWN")
        else:
            wischen("hoch")
    return bool(finde(muster))


# ---------------------------------------------------------------- Anmeldung

def anmelden():
    # Vor der Anmeldung spricht die App die Sprache des Geräts — im Emulator
    # oft Englisch; am Fernseher steht nur der Platzhalter im Feld.
    adresse_muster = r"^(Adresse|Address)$|lumen\.local|192\.168\.1\.20|^(Verbinden|Connect)$"
    k = warte(adresse_muster + r"|Server hinzufügen|Add server|Mit Code|Profil hier|" + re.escape(NAME), 40)
    foto("erster-start", "" if k else "kein Anmeldebildschirm erkannt", bool(k))
    if finde(r"^Server hinzufügen$|^Add server$"):
        waehle(r"^Server hinzufügen$|^Add server$")
        time.sleep(2)
    felder = textfelder()
    if felder:
        f = felder[0]
        if TV:
            fokussiere_feld(f)
        else:
            tippen_bei(*f.mitte)
        time.sleep(0.8)
        eingeben(SERVER)
        if not TV and tastatur_offen():
            # Mit offener Tastatur: muss „Verbinden" darüber zu sehen sein.
            foto("adresse-tastatur")
        tastatur_zu()
        foto("adresse")
        if finde(r"^Verbinden$|^Connect$"):
            waehle(r"^Verbinden$|^Connect$")
        time.sleep(4)
        # Erreicht der Emulator den Server überhaupt? (fürs Protokoll)
        print("   Server vom Emulator aus:", sh(
            f"printf 'GET /api/v1/hello HTTP/1.0\\r\\n\\r\\n' | nc -w 5 {SERVER.replace(':', ' ')} | tail -1")[:200],
            flush=True)
        # Nach „Verbinden" kann ein Ladekreis stehen bleiben (am Fernseher
        # gesehen: der Ablauf, der den Stand übernehmen sollte, wird mit der
        # Ansicht abgebrochen). Dann: Foto, App neu starten — der Server ist
        # da schon gespeichert.
        for versuch in range(3):
            if warte(r"Profil|Profile|Mit Code|With code|" + re.escape(NAME), 20):
                break
            foto("verbinden-haengt", "nach „Verbinden“ nach 20 s noch keine Profilauswahl — App neu gestartet", False)
            sh(f"am force-stop {PAKET}")
            app_starten()
    # Am Fernseher kommt zuerst „Mit Code anmelden"; das Profil steht daneben.
    if TV and warte(r"Profil hier wählen|Choose profile here|Pick a profile here", 10):
        foto("tv-code-anmelden")
        waehle(r"Profil hier wählen|Choose profile here|Pick a profile here")
        time.sleep(2)
    k = warte("^" + re.escape(NAME) + "$", 25)
    foto("profile", "" if k else "Profil nicht gefunden", bool(k))
    if k:
        waehle("^" + re.escape(NAME) + "$")
        time.sleep(2.5)
    felder = textfelder()
    if felder:
        if TV:
            fokussiere_feld(felder[0])
        else:
            tippen_bei(*felder[0].mitte)
        time.sleep(0.8)
        eingeben(PASSWORT)
        tastatur_zu()
        foto("passwort")
        knopf = r"^(Anmelden|Sign In|Entsperren|Unlock|Weiter|Continue|OK)$"
        if finde(knopf):
            waehle(knopf)
        else:
            taste("KEYCODE_ENTER", pause=2)
    else:
        print("   kein Passwortfeld gefunden", flush=True)
    k = warte(r"^(Startseite|Home)$|Weiterschauen|Continue Watching", 40)
    time.sleep(4)
    foto("startseite", "" if k else "nach der Anmeldung keine Startseite", bool(k))
    return bool(k)


def fokussiere_feld(feld: Knoten):
    """Am Fernseher ein Textfeld per Fokus erreichen und die Tastatur öffnen."""
    # Die Pfeiltasten bringen den Fokus hin; OK öffnete die Tastatur, deren
    # erste Taste dann den nächsten Druck schluckte. Ein Tippen aufs Feld
    # fokussiert es ebenso und lässt sich zuverlässig wiederholen.
    tippen_bei(*feld.mitte)
    time.sleep(1.2)


# ---------------------------------------------------------------- Rundgang

def player_verlassen():
    """Zurück aus dem Player, und das Telefon wieder hochkant."""
    for _ in range(3):
        if finde(r"^(Startseite|Home|Merken|Zurück)$"):
            break
        taste("KEYCODE_BACK", pause=1.5)
    sicherstellen()


FOLGE_S2 = r"Sieben Dreissig Sieben"


def staffel_2_offen(knoten: list[Knoten]) -> bool:
    """Steht „Staffel 2" als gewählte Staffel da (Chip oder Pille)?"""
    return any(k.gewaehlt and re.search(r"^(Staffel 2|Season 2|2)$", k.beschriftung)
               for k in knoten)


def folge_von_startseite():
    """Eine Folge aus Staffel 2 in „Weiterschauen" (einrichten.py): die
    Serie soll mit Staffel 2 aufgehen und die Folge zeigen — nicht Staffel 1."""
    muster = FOLGE_S2 + r"|S2 · F1|S2 · E1"
    if not waehle(muster):
        foto("folge-startseite", "Breaking Bad S2 F1 nicht auf der Startseite", False)
        return
    time.sleep(3)
    if TV:
        # Am Fernseher öffnet die Karte die Folge; „Serie" führt weiter.
        foto("folge-startseite-folge")
        if not waehle(r"^(Serie|Show|Series)$"):
            foto("folge-startseite-serie", "Knopf „Serie“ nicht gefunden", False)
            zurueck()
            return
        time.sleep(4)
        knoten = aufbau()
        f = fokussiert(knoten)
        t = finde(FOLGE_S2, knoten)
        # Die Karte trägt den Fokus, ihr Name steht in einem Kind.
        auf_folge = bool(f and t and (f.enthaelt(*t.mitte) or re.search(FOLGE_S2, f.beschriftung)))
        foto("folge-startseite-serie",
             f"Fokus: {f.beschriftung if f else 'keiner'}; Staffel 2 gewählt: {staffel_2_offen(knoten)}",
             auf_folge)
        zurueck(2)
    else:
        knoten = aufbau()
        # Die Folge gibt es nur in Staffel 2 — steht sie da, ist Staffel 2
        # offen (den gewählten Chip meldet uiautomator nicht als „selected").
        sichtbar = bool(finde(FOLGE_S2, knoten))
        foto("folge-startseite-serie", f"Folge aus Staffel 2 zu sehen: {sichtbar}", sichtbar)
        zurueck()
    sicherstellen()


def rundgang_handy():
    # Hochkant festhalten: der Player dreht sonst ins Querformat.
    sh("settings put system accelerometer_rotation 0")
    sh("settings put system user_rotation 0")
    wischen("hoch")
    foto("startseite-weiter-unten")
    wischen("hoch")
    foto("startseite-ganz-unten")
    wischen("runter"); wischen("runter"); wischen("runter")

    folge_von_startseite()

    schritt("filme", r"^(Filme|Movies)$", bereich="unten")
    schritt("filmreihen", r"^(Filmreihen|Franchises)$", bereich="oben")
    schritt("filmreihe-dark-knight", r"Dark Knight Collection|Dark Knight")
    zurueck()
    schritt("alle-filme", r"^(Alle Filme|All movies|All Movies)$", bereich="oben")
    if schritt("film-inception", r"^Inception$"):
        wischen("hoch")
        foto("film-inception-unten")
        zurueck()
    if schritt("film-avatar", r"^Avatar"):
        # Zwei Dateien (de/en): steht „Fassung" oder „Qualitäten" am Knopf?
        # (Der Knopf selbst spielt ab — darum nur das Foto oben.)
        zurueck()
    if schritt("film-der-pate", r"^Der Pate$"):
        zurueck()

    schritt("serien", r"^(Serien|Shows)$", bereich="unten")
    if schritt("serie-breaking-bad", r"^Breaking Bad$"):
        wischen("hoch")
        foto("serie-breaking-bad-folgen")
        schritt("serie-staffelwahl", r"^(Staffel 1|Season 1|Staffel 01)$|Staffel|Season")
        schritt("serie-specials", r"^(Specials|Staffel 0|Season 0|Extras)$")
        if finde(r"^(Staffel 2|Season 2)$") is None:
            # Staffelwahl als Menü: noch einmal öffnen.
            waehle(r"Specials|Staffel|Season")
        schritt("serie-staffel-2", r"^(Staffel 2|Season 2)$")
        zurueck(2)

    # Suche: oben auf der Startseite (Lupe) oder in den Filmen.
    schritt("startseite-zurueck", r"^(Startseite|Home)$", bereich="unten")
    if waehle(r"^(Suche|Suchen|Search)$|Titel, Schauspieler|Title, actor"):
        felder = textfelder()
        if felder:
            tippen_bei(*felder[0].mitte)
            eingeben("Dark")
            taste("KEYCODE_ENTER", pause=2)
            tastatur_zu()
        foto("suche-dark")
        zurueck()
    else:
        foto("suche", "Suche nicht gefunden", False)

    schritt("profil", r"^(Profil|Profile)$", bereich="unten")
    wischen("hoch")
    foto("profil-unten")
    wischen("hoch")
    foto("profil-ganz-unten")

    schritt("merkliste", r"^(Merkliste|Watchlist)$", bereich="unten")

    # Wiedergabe: Interstellar aus „Weiterschauen".
    schritt("startseite-fuer-wiedergabe", r"^(Startseite|Home)$", bereich="unten")
    if waehle(r"^Interstellar$"):
        time.sleep(2)
        foto("film-interstellar")
        if waehle(r"(Weiterschauen|Fortsetzen|Weiter bei|Ansehen|Abspielen|Play|Resume|Continue)"):
            time.sleep(8)
            foto("wiedergabe")
            # Der Player steht quer: die Mitte aus dem Bildschirmaufbau,
            # nicht aus `wm size` (das bleibt hochkant).
            flaeche = max(aufbau() or [Knoten("", "", "", "", 0, 0, BREITE, HOEHE, False, False, False)],
                          key=lambda k: k.flaeche)
            if finde(r"Steuerelemente|controls", LETZTER_AUFBAU, nur_sichtbar=False) is None:
                # Die Steuerung steht schon (zu Beginn eingeblendet): erst
                # ausblenden lassen, dann per Tippen wieder zeigen.
                time.sleep(4)
            tippen_bei(*flaeche.mitte)
            time.sleep(1)
            foto("wiedergabe-steuerung")
            player_verlassen()
            time.sleep(2)
            foto("nach-wiedergabe")


def rundgang_tv():
    # Wo steht der Fokus nach der Anmeldung? (Soll: erste Karte der ersten Reihe.)
    f = fokussiert(aufbau())
    foto("startseite-fokus", f"Fokus: {f.beschriftung if f else 'keiner'}", bool(f))
    taste("KEYCODE_DPAD_DOWN")
    foto("startseite-zeile-2")
    taste("KEYCODE_DPAD_DOWN", "KEYCODE_DPAD_DOWN")
    foto("startseite-zeile-4")
    # Zurück: erst nach oben zur ersten Reihe, dann ein Hinweis, erst der
    # zweite Druck oben verlässt die App.
    taste("KEYCODE_BACK")
    time.sleep(1)
    f = fokussiert(aufbau())
    foto("startseite-nach-zurueck", f"Fokus: {f.beschriftung if f else 'keiner'}")
    taste("KEYCODE_BACK")
    time.sleep(0.5)
    foto("startseite-zurueck-hinweis", "", bool(finde(r"nochmals|again")))
    time.sleep(3.5)
    taste(*(["KEYCODE_DPAD_UP"] * 8), pause=0.4)
    foto("startseite-oben", "nach 8× hinauf")

    folge_von_startseite()

    schritt("filme", r"^(Filme|Movies)$", bereich="oben")
    if schritt("film-inception", r"^Inception$"):
        taste("KEYCODE_DPAD_DOWN", "KEYCODE_DPAD_DOWN")
        foto("film-inception-unten")
        zurueck()
    if schritt("film-avatar", r"^Avatar"):
        if finde(r"Fassung|Version|Qualitäten|Qualität"):
            schritt("film-avatar-fassung", r"Fassung|Version|Qualitäten|Qualität")
            zurueck()
        zurueck()

    schritt("serien", r"^(Serien|Shows)$", bereich="oben")
    if schritt("serie-breaking-bad", r"^Breaking Bad$"):
        taste("KEYCODE_DPAD_DOWN")
        foto("serie-breaking-bad-runter")
        schritt("serie-specials", r"^(Specials|Staffel 0|Season 0|Extras)$")
        schritt("serie-staffel-2", r"^(Staffel 2|Season 2)$")
        zurueck()

    if schritt("suche", r"^(Suche|Search)$", bereich="oben"):
        felder = textfelder()
        if felder:
            fokussiere_feld(felder[0])
            eingeben("Dark")
            time.sleep(2)
            tastatur_zu()
        foto("suche-dark")

    schritt("ich", r"^(Ich|Me)$", bereich="oben")
    taste("KEYCODE_DPAD_DOWN", "KEYCODE_DPAD_DOWN", "KEYCODE_DPAD_DOWN")
    foto("ich-runter")
    if schritt("filmreihen", r"^(Filmreihen|Franchises)$"):
        schritt("filmreihe-dark-knight", r"Dark Knight Collection|Dark Knight")
        zurueck(2)

    schritt("startseite-fuer-wiedergabe", r"^(Startseite|Home)$", bereich="oben")
    if waehle(r"^Interstellar$"):
        time.sleep(2)
        foto("film-interstellar")
        if waehle(r"(Weiterschauen|Fortsetzen|Weiter bei|Ansehen|Abspielen|Play|Resume|Continue)"):
            time.sleep(8)
            foto("wiedergabe")
            taste("KEYCODE_DPAD_CENTER", pause=1)
            foto("wiedergabe-steuerung")
            time.sleep(4)
            taste("KEYCODE_DPAD_RIGHT", pause=1)
            foto("wiedergabe-spulen")
            # Hoch aus der Zeitleiste: auf Lumens Leiste (Ton, Untertitel,
            # Zahnrad) — nicht auf „Pause" von Media3. Das Foto eben dauerte
            # fast so lang, wie die Steuerung stehen bleibt: erst noch einmal
            # spulen, damit sie sicher steht und die Zeitleiste den Fokus hat.
            taste("KEYCODE_DPAD_RIGHT", pause=0.5)
            taste("KEYCODE_DPAD_UP", pause=1)
            f = fokussiert(aufbau())
            leiste = r"^(Untertitel|Subtitles|Tonspur|Audio track|Qualität|Quality|Nächste Folge|Next episode)$"
            foto("wiedergabe-spuren", f"Fokus: {f.beschriftung if f else 'keiner'}",
                 bool(f and re.search(leiste, f.beschriftung)))
            # Zurück: erst die Leiste zu (Steuerung bleibt), dann die
            # Steuerung weg — der Player bleibt offen; erst dann zu.
            # (Beide Drücke rasch hintereinander: die Steuerung blendet sich
            # nach 3,5 Sekunden ohnehin aus.)
            taste("KEYCODE_BACK", pause=0.8)
            taste("KEYCODE_BACK", pause=1)
            foto("wiedergabe-zurueck-2", "Leiste zu, Steuerung weg, Player noch offen",
                 finde(r"^(Startseite|Home|Abspielen|Weiterschauen|Play|Resume)$") is None)
            taste("KEYCODE_BACK", pause=1.5)
            time.sleep(2)
            foto("nach-wiedergabe", "zurück auf der Titelseite", finde(r"^Interstellar$|^Weiter bei|^Von vorn$") is not None)


def main():
    global BREITE, HOEHE
    BREITE, HOEHE = bildschirm()
    print(f"Bildschirm {BREITE}x{HOEHE}, Paket {PAKET}, Server {SERVER}", flush=True)
    # Ohne Animationen bleibt der Bildschirmaufbau ruhig genug zum Lesen.
    for k in ("window_animation_scale", "transition_animation_scale", "animator_duration_scale"):
        sh(f"settings put global {k} 0")
    # Den Hinweis „Viewing full screen" im Player gleich als gelesen markieren.
    sh("settings put secure immersive_mode_confirmations confirmed")
    adb("logcat", "-c")
    sh(f"am force-stop {PAKET}")
    sh(f"monkey -p {PAKET} -c android.intent.category.{'LEANBACK_LAUNCHER' if TV else 'LAUNCHER'} 1")
    time.sleep(6)
    dialoge_weg()
    try:
        if anmelden():
            rundgang_tv() if TV else rundgang_handy()
    except Exception as e:  # noqa: BLE001 — der Bericht soll trotzdem entstehen
        print("Abbruch:", repr(e), flush=True)
        foto("abbruch", repr(e), False)
    finally:
        log = adb("logcat", "-d", "-v", "time", timeout=60)
        with open(os.path.join(AUS, "logcat-voll.txt"), "w", encoding="utf-8") as f:
            f.write(log)
        rx = re.compile(r"FATAL|AndroidRuntime|ANR in|lumen|Lumen|" + re.escape(PAKET), re.I)
        with open(os.path.join(AUS, "logcat.txt"), "w", encoding="utf-8") as f:
            f.writelines(z + "\n" for z in log.splitlines() if rx.search(z))
        # Nur die eigenen: im Emulator stürzt auch mal der Launcher ab.
        zeilen = log.splitlines()
        absturz = []
        for i, z in enumerate(zeilen):
            if ("ANR in" in z and PAKET in z) or ("FATAL EXCEPTION" in z
                                                 and any(PAKET in n for n in zeilen[i:i + 4])):
                absturz.extend(zeilen[i:i + 12])
        with open(os.path.join(AUS, "bericht.json"), "w", encoding="utf-8") as f:
            json.dump({"art": ART, "schritte": BERICHT, "abstuerze": absturz}, f,
                      ensure_ascii=False, indent=1)
        print(f"{len(BERICHT)} Fotos, {sum(1 for b in BERICHT if not b['ok'])} Schritte ohne Ziel, "
              f"{len(absturz)} Abstürze", flush=True)


if __name__ == "__main__":
    main()
