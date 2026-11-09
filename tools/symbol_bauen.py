#!/usr/bin/env python3
# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
#
# Veröffentlicht unter der GNU Affero General Public License v3.0.
# Der vollständige Text steht in der Datei LICENSE.
# SPDX-License-Identifier: AGPL-3.0-only
"""Zeichnet das Lumen-Symbol und legt alle Grössen an, die die Apps wollen.

Die Bildmarke: ein Abspiel-Dreieck aus Licht. Es zeigt nach rechts, wie
jeder Abspielknopf, und hinter ihm steht ein weicher Schein — lumen heisst
Licht, und ein Filmabend fängt damit an, dass es dunkel wird und etwas
aufleuchtet.

Der erste Entwurf war ein Projektorstrahl: Spitze links, Öffnung rechts.
Er sah schöner aus und war falsch — ein nach links zeigendes Dreieck heisst
auf jedem Gerät „zurück". Ein Symbol, das man erklären muss, ist keines.

Gezeichnet wird mit Pillow, nicht als SVG — damit die Verläufe wirklich
stimmen und nicht davon abhängen, was gerade zum Umwandeln installiert
ist. Alles entsteht vierfach vergrössert und wird dann verkleinert; sonst
sind die schrägen Kanten treppig.

    python3 tools/symbol_bauen.py
"""

from __future__ import annotations

import os
import pathlib
from PIL import Image, ImageDraw, ImageFilter

WURZEL = pathlib.Path(__file__).resolve().parent.parent
UEBER = 4  # so viel grösser wird gezeichnet, bevor verkleinert wird

# Die Farben. Der Grund ist nicht schwarz, sondern ein sehr dunkles Blau —
# Schwarz wirkt auf einem Startbildschirm wie ein Loch, ein dunkles Blau
# wie Nacht.
GRUND_OBEN = (14, 18, 34)
GRUND_UNTEN = (7, 9, 20)
STRAHL_HELL = (255, 250, 235)   # warmes Weiss am Ursprung
STRAHL_FERN = (74, 150, 255)    # kühles Blau am Ende
GLUT = (90, 160, 255)           # der Schein um den Strahl


def verlauf_senkrecht(groesse: int, oben, unten) -> Image.Image:
    """Ein senkrechter Verlauf. Eine Spalte reicht — der Rest ist gedehnt."""
    spalte = Image.new("RGB", (1, groesse))
    p = spalte.load()
    for y in range(groesse):
        t = y / max(groesse - 1, 1)
        p[0, y] = tuple(round(o + (u - o) * t) for o, u in zip(oben, unten))
    return spalte.resize((groesse, groesse), Image.BILINEAR)


def radialer_schein(groesse: int, mx: float, my: float, radius: float) -> Image.Image:
    """Eine weiche runde Aufhellung — der Schein, den Licht im Dunst macht."""
    klein = max(groesse // 8, 16)
    m = Image.new("L", (klein, klein), 0)
    d = ImageDraw.Draw(m)
    schritte = 26
    for i in range(schritte, 0, -1):
        t = i / schritte
        r = radius * t * klein / groesse
        # Quadratisch abfallend: so sieht ein Lichtschein aus, nicht linear.
        wert = round(255 * (1 - t) ** 2)
        d.ellipse([mx * klein / groesse - r, my * klein / groesse - r,
                   mx * klein / groesse + r, my * klein / groesse + r], fill=wert)
    return m.resize((groesse, groesse), Image.BICUBIC).filter(
        ImageFilter.GaussianBlur(groesse / 60))


def strahl_maske(groesse: int) -> Image.Image:
    """Das Abspiel-Dreieck als Maske, mit gerundeten Ecken.

    Pillow kann keine gerundeten Vielecke. Zwei Wege wurden probiert:

    Der erste zog den Umriss mit dicker Linie und runden Gelenken nach.
    Das klappt bei stumpfen Winkeln, aber die Spitze des Dreiecks ist
    sehr spitz — dort blieb eine sichtbare Kerbe, egal wie die Ecken
    eingerückt wurden.

    Der zweite, der hier steht: das scharfe Dreieck weichzeichnen und dann
    an einer Schwelle wieder hart machen. Weichzeichnen rundet jede Ecke
    von selbst, und zwar umso stärker, je spitzer sie ist — genau das will
    man. Die Schwelle läuft über ein paar Stufen statt auf einen Schlag,
    sonst wären die Kanten treppig.
    """
    rund = groesse * 0.075
    links = groesse * 0.300
    rechts = groesse * 0.772
    halb = groesse * 0.268
    mitte = groesse * 0.5

    # Etwas grösser zeichnen: das Weichzeichnen frisst ringsum ein Stück.
    wuchs = rund * 0.62
    ecken = [(rechts + wuchs * 1.5, mitte),
             (links - wuchs, mitte - halb - wuchs),
             (links - wuchs, mitte + halb + wuchs)]

    m = Image.new("L", (groesse, groesse), 0)
    ImageDraw.Draw(m).polygon(ecken, fill=255)
    m = m.filter(ImageFilter.GaussianBlur(rund * 0.55))

    # Schwelle mit weichem Saum: unter 110 nichts, über 145 alles, dazwischen
    # ein gleitender Übergang von einer guten Pixelbreite.
    return m.point(lambda v: 0 if v < 120 else (255 if v > 138 else
                                                round((v - 120) * 255 / 18)))


def strahl_farbe(groesse: int) -> Image.Image:
    """Der Verlauf in der Fläche: warmes Weiss oben links, kühles Blau
    unten rechts. Schräg, damit die Form plastisch wirkt und nicht wie ein
    ausgeschnittenes Stück Papier."""
    g = groesse
    bild = Image.new("RGB", (g, g))
    p = bild.load()
    for y in range(g):
        for x in range(0, g, 4):   # alle vier Spalten genügen, danach wird
            t = (x / g * 0.62 + y / g * 0.38)   # verkleinert
            t = min(max(t, 0.0), 1.0) ** 0.85
            farbe = tuple(round(a + (b - a) * t)
                          for a, b in zip(STRAHL_HELL, STRAHL_FERN))
            for dx in range(4):
                if x + dx < g:
                    p[x + dx, y] = farbe
    return bild.filter(ImageFilter.GaussianBlur(g / 200))


def symbol(groesse: int, mit_grund: bool = True, rand: float = 0.0) -> Image.Image:
    """Das fertige Symbol in der gewünschten Kantenlänge.

    rand lässt rundherum Luft — Android schneidet vom Vordergrund eines
    adaptiven Symbols fast ein Drittel weg.
    """
    g = groesse * UEBER
    bild = Image.new("RGBA", (g, g), (0, 0, 0, 0))

    if mit_grund:
        bild.paste(verlauf_senkrecht(g, GRUND_OBEN, GRUND_UNTEN), (0, 0))
        bild.putalpha(255)
        # Ein Schein hinter der Spitze, damit der Grund nicht tot wirkt.
        schein = radialer_schein(g, g * 0.47, g * 0.5, g * 0.52)
        farbe = Image.new("RGBA", (g, g), GLUT + (255,))
        bild = Image.alpha_composite(bild, Image.merge(
            "RGBA", (*farbe.split()[:3], schein.point(lambda v: v * 0.22))))

    inhalt = Image.new("RGBA", (g, g), (0, 0, 0, 0))
    maske = strahl_maske(g)

    # Erst ein weicher Abdruck des Strahls — das ist der Dunst um das Licht.
    hauch = maske.filter(ImageFilter.GaussianBlur(g * 0.022)).point(lambda v: v * 0.30)
    hauchfarbe = Image.new("RGBA", (g, g), (150, 200, 255, 255))
    inhalt = Image.alpha_composite(inhalt, Image.merge(
        "RGBA", (*hauchfarbe.split()[:3], hauch)))

    # Dann der Strahl selbst.
    inhalt = Image.alpha_composite(inhalt, Image.merge(
        "RGBA", (*strahl_farbe(g).split(), maske)))


    if rand > 0:
        klein = round(g * (1 - 2 * rand))
        verkleinert = inhalt.resize((klein, klein), Image.LANCZOS)
        inhalt = Image.new("RGBA", (g, g), (0, 0, 0, 0))
        inhalt.paste(verkleinert, ((g - klein) // 2, (g - klein) // 2))

    bild = Image.alpha_composite(bild, inhalt) if mit_grund else inhalt
    return bild.resize((groesse, groesse), Image.LANCZOS)


def gerundet(bild: Image.Image, anteil: float = 0.2237) -> Image.Image:
    """Ecken runden — für Android-Symbole, die nicht adaptiv sind.

    0.2237 ist das Verhältnis, mit dem iOS seine Ecken rundet; es sieht
    auch anderswo richtig aus.
    """
    g = bild.size[0]
    gross = g * UEBER
    m = Image.new("L", (gross, gross), 0)
    ImageDraw.Draw(m).rounded_rectangle([0, 0, gross - 1, gross - 1],
                                        radius=round(gross * anteil), fill=255)
    m = m.resize((g, g), Image.LANCZOS)
    aus = bild.copy()
    aus.putalpha(m)
    return aus


def banner(breite: int, hoehe: int) -> Image.Image:
    """Das Kästchen auf dem Startbildschirm eines Fernsehers.

    Zeichen und Schriftzug nebeneinander, weil dort Platz in die Breite
    ist. Die Schriftgrösse wird nicht geraten, sondern so gewählt, dass
    der Name hineinpasst — beim ersten Versuch stand da „Lumer", weil eine
    feste Grösse bei 320 Pixeln Breite zu gross war.
    """
    from PIL import ImageFont

    g = (breite * UEBER, hoehe * UEBER)
    bild = verlauf_senkrecht(max(g), GRUND_OBEN, GRUND_UNTEN).resize(g).convert("RGBA")

    rand = round(hoehe * UEBER * 0.08)
    zeichenhoehe = g[1] - 2 * rand
    zeichen = symbol(zeichenhoehe, mit_grund=False)

    # Auf einem sehr breiten Bild — dem Streifen oben auf dem Fernseher —
    # klebte alles am linken Rand und rechts gähnte Leere. Dort steht die
    # Marke darum in der Mitte.
    breit = g[0] / g[1] > 3
    links_start = rand
    if breit:
        links_start = round(g[0] * 0.26)
    bild.alpha_composite(zeichen, (links_start, rand))

    schriftdatei = None
    for pfad in ("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
                 "/usr/share/fonts/truetype/liberation/LiberationSans-Bold.ttf",
                 "/usr/share/fonts/truetype/freefont/FreeSansBold.ttf"):
        if os.path.exists(pfad):
            schriftdatei = pfad
            break
    if schriftdatei is None:
        return bild.convert("RGB").resize((breite, hoehe), Image.LANCZOS)

    d = ImageDraw.Draw(bild)
    text = "Lumen"
    x = links_start + zeichenhoehe - round(zeichenhoehe * 0.14)
    platz = g[0] - x - rand

    groesse = round(g[1] * 0.34)
    while groesse > 8:
        schrift = ImageFont.truetype(schriftdatei, groesse)
        k = d.textbbox((0, 0), text, font=schrift)
        if k[2] - k[0] <= platz:
            break
        groesse -= 2
    k = d.textbbox((0, 0), text, font=schrift)
    d.text((x - k[0], (g[1] - (k[3] + k[1])) / 2), text,
           font=schrift, fill=(240, 244, 255))

    return bild.convert("RGB").resize((breite, hoehe), Image.LANCZOS)


# ---------------------------------------------------------------- Ablegen

def schreibe(bild: Image.Image, pfad: pathlib.Path, modus="RGBA"):
    pfad.parent.mkdir(parents=True, exist_ok=True)
    bild.convert(modus).save(pfad, "PNG", optimize=True)
    print(f"  {pfad.relative_to(WURZEL)}  {bild.size[0]}×{bild.size[1]}")


def android():
    print("Android")
    res = WURZEL / "android/app/src/main/res"
    tvres = WURZEL / "android/tv/src/main/res"

    # Adaptive Symbole: Vordergrund und Hintergrund getrennt. Android
    # beschneidet den Vordergrund je nach Gerät rund oder eckig, darum
    # steht das Zeichen in der inneren Hälfte.
    for dichte, px in (("mdpi", 108), ("hdpi", 162), ("xhdpi", 216),
                       ("xxhdpi", 324), ("xxxhdpi", 432)):
        for ziel in (res, tvres):
            schreibe(symbol(px, mit_grund=False, rand=0.26),
                     ziel / f"mipmap-{dichte}/ic_launcher_foreground.png")
            schreibe(verlauf_senkrecht(px, GRUND_OBEN, GRUND_UNTEN).convert("RGBA"),
                     ziel / f"mipmap-{dichte}/ic_launcher_background.png")
            # Für alte Androide, die nichts Adaptives kennen.
            schreibe(gerundet(symbol(round(px * 48 / 108))),
                     ziel / f"mipmap-{dichte}/ic_launcher.png")
            schreibe(gerundet(symbol(round(px * 48 / 108)), anteil=0.5),
                     ziel / f"mipmap-{dichte}/ic_launcher_round.png")

    # Das Kästchen auf dem Fernseher.
    for dichte, (b, h) in (("xhdpi", (320, 180)), ("xxhdpi", (480, 270))):
        schreibe(banner(b, h), tvres / f"drawable-{dichte}/tv_banner.png", "RGB")

    # Für den Laden.
    schreibe(symbol(512), WURZEL / "android/symbol-512.png")


def apple():
    print("Apple")
    a = WURZEL / "apple/Lumen/Assets.xcassets/AppIcon.appiconset"
    # Seit Xcode 14 genügt ein einziges Bild für iOS; macOS will weiterhin
    # seine Grössen einzeln.
    schreibe(symbol(1024), a / "lumen-1024.png", "RGB")
    for px in (16, 32, 64, 128, 256, 512, 1024):
        schreibe(symbol(px), a / f"mac-{px}.png", "RGB")

    # Apple TV: das Symbol besteht aus Ebenen, die sich beim Darüberfahren
    # gegeneinander verschieben. Hier sind es zwei — Grund und Strahl.
    tv = WURZEL / "apple/LumenTV/Assets.xcassets"
    for name, (b, h) in (("App Icon - App Store", (1280, 768)),
                         ("App Icon", (400, 240))):
        ordner = tv / f"{name}.imagestack"
        schreibe(Image.new("RGBA", (b, h), GRUND_UNTEN + (255,)),
                 ordner / "Back.imagestacklayer/Content.imageset/back.png")
        zeichen = Image.new("RGBA", (b, h), (0, 0, 0, 0))
        s = symbol(h, mit_grund=False)
        zeichen.alpha_composite(s, ((b - h) // 2, 0))
        schreibe(zeichen, ordner / "Front.imagestacklayer/Content.imageset/front.png")

    # Das breite Bild ganz oben auf dem Fernseher.
    schreibe(banner(1920, 720), tv / "Top Shelf Image.imageset/topshelf.png", "RGB")


def web():
    print("Web")
    w = WURZEL / "server/cmd/lumen/web"
    schreibe(symbol(180), w / "apple-touch-icon.png", "RGB")
    schreibe(gerundet(symbol(192)), w / "symbol-192.png")
    schreibe(gerundet(symbol(512)), w / "symbol-512.png")
    # Das kleine Bild im Browser-Reiter. 32 Pixel sind wenig — der Strahl
    # bekommt dort etwas mehr Luft, sonst klebt er am Rand.
    schreibe(symbol(32), w / "favicon-32.png", "RGB")
    schreibe(symbol(16), w / "favicon-16.png", "RGB")


if __name__ == "__main__":
    android()
    apple()
    web()
    print("\nFertig.")
