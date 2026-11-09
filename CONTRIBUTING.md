# Mitmachen bei Lumen

Schön, dass du helfen willst. Diese Seite sagt, wie ein Beitrag aussieht,
der schnell aufgenommen werden kann. *English speakers: issues and pull
requests in English are welcome; code comments and identifiers stay German
(see below).*

## Bevor du anfängst

- **Fehler und Wünsche** gehören in ein
  [Issue](https://github.com/ada462790-beep/lumen-media/issues/new/choose) —
  bitte die Vorlagen benutzen.
- **Sicherheitslücken nie öffentlich melden**, sondern vertraulich über
  *Security › Report a vulnerability* — siehe [SECURITY.md](SECURITY.md).
- **Grössere Änderungen** (neue Funktion, Umbau, neue Abhängigkeit) erst in
  einem Issue besprechen. Das erspart dir Arbeit, die dann nicht passt.
- Es gilt der [Verhaltenskodex](CODE_OF_CONDUCT.md).

## Die Beitragsvereinbarung (CLA)

Jeder Pull-Request braucht deine Zustimmung zur [CLA](CLA.md). Kurz: Du
behältst das Urheberrecht an deinem Beitrag, und die Lumen-Autoren dürfen
ihn unter der AGPL-3.0 und auch unter anderen Lizenzen weitergeben. Du
stimmst zu, indem du in der Pull-Request-Beschreibung das Häkchen

```
- [x] Ich habe die CLA gelesen und stimme zu
```

setzt. Der Ablauf „CLA" prüft das; ohne Häkchen bleibt er rot. Nachträglich
setzen genügt — danach den Ablauf neu starten oder einen neuen Stand
schieben.

## Ablauf

1. Repository forken, einen Zweig anlegen, ändern.
2. Lokal prüfen (siehe unten) — dieselben Prüfungen laufen im Pull-Request.
3. Pull-Request gegen `main` öffnen, die Vorlage ausfüllen, CLA-Häkchen
   setzen.

In Pull-Requests laufen „Prüfen", „Apps übersetzen" (nur bei Änderungen
unter `android/` oder `apple/`) und „CLA" — alle nur mit Leserechten und
ohne Geheimnisse. Ein Fork braucht dafür nichts einzurichten.

## Lokal prüfen

```bash
# Server: bauen, prüfen, testen (Go 1.24; FFmpeg für die Umwandlungstests)
cd server && gofmt -l . | grep -v '^vendor/'   # darf nichts ausgeben
go vet ./... && go test -mod=vendor ./...
cd ..

# App und Server passen zusammen, Übersetzungen vollständig, Adressen da
for t in sprache_pruefen schnittstelle_pruefen adressen_pruefen \
         sprache_apple_pruefen sprache_android_pruefen; do
  python3 tools/$t.py
done

# Die Web-App ist reines JavaScript ohne Bauschritt — als Modul prüfen
for f in server/cmd/lumen/web/*.js; do node --input-type=module --check < "$f"; done
```

Die Android-Apps baut `gradle :app:assembleDebug` (braucht das
Android-SDK), die Apple-Apps `cd apple && xcodegen` (braucht einen Mac).
Ohne beides genügt ein Push in deinen Fork: `.github/workflows/apps.yml`
übersetzt dort alles.

## Regeln

### Sprache im Code

- **Kommentare und Bezeichner sind deutsch** — Funktionen, Variablen,
  Dateinamen, Testnamen (`TestSerieBleibtNachUmbenennung`,
  `fassungWaehlen`). Umlaute in Bezeichnern umschreiben (`ae`, `oe`,
  `ue`), in Kommentaren und Texten normal schreiben.
- Kommentare erklären das **Warum**, nicht das Was.
- Schweizer Rechtschreibung im Quelltext: `ss` statt `ß`.

### Übersetzungen: alle fünf Sprachen

Jeder sichtbare Text steht in allen fünf Sprachen: **Deutsch (Schweiz)** als
Quelle, **Deutsch (Deutschland)**, **Deutsch (Österreich)**, **Englisch
(USA)** und **Englisch (Vereinigtes Königreich)**. Nichts Deutsches fest
verdrahten.

| Teil | Wo | Regeln |
|---|---|---|
| Web-App | `tr('…')`, Kataloge in `server/cmd/lumen/web/sprachen/` | [BEGRIFFE.md](server/cmd/lumen/web/sprachen/BEGRIFFE.md) |
| Server-Meldungen | `tr(w, …)`, Katalog in `server/internal/sprache/` | |
| Apple | `apple/Sprache/*.xcstrings`, LumenKit eigener Katalog | [apple/BEGRIFFE.md](apple/BEGRIFFE.md) |
| Android | `values*/strings.xml`, Kern-Sätze in `lumen-core/…/Texte.kt` | [android/BEGRIFFE.md](android/BEGRIFFE.md) |

Die Prüfskripte oben finden fehlende Übersetzungen, fest verdrahtete
Texte und ß-Wörter ohne Eintrag für Deutschland und Österreich.

### Datenschutz: die Verwaltung sieht nie, wer was schaut

Das ist die wichtigste Regel in Lumen, und Tests sichern sie ab (siehe
„Die Datenschutz-Zusage im Code" im [README](README.md) und
[DATENSCHUTZ.md](DATENSCHUTZ.md)):

- Keine Abfrage über mehrere Profile hinweg auf Fortschritt, Bewertungen
  oder Merkliste; kein `count`/`sum`/`GROUP BY` auf diesen Tabellen.
- Kein Wiedergabe-Verlauf, keine Uhrzeiten, die mit Schauen zusammenhängen.
- Die Verwaltung bekommt bei laufenden Wiedergaben nur die Last — nie
  Titel, Person, Gerät, Ort oder Startzeit. Auch nicht im Protokoll, nicht
  in Dateinamen, nicht in Zwischendateien.
- Zugangsschlüssel nie in Adressen.
- Keine Telemetrie, keine Aufrufe zu Diensten, die die Verwaltung nicht
  ausdrücklich eingerichtet hat.

Ein Pull-Request, der einen dieser Tests lockert oder löscht, wird nicht
aufgenommen.

### Port 8097

Lumen hört auf **8097** (HTTPS: 8920) — überall: Server, Abbild, Apps,
Beispiele, Doku. Den Port von Emby und Jellyfin bitte nirgends hinschreiben,
auch nicht in Kommentaren; „Prüfen" schlägt sonst fehl.

### Lizenzkopf

Jede neue Quelldatei (`.go`, `.swift`, `.kt`, `.kts`, `.js`, `.css`,
`.html`, `.py`, `.sql`) beginnt mit dem AGPL-Kopf, etwa für Go:

```go
// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only
```

Copyright-Inhaber ist immer „Die Lumen-Autoren", nicht eine einzelne
Person. Der Ablauf „Prüfen" findet Dateien ohne Kopf.

### Nichts Persönliches

Keine echten Namen, E-Mail-Adressen, Heimnetz-Adressen oder Pfade aus
deinem Rechner in Code, Tests oder Beispielen. Testprofile heissen „Alex",
„Mara", „Jonas" …; Adressen sind Beispiele wie `192.168.1.20` oder
`meinserver.example`.

### Abhängigkeiten

Der Server kommt mit der Standardbibliothek und SQLite aus; Abhängigkeiten
liegen in `server/vendor/`. Eine neue Abhängigkeit (auch in den Apps oder
der Web-App) bitte vorher in einem Issue besprechen. GitHub-Actions werden
auf einen Commit festgenagelt (`uses: …@<sha> # vX.Y.Z`).

### Commits

Kleine, in sich geschlossene Commits mit einer Nachricht, die sagt, was
sich ändert und warum — gern auf Deutsch, Englisch geht auch.
