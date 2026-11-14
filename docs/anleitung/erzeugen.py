# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
#
# Veröffentlicht unter der GNU Affero General Public License v3.0.
# Der vollständige Text steht in der Datei LICENSE.
# SPDX-License-Identifier: AGPL-3.0-only

"""Erzeugt docs/Lumen-Installationsanleitung.pdf.

Aufruf (aus dem Repository):  python3 docs/anleitung/erzeugen.py
Braucht reportlab (pip install reportlab). Die Schrift DejaVu wird
eingebettet, wenn sie auf dem System liegt; sonst Helvetica.
"""

import os
from datetime import date

from reportlab.lib import colors
from reportlab.lib.enums import TA_LEFT
from reportlab.lib.pagesizes import A4
from reportlab.lib.styles import ParagraphStyle
from reportlab.lib.units import mm
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.platypus import (KeepTogether, PageBreak, Paragraph, Preformatted,
                                SimpleDocTemplate, Spacer, Table, TableStyle)

HIER = os.path.dirname(os.path.abspath(__file__))
ZIEL = os.path.join(HIER, "..", "Lumen-Installationsanleitung.pdf")
REPO = "https://github.com/ada462790-beep/lumen-media"

# ------------------------------------------------------------------ Schriften
SCHRIFT, FETT, MONO = "Helvetica", "Helvetica-Bold", "Courier"
_dv = "/usr/share/fonts/truetype/dejavu/"
if os.path.exists(_dv + "DejaVuSans.ttf"):
    pdfmetrics.registerFont(TTFont("DV", _dv + "DejaVuSans.ttf"))
    pdfmetrics.registerFont(TTFont("DV-B", _dv + "DejaVuSans-Bold.ttf"))
    pdfmetrics.registerFont(TTFont("DV-M", _dv + "DejaVuSansMono.ttf"))
    SCHRIFT, FETT, MONO = "DV", "DV-B", "DV-M"

AKZENT = colors.HexColor("#0a84ff")
GRAU = colors.HexColor("#6e6e73")
FLAECHE = colors.HexColor("#f2f2f7")
WARN = colors.HexColor("#fff4e5")
WARN_RAND = colors.HexColor("#ff9f0a")

st = {
    "titel": ParagraphStyle("titel", fontName=FETT, fontSize=30, leading=36, spaceAfter=6),
    "unter": ParagraphStyle("unter", fontName=SCHRIFT, fontSize=13, leading=18, textColor=GRAU),
    "h1": ParagraphStyle("h1", fontName=FETT, fontSize=17, leading=22, spaceBefore=14, spaceAfter=6,
                         textColor=colors.black),
    "h2": ParagraphStyle("h2", fontName=FETT, fontSize=12.5, leading=17, spaceBefore=10, spaceAfter=3),
    "text": ParagraphStyle("text", fontName=SCHRIFT, fontSize=10.2, leading=15, spaceAfter=5, alignment=TA_LEFT),
    "klein": ParagraphStyle("klein", fontName=SCHRIFT, fontSize=8.6, leading=12, textColor=GRAU),
    "punkt": ParagraphStyle("punkt", fontName=SCHRIFT, fontSize=10.2, leading=15, leftIndent=14,
                            bulletIndent=2, spaceAfter=2),
    "code": ParagraphStyle("code", fontName=MONO, fontSize=7.9, leading=10.4),
    "zelle": ParagraphStyle("zelle", fontName=SCHRIFT, fontSize=9, leading=12.5),
    "zellefett": ParagraphStyle("zellefett", fontName=FETT, fontSize=9, leading=12.5),
}


def P(t, s="text"):
    return Paragraph(t, st[s])


def punkte(*zeilen):
    return [Paragraph(z, st["punkt"], bulletText="•") for z in zeilen]


def code(t):
    """Ein grauer Kasten mit Code, Zeilen unverändert."""
    pre = Preformatted(t.strip("\n"), st["code"])
    tab = Table([[pre]], colWidths=[170 * mm])
    tab.setStyle(TableStyle([
        ("BACKGROUND", (0, 0), (-1, -1), FLAECHE),
        ("BOX", (0, 0), (-1, -1), 0.4, colors.HexColor("#d1d1d6")),
        ("LEFTPADDING", (0, 0), (-1, -1), 8), ("RIGHTPADDING", (0, 0), (-1, -1), 8),
        ("TOPPADDING", (0, 0), (-1, -1), 6), ("BOTTOMPADDING", (0, 0), (-1, -1), 6),
    ]))
    return KeepTogether([tab, Spacer(1, 6)])


def hinweis(t, warnung=False):
    tab = Table([[P(t)]], colWidths=[170 * mm])
    tab.setStyle(TableStyle([
        ("BACKGROUND", (0, 0), (-1, -1), WARN if warnung else colors.HexColor("#eaf3ff")),
        ("LINEBEFORE", (0, 0), (0, -1), 3, WARN_RAND if warnung else AKZENT),
        ("LEFTPADDING", (0, 0), (-1, -1), 10), ("RIGHTPADDING", (0, 0), (-1, -1), 8),
        ("TOPPADDING", (0, 0), (-1, -1), 6), ("BOTTOMPADDING", (0, 0), (-1, -1), 4),
    ]))
    return KeepTogether([tab, Spacer(1, 8)])


def tabelle(kopf, zeilen, breiten):
    daten = [[P(k, "zellefett") for k in kopf]] + [[P(z, "zelle") for z in r] for r in zeilen]
    tab = Table(daten, colWidths=[b * mm for b in breiten], repeatRows=1)
    tab.setStyle(TableStyle([
        ("BACKGROUND", (0, 0), (-1, 0), FLAECHE),
        ("LINEBELOW", (0, 0), (-1, -1), 0.3, colors.HexColor("#d1d1d6")),
        ("VALIGN", (0, 0), (-1, -1), "TOP"),
        ("LEFTPADDING", (0, 0), (-1, -1), 5), ("RIGHTPADDING", (0, 0), (-1, -1), 5),
        ("TOPPADDING", (0, 0), (-1, -1), 4), ("BOTTOMPADDING", (0, 0), (-1, -1), 4),
    ]))
    return KeepTogether([tab, Spacer(1, 8)])


def fusszeile(c, doc):
    c.saveState()
    c.setFont(SCHRIFT, 8)
    c.setFillColor(GRAU)
    c.drawString(20 * mm, 12 * mm, "Lumen – Installationsanleitung für Synology")
    c.drawRightString(190 * mm, 12 * mm, f"Seite {doc.page}")
    c.restoreState()


COMPOSE = """
services:
  lumen:
    image: ghcr.io/ada462790-beep/lumen-media:latest
    pull_policy: always
    container_name: lumen
    restart: unless-stopped
    labels:
      - com.centurylinklabs.watchtower.enable=true
    stop_grace_period: 30s
    init: true
    # Im Netz des NAS: nur so sieht Lumen, wer zu Hause ist.
    network_mode: host
    volumes:
      - type: bind
        source: /volume1/docker/lumen/daten
        target: /data
        bind:
          create_host_path: false
      - /volume1/video:/volume1/video:rw
    environment:
      - TZ=Europe/Zurich
      - PUID=1026
      - PGID=100
      - LUMEN_ADDR=:8097
      - LUMEN_AKTUALISIERER_URL=http://127.0.0.1:8080
      - LUMEN_AKTUALISIERER_TOKEN=${LUMEN_AKTUALISIERER_TOKEN:-}

  # Optional: der Aktualisierer für den Knopf "Jetzt aktualisieren"
  # (Watchtower von Nick Fedor, Apache-2.0, eigenes Abbild vom Docker Hub
  # des Herausgebers - nicht Teil von Lumen). Ohne ihn diesen Block weglassen.
  aktualisierer:
    image: nickfedor/watchtower:1.24.0
    container_name: lumen-aktualisierer
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    ports:
      - "127.0.0.1:8080:8080"
    environment:
      - TZ=Europe/Zurich
      - WATCHTOWER_HTTP_API_ENDPOINTS=update
      - WATCHTOWER_HTTP_API_TOKEN=${LUMEN_AKTUALISIERER_TOKEN:-}
      - WATCHTOWER_LABEL_ENABLE=true
      - WATCHTOWER_CLEANUP=true
      - WATCHTOWER_TIMEOUT=30s
      - WATCHTOWER_NO_STARTUP_MESSAGE=true
"""


def inhalt():
    s = []
    # ---------------------------------------------------------- Titelseite
    s += [Spacer(1, 60 * mm), P("Lumen", "titel"),
          P("Installationsanleitung für Synology (DSM 7.2, Container Manager)", "unter"),
          Spacer(1, 10 * mm),
          P("Lumen ist ein eigener Streaming-Server für Filme und Serien im Heimnetz – "
            "ohne Konto bei einem Anbieter, ohne Werbung, mit Profilen für die ganze "
            "Familie. Diese Anleitung führt Schritt für Schritt durch die Einrichtung "
            "auf einem Synology-NAS, ganz ohne Terminal."),
          Spacer(1, 6 * mm),
          P(f"Quellcode, Fragen und die neueste Fassung dieser Anleitung: "
            f"<link href='{REPO}' color='#0a84ff'>{REPO}</link>"),
          Spacer(1, 50 * mm),
          P(f"Stand: {date.today().strftime('%d.%m.%Y')} · Lumen ist freie Software unter der "
            f"GNU AGPL-3.0.", "klein"),
          PageBreak()]

    # ---------------------------------------------------------- Überblick
    s += [P("Überblick", "h1"),
          P("Die Einrichtung dauert etwa 20 Minuten. In dieser Reihenfolge:")]
    s += [tabelle(["Schritt", "Was", "Wo"], [
        ["1", "Ordner für Lumen anlegen", "File Station"],
        ["2", "Benutzernummer herausfinden (PUID/PGID)", "Aufgabenplaner"],
        ["3", "Schlüssel für Updates per Knopfdruck anlegen (optional)", "Aufgabenplaner"],
        ["4", "Projekt im Container Manager erstellen", "Container Manager"],
        ["5", "Firewall: Port 8097 fürs Heimnetz freigeben", "Systemsteuerung"],
        ["6", "Erster Start: Besitzer anlegen, Bibliotheken hinzufügen", "Browser"],
        ["7", "TMDb-Schlüssel für Poster und Beschreibungen", "Browser"],
        ["8", "Familie einladen, Apps und Fernseher verbinden", "Browser / Apps"],
    ], [18, 110, 42])]
    s += [P("Was du brauchst", "h2")]
    s += punkte(
        "Ein Synology-NAS mit Intel/AMD-Prozessor (die meisten „+“-Modelle) oder 64-Bit-ARM, "
        "auf dem der <b>Container Manager</b> läuft (Paket-Zentrum). Welcher Prozessor drin ist: "
        "<i>Systemsteuerung › Info-Center</i>.",
        "Deine Filme und Serien in einem freigegebenen Ordner, zum Beispiel <b>video</b> "
        "(<font face='%s'>/volume1/video</font>)." % MONO,
        "Einen Browser im Heimnetz. Für die Verwaltung braucht es einen Computer; "
        "auf Handy und Tablet ist Lumen zum Schauen da.")
    s += [hinweis("Tipp zur Ordnung: Filme in einen Ordner (etwa <b>_filme</b>), Serien in einen "
                  "anderen (<b>_serien</b>), jede Serie in einem eigenen Ordner mit Unterordnern "
                  "<b>Staffel 1</b>, <b>Staffel 2</b>. Lumen erkennt auch andere Schemata, aber so "
                  "klappt es am zuverlässigsten.")]

    # ---------------------------------------------------------- 1 Ordner
    s += [P("1. Ordner anlegen", "h1"),
          P("In der <b>File Station</b> im freigegebenen Ordner <b>docker</b> den Ordner "
            "<b>lumen</b> anlegen und darin <b>daten</b>. Gibt es den Ordner <b>docker</b> "
            "noch nicht, legst du ihn unter <i>Systemsteuerung › Freigegebener Ordner</i> an."),
          code("""
/volume1/docker/lumen          hier liegt die Projektdatei (docker-compose.yml)
/volume1/docker/lumen/daten    Datenbank, Poster, tägliche Sicherung
""")]

    # ---------------------------------------------------------- 2 PUID
    s += [P("2. Benutzernummer herausfinden (PUID und PGID)", "h1"),
          P("Lumen läuft im Container nie als Administrator, sondern als ein normaler "
            "Benutzer deines NAS. Dieser Benutzer braucht im Ordner mit den Filmen "
            "<b>Lesen/Schreiben</b> (Schreiben nur, damit Lumen Filme in Teilen – CD1/CD2 – "
            "auf Wunsch zu einer Datei zusammenfügen kann; sonst schreibt es dort nichts)."),
          P("Auf einem Synology hat der erste selbst angelegte Benutzer meist die Nummer "
            "<b>1026</b>, die Gruppe <i>users</i> ist <b>100</b>. So findest du es sicher heraus:")]
    s += punkte(
        "<i>Systemsteuerung › Aufgabenplaner › Erstellen › Geplante Aufgabe › "
        "Benutzerdefiniertes Skript</i>, Name beliebig, Benutzer <b>root</b>.",
        "Unter <i>Aufgabeneinstellungen</i> dieses Skript eintragen (deinen NAS-Benutzernamen "
        "einsetzen):")
    s += [code("id DEIN_NAS_BENUTZER > /volume1/docker/lumen/id.txt")]
    s += punkte(
        "Speichern, die Aufgabe markieren, <i>Ausführen</i>. In der File Station "
        "<font face='%s'>docker/lumen/id.txt</font> öffnen: dort steht etwa "
        "<font face='%s'>uid=1026(...) gid=100(users)</font>." % (MONO, MONO),
        "Danach Aufgabe und Datei löschen.",
        "Rechte prüfen: <i>Systemsteuerung › Freigegebener Ordner › video › Bearbeiten › "
        "Berechtigungen</i> – der Benutzer braucht <i>Lesen/Schreiben</i>.")

    # ---------------------------------------------------------- 3 Token
    s += [P("3. Schlüssel für Updates per Knopfdruck (optional)", "h1"),
          P("Im Container kann sich Lumen nicht selbst austauschen. Wer Updates später mit "
            "einem Knopf in der Verwaltung (oder nachts von selbst) machen will, braucht einen "
            "kleinen Begleiter, den <b>Aktualisierer</b>, und einen geheimen Schlüssel, mit dem "
            "Lumen ihm Bescheid gibt. Ohne ihn geht alles genauso – Updates machst du dann im "
            "Container Manager (Abschnitt 9)."),
          P("Den Schlüssel legt wieder eine Aufgabe im Aufgabenplaner an (Benutzer <b>root</b>), "
            "mit diesem Skript:"),
          code("""
grep -qs LUMEN_AKTUALISIERER_TOKEN /volume1/docker/lumen/.env || \\
  echo "LUMEN_AKTUALISIERER_TOKEN=$(openssl rand -hex 32)" >> /volume1/docker/lumen/.env
"""),
          P("Einmal <i>Ausführen</i>, danach kann die Aufgabe weg. In "
            "<font face='%s'>/volume1/docker/lumen/.env</font> steht jetzt eine zufällige, "
            "64-stellige Zeichenfolge. Diese Datei niemandem geben." % MONO),
          hinweis("<b>Fremde Software:</b> Der Aktualisierer ist <b>nicht Teil von Lumen</b>. Es ist "
                  "<b>Watchtower</b> in der gepflegten Fassung von Nick Fedor, veröffentlicht unter "
                  "der Apache-Lizenz 2.0. Der Container Manager lädt ihn direkt vom Herausgeber "
                  "(Docker Hub <font face='%s'>nickfedor/watchtower</font>, Quelle: "
                  "<link href='https://github.com/nicholas-fedor/watchtower' color='#0a84ff'>"
                  "github.com/nicholas-fedor/watchtower</link>). Nur er bekommt Zugriff auf Docker; "
                  "Lumen kann ihm mit dem Schlüssel genau eines sagen: „aktualisiere jetzt“. "
                  "Er tauscht nur Container mit dem Lumen-Etikett aus." % MONO)]

    # ---------------------------------------------------------- 4 Projekt
    s += [PageBreak(), P("4. Projekt im Container Manager erstellen", "h1")]
    s += punkte(
        "<i>Container Manager › Projekt › Erstellen</i>.",
        "Projektname <b>lumen</b>, Pfad <b>/docker/lumen</b>, Quelle "
        "<i>docker-compose.yml erstellen</i>.",
        "Diesen Inhalt hineinkopieren und die markierten Stellen anpassen:")
    s += [code(COMPOSE)]
    s += [tabelle(["Zeile", "Anpassen"], [
        ["<font face='%s'>/volume1/video:/volume1/video:rw</font>" % MONO,
         "Links und rechts der Pfad zu deinen Filmen, so wie die File Station ihn zeigt. "
         "Weitere Ordner als weitere Zeilen darunter (gleich eingerückt)."],
        ["PUID / PGID", "Die Nummern aus Schritt 2."],
        ["TZ", "Deine Zeitzone. Wichtig für Bildschirmzeit und Zeitpläne."],
        ["network_mode: host", "So stehen lassen. Lumen läuft damit im Netz des NAS und "
         "sieht die echten Adressen der Geräte – nur so weiss es, wer zu Hause ist (Profilauswahl, "
         "Übernahme aus Emby). Einen Block „ports:“ gibt es darum nicht."],
        ["aktualisierer: …", "Nur mit Schritt 3. Ohne Aktualisierer den ganzen Block ab "
         "„# Optional“ weglassen. Ist Port 8080 auf dem NAS belegt: links eine andere Zahl "
         "(etwa 127.0.0.1:18080:8080) und bei Lumen http://127.0.0.1:18080."],
    ], [62, 108])]
    s += punkte(
        "Ein Webportal über Web Station braucht es nicht. <i>Weiter › Fertig</i> – der "
        "Container Manager lädt das Abbild und startet Lumen.")
    s += [hinweis("Es lädt nicht? Prüfe den Namen <font face='%s'>ghcr.io/ada462790-beep/"
                  "lumen-media:latest</font> (mit <b>-media</b>). Steht unter <i>Container Manager › "
                  "Registry</i> eine alte Anmeldung für ghcr.io, entfernen und neu <i>Erstellen</i>. "
                  "Das Abbild ist öffentlich, es braucht keine Anmeldung." % MONO, warnung=True)]

    # ---------------------------------------------------------- 5 Firewall
    s += [P("5. Firewall", "h1"),
          P("Ist die Firewall des NAS eingeschaltet (<i>Systemsteuerung › Sicherheit › Firewall</i>), "
            "unter <i>Regeln bearbeiten</i> eine Regel anlegen: Port <b>8097/TCP</b>, Quelle "
            "<b>nur dein Heimnetz</b> (zum Beispiel <font face='%s'>192.168.1.0/24</font>), "
            "<i>Zulassen</i>. Weil Lumen im Netz des NAS läuft (<font face='%s'>network_mode: "
            "host</font>), gilt die Firewall für Lumen wie für jeden anderen Dienst – ohne diese "
            "Regel kommt bei eingeschalteter Firewall niemand an Lumen heran. Lumen nie "
            "ungeschützt ins Internet stellen und Port 8097 nicht im Router freigeben – für "
            "unterwegs gibt es einen eigenen, sicheren Weg (siehe README, „Von unterwegs "
            "schauen“)." % (MONO, MONO))]

    # ---------------------------------------------------------- 6 Erster Start
    s += [P("6. Erster Start", "h1"),
          P("Im Browser <font face='%s'>http://ADRESSE-DES-NAS:8097</font> öffnen "
            "(die Adresse steht in <i>Systemsteuerung › Netzwerk</i>)." % MONO)]
    s += punkte(
        "<b>Sprache und Region</b> wählen (Deutsch Schweiz, Deutschland, Österreich, Englisch).",
        "Dich als <b>Besitzer</b> anlegen, mit PIN oder Passwort. Der Besitzer verwaltet alles.",
        "<b>Bibliothek hinzufügen:</b> <i>Durchsuchen</i> zeigt oben die Ordner, die du in der "
        "Projektdatei freigegeben hast. Eine Bibliothek für Filme, eine für Serien – je mit "
        "dem passenden Ordner. Lumen prüft die Ordner sofort; bei grossen Sammlungen dauert der "
        "erste Durchlauf eine Weile.",
        "Nicht erkannte Titel stehen danach unter <i>Verwaltung › Aufräumen</i>, mit dem Grund. "
        "Dort lässt sich jeder Titel über <b>ID eingeben</b> von Hand zuordnen: IMDb-, TMDb- oder "
        "TheTVDB-Nummer oder einfach den Link von der jeweiligen Webseite einfügen.")

    # ---------------------------------------------------------- 7 TMDb
    s += [P("7. TMDb-Schlüssel für Poster und Beschreibungen", "h1"),
          P("Poster, Beschreibungen, Besetzung, Folgentitel und die Vorschläge beim Wünschen "
            "kommen von <b>TMDb</b> (The Movie Database). Der Schlüssel ist kostenlos:")]
    s += punkte(
        "Auf <link href='https://www.themoviedb.org' color='#0a84ff'>themoviedb.org</link> ein "
        "Konto anlegen.",
        "<i>Einstellungen › API</i> – einen API-Zugang beantragen (private Nutzung).",
        "Den langen <b>API-Lesezugriffstoken</b> (API Read Access Token) kopieren – nicht den "
        "kurzen API-Schlüssel.",
        "In Lumen unter <i>Verwaltung › Metadaten</i> einfügen. Lumen prüft ihn beim Speichern.")
    s += [P("Ohne Schlüssel geht es auch: Liegen von Emby, Jellyfin oder Kodi NFO-Dateien und "
            "Poster neben den Filmen, liest Lumen Titel, Beschreibung, Altersfreigabe, Besetzung "
            "und Sammlungen daraus.", "klein"),
          P("Dieses Produkt nutzt die TMDb-API, ist aber nicht von TMDb unterstützt oder "
            "zertifiziert.", "klein")]

    # ---------------------------------------------------------- 8 Familie
    s += [P("8. Familie, Apps und Fernseher", "h1")]
    s += punkte(
        "<b>Profile:</b> <i>Verwaltung › Konten</i> – Einladungen für Familienmitglieder, mit "
        "Kinderprofil, Altersgrenze und Bibliotheken. Offene Einladungen lassen sich dort sehen "
        "und zurückziehen.",
        "<b>Bildschirmzeit</b> für Kinder: erlaubte Zeiten an Schultagen und am Wochenende, "
        "Höchstdauer pro Tag, „heute einmalig mehr“. Lumen setzt sie selbst durch.",
        "<b>Privatsphäre:</b> Niemand – auch der Besitzer nicht – sieht, wer was wann schaut. "
        "Bei Kindern sieht die Verwaltung nur ungefähr, wie viel heute geschaut wurde.",
        "<b>Apps:</b> iPhone, iPad, Mac, Apple TV, Android und Android TV verbinden sich mit der "
        "Adresse deines NAS. Am Fernseher meldet man sich mit einem 6-stelligen Code an, den man "
        "auf dem Handy oder im Browser bestätigt.")

    # ---------------------------------------------------------- 9 Updates
    s += [P("9. Updates", "h1"),
          P("<b>Mit Aktualisierer (Schritt 3):</b> <i>Verwaltung › Updates</i> zeigt, wenn eine "
            "neue Fassung da ist, den Knopf <b>Jetzt aktualisieren</b>. Lumen fragt nach, falls "
            "gerade jemand schaut, sichert die Datenbank und startet mit der neuen Fassung neu. "
            "Unter <i>Von selbst aktualisieren</i> lässt sich ein Zeitfenster in der Nacht "
            "einstellen – dann nur, wenn niemand schaut."),
          P("<b>Ohne Aktualisierer:</b> <i>Container Manager › Projekt › lumen › Aktion › "
            "Erstellen</i>. Weil <font face='%s'>pull_policy: always</font> in der Datei steht, "
            "holt er dabei das neue Abbild; deine Daten bleiben." % MONO),
          P("Vor jedem Umbau der Datenbank legt Lumen selbst eine Sicherung an.", "klein")]

    # ---------------------------------------------------------- 10 Sicherung
    s += [P("10. Sichern und PIN vergessen", "h1")]
    s += punkte(
        "<b>Sicherung:</b> Lumen sichert die Datenbank täglich in "
        "<font face='%s'>daten/sicherungen</font> (zwei Generationen). Wichtig zum Mitsichern, "
        "etwa mit Hyper Backup: der ganze Ordner <font face='%s'>daten</font> – vor allem "
        "<font face='%s'>lumen.db</font>, <font face='%s'>sicherungen/</font> und, wenn du "
        "HTTPS oder den Fernzugriff nutzt, die beiden Zertifikatsdateien." % (MONO, MONO, MONO, MONO),
        "<b>Wiederherstellen:</b> <i>Verwaltung › Sicherung</i> zeigt jede Sicherung mit Datum und "
        "Uhrzeit.",
        "<b>PIN eines Familienmitglieds vergessen:</b> <i>Verwaltung › Konten › PIN zurücksetzen</i>. "
        "Lumen zeigt einmal einen 6-stelligen Einmal-Code; damit legt die Person ihre neue PIN "
        "selbst fest. Dabei wird ihr Verlauf gelöscht.",
        "<b>Eigene Besitzer-PIN vergessen:</b> geht absichtlich nur auf dem NAS selbst – ohne SSH "
        "mit einer Aufgabe im Aufgabenplaner (Benutzer root):")
    s += [code("printf '2580\\n2580\\n' | /usr/local/bin/docker exec -i lumen lumen zuruecksetzen "
               "--konto DEIN_NAME --verwaltung \\\n  > /volume1/docker/lumen/zuruecksetzen.txt 2>&1"),
          P("Die neue PIN steht zweimal im Skript. Einmal ausführen, "
            "<font face='%s'>zuruecksetzen.txt</font> prüfen (dort steht „Erledigt“), dann Aufgabe "
            "und Datei löschen und in Lumen gleich eine eigene PIN wählen." % MONO, "klein")]

    # ---------------------------------------------------------- 11 Probleme
    s += [P("11. Wenn etwas nicht klappt", "h1")]
    s += [tabelle(["Was du siehst", "Was hilft"], [
        ["Seite lädt nicht", "Läuft der Container (Container Manager › Container)? Firewall-Regel "
         "für 8097? Richtige Adresse und Port?"],
        ["„Geht nur zu Hause im Heimnetz“, obwohl du zu Hause bist",
         "<i>Verwaltung › Dieses Gerät</i> zeigt, welche Adresse Lumen sieht und warum. Steht dort "
         "ein Gateway wie 172.17.0.1, fehlt <font face='%s'>network_mode: host</font> in der "
         "Projektdatei (alte Fassung mit „ports:“): Projekt stoppen, den Block „ports:“ durch "
         "„network_mode: host“ ersetzen wie in Schritt 4, dann Aktion › Erstellen." % MONO],
        ["Bibliothek bleibt leer", "Darf der Benutzer aus PUID/PGID den Ordner lesen? Ist der "
         "Ordner unter volumes freigegeben? Lumen sagt es beim „Durchsuchen“."],
        ["Keine Poster, Titel wie Dateinamen", "TMDb-Schlüssel fehlt oder ist der kurze statt des "
         "langen. Aufräumen zeigt oben einen Hinweis."],
        ["Film steht doppelt oder falsch", "Aufräumen › ID eingeben, oder in den Dateinamen die "
         "Nummer schreiben, etwa <font face='%s'>Alien (1979) {tmdb-348}</font>." % MONO],
        ["Knopf „Jetzt aktualisieren“ fehlt", "Aktualisierer nicht eingerichtet, oder Schlüssel "
         "in .env fehlt. Die Updates-Seite sagt, was fehlt."],
        ["Etwas anderes", f"Protokoll unter Verwaltung › Protokoll. Fragen und Fehler: "
         f"<link href='{REPO}/issues' color='#0a84ff'>{REPO}/issues</link>"],
    ], [52, 118])]

    # ---------------------------------------------------------- Lizenzen
    s += [P("Lizenzen und fremde Software", "h1"),
          P("<b>Lumen</b> ist freie Software unter der GNU Affero General Public License v3.0 – "
            "du darfst es benutzen, weitergeben und verändern; wer eine veränderte Fassung anderen "
            "anbietet, muss deren Quellcode ebenfalls offenlegen."),
          P("Lumen nutzt Software von anderen. Das Docker-Abbild enthält unter anderem "
            "<b>FFmpeg</b> und weitere Pakete aus Alpine Linux, jeweils unter ihren eigenen "
            "Lizenzen. Der optionale <b>Aktualisierer</b> (Watchtower, Apache-2.0) ist nicht Teil "
            "von Lumen und wird separat vom Herausgeber geladen. Die vollständige Liste mit "
            "Lizenzen und Quellen steht im Repository in <b>DRITTSOFTWARE.md</b>.")]
    return s


def main():
    doc = SimpleDocTemplate(ZIEL, pagesize=A4, leftMargin=20 * mm, rightMargin=20 * mm,
                            topMargin=18 * mm, bottomMargin=20 * mm,
                            title="Lumen – Installationsanleitung", author="Die Lumen-Autoren",
                            subject="Lumen auf einem Synology-NAS einrichten")
    doc.build(inhalt(), onFirstPage=lambda c, d: None, onLaterPages=fusszeile)
    print("geschrieben:", os.path.normpath(ZIEL))


if __name__ == "__main__":
    main()
