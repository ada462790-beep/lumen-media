# Begriffe

Damit Lumen in jeder Sprache gleich spricht: ein Begriff, eine Übersetzung.
Gilt für die Web-App (`sprachen/*.json`) und die Meldungen des Servers
(`server/internal/sprache/kataloge/*.json`). Die Apps sollten dieselben
Wörter nehmen.

## Wie die Kataloge funktionieren

- Der Schlüssel ist der deutsche Text in Schweizer Schreibweise, so wie er
  im Code steht: `tr('Weiterschauen')`. Ohne Eintrag erscheint er so.
- `de-CH.json` enthält nur Mehrzahlformen (`{ "one": …, "other": … }`).
- `de-DE.json` enthält nur, was in Deutschland anders geschrieben wird – die
  Wörter mit ß. „dass", „muss", „Fluss", „Schloss", „Schlüssel" bleiben.
  Vorschlag erzeugen: `python3 tools/sprache_pruefen.py --eszett`, dann
  jedes Wort ansehen.
- `de-AT.json` erbt von de-DE. Österreichische Monatsnamen („Jänner")
  kommen aus `Intl`, nicht aus dem Katalog.
- `en-GB.json` erbt von en-US und enthält nur die britischen Abweichungen.
- Mehrdeutige deutsche Wörter tragen einen Zusammenhang vor `|`:
  `tr('Musik|{n} Titel')` → „{n} songs", `tr('{n} Titel')` → „{n} titles".
- Neue Sprache (z. B. fr-CH): Datei anlegen, in `sprache.js` (SPRACHEN,
  ERBT) und in `server/internal/sprache/sprache.go` (Sprachen) eintragen.

Prüfen: `python3 tools/sprache_pruefen.py` (läuft auch in der CI).

## Begriffsliste

| Deutsch (de-CH)       | English (US)            | English (UK)            |
|-----------------------|-------------------------|-------------------------|
| Startseite            | Home                    | Home                    |
| Filme / Film          | Movies / Movie          | Films / Film            |
| Serien / Serie        | TV Shows / Show         | TV Programmes / Programme |
| Staffel / Folge       | Season / Episode        | Season / Episode        |
| S1 F2                 | S1 E2                   | S1 E2                   |
| Merkliste             | Watchlist               | Watchlist               |
| Merken                | Add to Watchlist        | Add to Watchlist        |
| Gemerkt               | On Watchlist            | On Watchlist            |
| Gesehen               | Watched                 | Watched                 |
| Als gesehen markieren | Mark as Watched         | Mark as Watched         |
| Weiterschauen         | Continue Watching       | Continue Watching       |
| Weiter bei 12:34      | Resume at 12:34         | Resume at 12:34         |
| Abspielen / Ansehen   | Play / Watch            | Play / Watch            |
| Favoriten             | Favorites               | Favourites              |
| Filmreihe(n)          | Franchise(s)            | Franchise(s)            |
| Sammlung(en)          | Collection(s)           | Collection(s)           |
| Liste(n)              | List(s)                 | List(s)                 |
| Wiedergabeliste (Musik) | Playlist              | Playlist                |
| Titel (Musik)         | Song(s)                 | Song(s)                 |
| Interpret             | Artist                  | Artist                  |
| Wünsche / wünschen    | Requests / Request      | Requests / Request      |
| Neuigkeiten           | What’s New              | What’s New              |
| Ich                   | Me                      | Me                      |
| Verwaltung            | Admin                   | Admin                   |
| Verwaltungssperre     | Admin Lock              | Admin Lock              |
| Mitverwalter / Verwaltet mit | Co-admin         | Co-admin                |
| Vollzugriff           | Full Access             | Full Access             |
| Besitzer              | Owner                   | Owner                   |
| Haushalt              | Household               | Household               |
| Kinderprofil          | Kids Profile            | Kids Profile            |
| bis FSK 12            | up to age 12            | up to age 12            |
| Mediathek             | Library                 | Library                 |
| Bibliothek(en)        | Library / Libraries     | Library / Libraries     |
| Umwandeln             | Convert                 | Convert                 |
| Direkt abspielbar     | Plays directly          | Plays directly          |
| Vorspann              | Intro                   | Intro                   |
| Untertitel            | Subtitles               | Subtitles               |
| für Hörgeschädigte    | SDH                     | SDH                     |
| Sicherung             | Backup                  | Backup                  |
| Protokoll             | Log                     | Log                     |
| Aufräumen             | Cleanup                 | Cleanup                 |
| Fassung (von Lumen)   | Version                 | Version                 |
| Fassungen (eines Films) | Versions              | Versions                |
| Einladung             | Invitation              | Invitation              |
| von unterwegs         | remotely / away from home | remotely / away from home |
| Fernzugriff           | Remote Access           | Remote Access           |
| Vermittler            | relay                   | relay                   |
| Metadaten neu laden   | Refresh Metadata        | Refresh Metadata        |
| Anderen Titel zuweisen | Match a Different Title | Match a Different Title |
| Abmelden / Anmelden   | Sign Out / Sign In      | Sign Out / Sign In      |
| Abbrechen             | Cancel                  | Cancel                  |
| Löschen               | Delete                  | Delete                  |
| Speichern             | Save                    | Save                    |

Ton: kurz und freundlich wie bei Apple. Knöpfe und Überschriften in
Title Case („Mark as Watched"), Hinweise und Meldungen als Satz.
