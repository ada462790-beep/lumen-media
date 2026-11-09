# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
#
# Veröffentlicht unter der GNU Affero General Public License v3.0.
# Der vollständige Text steht in der Datei LICENSE.
# SPDX-License-Identifier: AGPL-3.0-only

# Oberflächentest (Playwright) für Handbearbeitung, Sammlungen, Untertitel
# und die Übernahme aus Emby/Jellyfin.
#
# Vorbereitung:
#   1. go run ./cmd/lumen-nachbau $ARBEIT/nachbau.env   (Nachbau von OpenSubtitles und Jellyfin)
#   2. Lumen mit LUMEN_OPENSUBTITLES_URL=<OS aus nachbau.env>, Daten in $ARBEIT/funktionen-daten,
#      Filme (mit „Sonne (2020).mkv") in $ARBEIT/funktionen-medien/filme, Profil Alex.
#   3. LUMEN_TEST_ARBEIT=$ARBEIT LUMEN_TEST_URL=http://127.0.0.1:18661/ python3 tools/oberflaeche_funktionen.py
# Der Test setzt den Stand der Datenbank zu Beginn zurück und ist wiederholbar.
import os, struct, zlib
from playwright.sync_api import sync_playwright, expect
SP=os.environ.get("LUMEN_TEST_ARBEIT", ".")
URL=os.environ.get("LUMEN_TEST_URL", "http://127.0.0.1:18661/")
env=dict(l.strip().split("=",1) for l in open(SP+"/nachbau.env") if "=" in l)
MEDIEN=SP+"/funktionen-medien/filme"

def png(w,h):
    raw=b"".join(b"\x00"+b"".join(bytes([(x*3)%256,(y*2)%256,160]) for x in range(w)) for y in range(h))
    def ch(t,d): return struct.pack(">I",len(d))+t+d+struct.pack(">I",zlib.crc32(t+d)&0xffffffff)
    return b"\x89PNG\r\n\x1a\n"+ch(b"IHDR",struct.pack(">IIBBBBB",w,h,8,2,0,0,0))+ch(b"IDAT",zlib.compress(raw))+ch(b"IEND",b"")
open(SP+"/poster.png","wb").write(png(200,300))

def anmelden(pg):
    pg.goto(URL)
    pg.click("[data-pick]:has-text('Alex')")
    pg.fill("input[name=secret]", "einsehrgutespasswort")
    pg.press("input[name=secret]", "Enter")
    pg.wait_for_selector(".hero")

def zu_sonne(pg):
    pg.click(".sidebar [data-go=movies]")
    pg.click(".poster:has-text('Sonne')")
    pg.wait_for_selector("h1.t-hero")

# Ausgangslage herstellen, damit der Test wiederholbar ist.
import sqlite3, glob
db=sqlite3.connect(SP+"/funktionen-daten/lumen.db")
db.executescript("""DELETE FROM sammlung; DELETE FROM feld_sperre; DELETE FROM favorit; DELETE FROM progress; DELETE FROM rating;
  DELETE FROM artwork WHERE path LIKE '%-hand-%';
  DELETE FROM genre WHERE item_id IN (SELECT id FROM item WHERE title LIKE 'Sonne%');
  UPDATE item SET title='Sonne', sort_title='sonne', overview=NULL, match_state='pending' WHERE title LIKE 'Sonne%';
  DELETE FROM setting WHERE key LIKE 'untertitel.%';""")
db.commit(); db.close()
for f in glob.glob(MEDIEN+"/*.srt"): os.remove(f)

fehler=[]
with sync_playwright() as p:
    b=p.chromium.launch()
    pg=b.new_page(viewport={"width":1400,"height":900})
    pg.on("pageerror", lambda e: fehler.append(str(e)))
    anmelden(pg)
    zu_sonne(pg)

    # 1. Bearbeiten
    pg.click("[data-fk-bearbeiten]")
    d=pg.locator("dialog.fenster")
    d.locator("[name=title]").fill("Sonne über Zürich")
    d.locator("[name=overview]").fill("Ein Sommer am See.")
    d.locator("[data-genre-eingabe]").fill("Drama")
    d.locator("[data-genre-eingabe]").press("Enter")
    expect(d.locator("[data-speichern]")).to_have_text("3 Änderungen speichern")
    d.locator("input[data-bild-datei=poster]").set_input_files(SP+"/poster.png")
    expect(d.locator("[data-meldung]")).to_contain_text("Bild gesetzt")
    expect(d.locator("[data-bildbox=poster] img")).to_be_visible()
    d.locator("[data-speichern]").click()
    expect(d).to_have_count(0)
    expect(pg.locator("h1.t-hero")).to_have_text("Sonne über Zürich")
    pg.screenshot(path=SP+"/1-nach-bearbeiten.png")
    # Sperren sichtbar und einzeln aufhebbar
    pg.click("[data-fk-bearbeiten]")
    expect(d.locator("[data-schloss=title]")).to_be_visible()
    expect(d.locator("[data-schloss=poster]")).to_be_visible()
    d.locator("[data-schloss=overview]").click()
    expect(d.locator("[data-schloss=overview]")).to_have_count(0)
    expect(d.locator("[data-meldung]")).to_contain_text("Sperre ist aufgehoben")
    pg.screenshot(path=SP+"/2-sperren.png")
    pg.keyboard.press("Escape")
    expect(d).to_have_count(0)
    # Fehlerfall: leerer Titel
    pg.click("[data-fk-bearbeiten]")
    d.locator("[name=title]").fill("")
    d.locator("[data-speichern]").click()
    expect(d.locator("[data-meldung]")).to_contain_text("nicht leer")
    d.locator("[name=ageRating]").fill("quatsch")
    d.locator("[name=title]").fill("Sonne über Zürich")
    d.locator("[data-speichern]").click()
    expect(d.locator("[data-meldung]")).to_contain_text("Altersfreigabe")
    pg.keyboard.press("Escape")

    # 2. Sammlungen
    pg.click("[data-fk-sammlung]")
    d.locator("[name=name]").fill("Sommerfilme")
    d.locator("[data-sam-neuform] button").click()
    expect(d.locator("[data-sam-wahl][aria-pressed=true]")).to_have_count(1)
    pg.keyboard.press("Escape")
    pg.click(".sidebar [data-go=collections]")
    pg.click(".reiter [data-go=sammlungen]")
    expect(pg.locator(".reihe-karte:has-text('Sommerfilme')")).to_be_visible()
    pg.screenshot(path=SP+"/3-sammlungen.png")
    pg.click(".reihe-karte:has-text('Sommerfilme')")
    expect(pg.locator(".poster:has-text('Sonne über Zürich')")).to_be_visible()
    pg.click("[data-sam-bearbeiten]")
    d.locator("[name=beschreibung]").fill("Für warme Abende.")
    d.locator("[name=aufStart]").check()
    d.locator("[data-sam-bild]").set_input_files(SP+"/poster.png")
    expect(d.locator("[data-meldung]")).to_contain_text("Bild gesetzt")
    d.locator("button[type=submit]").click()
    expect(d).to_have_count(0)
    expect(pg.locator("text=Für warme Abende.")).to_be_visible()
    pg.wait_for_function("() => { const i = document.querySelector('header img.art-img'); return i && i.complete && i.naturalWidth > 0 }")
    pg.screenshot(path=SP+"/4-sammlung.png")
    pg.click(".sidebar [data-go=home]")
    expect(pg.locator("h2:has-text('Sommerfilme')")).to_be_visible()

    # 3. Untertitel: erst einrichten
    pg.click(".sidebar [data-go=admin]")
    pg.click("[data-go=metadata]")
    pg.click("[data-fk-ut-verwaltung]")
    d.locator("[name=apiKey]").fill("schluessel-123")
    d.locator("button.btn-primary").click()
    expect(d.locator("[data-meldung]")).to_contain_text("OpenSubtitles antwortet")
    pg.screenshot(path=SP+"/5-ut-verwaltung.png")
    pg.keyboard.press("Escape")
    zu_sonne(pg)
    pg.click("[data-fk-untertitel]")
    # Der Titel ist jetzt von Hand geändert — der Nachbau kennt nur „Sonne".
    expect(d.locator(".wahl-leer")).to_contain_text("Nichts gefunden")
    d.locator("[name=name]").fill("Sonne")
    d.locator("[data-ut-form] .btn-primary").click()
    expect(d.locator("[data-ut-holen]").first).to_be_visible()
    pg.screenshot(path=SP+"/6-ut-suche.png")
    d.locator("[data-ut-holen]").first.click()
    expect(d.locator("[data-meldung]")).to_contain_text("jetzt als Spur da")
    assert os.path.exists(MEDIEN+"/Sonne (2020).de.srt"), "Sidecar fehlt"
    pg.keyboard.press("Escape")
    # Im Player: Eintrag im Untertitel-Menü
    pg.click("[data-play]")
    pg.wait_for_selector(".player")
    pg.mouse.move(700,450)
    pg.click("[data-spuren=untertitel]")
    expect(pg.locator("[data-fk-player-untertitel]")).to_be_visible()
    expect(pg.locator(".spuren .spur:has-text('Deutsch')")).to_be_visible()
    pg.screenshot(path=SP+"/7-player-menue.png")
    pg.click("[data-fk-player-untertitel]")
    expect(d).to_be_visible()
    pg.keyboard.press("Escape")
    expect(pg.locator(".player")).to_be_visible()   # Esc schliesst das Fenster, nicht den Film
    pg.click("[data-stop]")

    # 4. Übernahme (Ich-Bereich)
    pg.click(".sidebar [data-go=settings]")
    pg.click("[data-fk-uebernahme]")
    d.locator("[name=adresse]").fill(env["EMBY"])
    d.locator("[data-ue1] button").click()
    d.locator("[name=benutzer]").fill("alex")
    d.locator("[name=passwort]").fill("falsch")
    d.locator("[data-ue2] button.btn-primary").click()
    expect(d.locator("[data-meldung]")).to_contain_text("Passwort stimmt nicht")
    d.locator("[name=passwort]").fill("geheim")
    d.locator("[data-ue2] button.btn-primary").click()
    expect(d.locator(".zahlen")).to_be_visible()
    pg.screenshot(path=SP+"/8-uebernahme-vorschau.png")
    # „Gibt es nicht" fehlt wirklich; „Sonne" heisst in Lumen inzwischen anders.
    expect(d.locator(".zahl:has-text('nicht gefunden') b")).to_have_text("2")
    expect(d.locator(".zahl:has-text('gesehen') b").first).to_have_text("1")
    d.locator("[data-ue3] button.btn-primary").click()
    expect(d.locator(".fenster-meldung--gut")).to_contain_text("Übernommen")
    pg.screenshot(path=SP+"/9-uebernahme-fertig.png")
    d.locator(".fenster-fuss [data-fenster-zu]").click()

    # 5. Handy: keine Verwaltung, aber Übernahme
    ctx=b.new_context(viewport={"width":390,"height":844}, has_touch=True, is_mobile=True)
    m=ctx.new_page()
    m.on("pageerror", lambda e: fehler.append(str(e)))
    anmelden(m)
    m.goto(URL)
    m.wait_for_selector(".hero")
    m.click(".tabbar [data-go=movies]")
    m.click(".poster:has-text('Sonne')")
    m.wait_for_selector("h1.t-hero")
    assert m.locator("[data-fk-bearbeiten]").count()==0, "Bearbeiten auf dem Handy"
    expect(m.locator("[data-fk-untertitel]")).to_be_visible()
    m.click(".tabbar [data-go=settings]")
    m.click("[data-fk-uebernahme]")
    expect(m.locator("dialog.fenster [name=adresse]")).to_be_visible()
    m.screenshot(path=SP+"/10-handy-uebernahme.png")
    b.close()
print("Seitenfehler:", fehler)
assert not fehler
print("OK")
