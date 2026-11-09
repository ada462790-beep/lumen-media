# Sprachen der Android-Apps

Telefon, Tablet und Fernseher sprechen fünf Sprachen: Deutsch (Schweiz,
Deutschland, Österreich) und Englisch (USA, Vereinigtes Königreich). Welche
gilt, bestimmt das **Profil auf dem Server** (`GET /api/v1/me` → `sprache`),
vor der Anmeldung die Vorgabe des Servers (`GET /api/v1/hello` → `sprache`).
Gewählt wird unter Profil › Sprache (Telefon) bzw. Ich › Sprache (Fernseher);
gespeichert wird die Wahl als Pref `sprache` (leer = wie der Server).

## Wo die Texte stehen

| Teil         | Datei                                             | Inhalt |
|--------------|---------------------------------------------------|--------|
| lumen-core   | `lumen-core/…/Texte.kt` (`Tx`, `TxN`)             | Kennungen samt Schweizer Vorgabe für alles, was der Kern selbst formuliert (Abzeichen, Knöpfe, Prüfungen, Fehler) |
| lumen-texte  | `lumen-texte/src/main/res/values*/kern.xml`       | die Übersetzungen dieser Kennungen (`kern_…`) |
| lumen-texte  | `lumen-texte/src/main/res/values*/sprache.xml`    | die Einstellung „Sprache" |
| app, tv      | `*/src/main/res/values*/strings.xml`              | die Oberfläche |

lumen-core bleibt reines Kotlin ohne Android. Es liefert Kennungen; der
`KernUebersetzer` in lumen-texte macht daraus Text aus den Ressourcen.

## Rückfallkette

```
values/            Quelle: Deutsch (Schweiz), ss statt ß — auch für jede Sprache, die Lumen nicht kann
values-de-rCH/     Abschrift von values/ (erzeugt, nicht von Hand ändern)
values-de/         Deutsch (Deutschland), mit ß — auch für de-LU, de-BE, de-LI …
values-de-rAT/     nur Abweichungen von values-de
values-en/         English (United States) — auch für jedes andere Englisch
values-en-rGB/     nur Abweichungen von values-en
```

Warum die Abschrift: Ein Gerät auf de-CH sucht erst `values-de-rCH`, dann
`values-de`, erst danach `values`. Ohne Abschrift bekäme die Schweiz ß.
Warum `values-de` = Deutschland: so erben de-AT und alle übrigen deutschen
Regionen die Schreibung mit ß, und Österreich braucht nur, was dort anders
ist. Gleiches gilt für en-GB gegenüber en-US. Das deckt sich mit den
Katalogen des Servers (`server/internal/sprache/kataloge`: de-AT → de-DE →
de-CH, en-GB → en-US).

Datum, Monatsnamen und Sprachnamen kommen aus der Locale: in de-AT darum
„3. Jän.", in en-GB „29 Oct", in en-US „Oct 29". Die Muster stehen als
`kern_muster_tag_monat` und `kern_muster_datum` in kern.xml.

## Schreibregeln

**Deutsch (Schweiz)** — kein ß. Anführungszeichen „…" wie bisher im Code,
Auslassung mit Leerzeichen davor („Wird geladen …"), Tausender mit ’.

**Deutsch (Deutschland, Österreich)** — ß nur dort, wo es hingehört; nicht
blind ss → ß. Betroffen sind heute: gross → groß, Grösse → Größe, heisst →
heißt, ausserhalb → außerhalb, aussen → außen, liess(en) → ließ(en).
**dass, muss, Passwort, Schlüssel, lässt, passt, Adresse, Fassung bleiben.**
Tausender mit Punkt (Deutschland) bzw. geschütztem Leerzeichen (Österreich).
Österreich: „Jänner" usw. über die Locale; eigene Wörter nur, wenn sie in
der Oberfläche wirklich anders heissen.

**English (US)** — Title Case für Knöpfe, Reiter und Überschriften
(„Add to Watchlist", „Sign Out"), Satzschreibung für Sätze und Hinweise.
Typografische Apostrophe (don’t), Anführungszeichen “…”, Auslassung ohne
Leerzeichen („Loading…"), Tausender mit Komma.

**English (UK)** — wie US, aber britische Schreibung (favourite, colour,
recognise, licence), Anführungszeichen ‘…’, „Films" statt „Movies",
Datum dd/MM/yyyy.

## Begriffe

| de-CH (Quelle)          | de-DE / de-AT          | en-US                    | en-GB              |
|-------------------------|------------------------|--------------------------|--------------------|
| Startseite              | Startseite             | Home                     | Home               |
| Filme                   | Filme                  | Movies                   | Films              |
| Serien / Serie          | Serien / Serie         | Shows / Show             | Shows / Show       |
| Staffel / Folge         | Staffel / Folge        | Season / Episode         | Season / Episode   |
| Merkliste               | Merkliste              | Watchlist                | Watchlist          |
| Merken                  | Merken                 | Add to Watchlist         | Add to Watchlist   |
| Gesehen                 | Gesehen                | Watched                  | Watched            |
| Als gesehen markieren   | Als gesehen markieren  | Mark as Watched          | Mark as Watched    |
| Weiterschauen           | Weiterschauen          | Continue Watching        | Continue Watching  |
| Weiter bei 1:12:04      | Weiter bei 1:12:04     | Resume from 1:12:04      | Resume from 1:12:04 |
| Ansehen                 | Ansehen                | Play                     | Play               |
| Von vorn                | Von vorn               | Start Over               | Start Over         |
| Als Nächstes            | Als Nächstes           | Up Next                  | Up Next            |
| Nächste Folge           | Nächste Folge          | Next Episode             | Next Episode       |
| Vorspann überspringen   | Vorspann überspringen  | Skip Intro               | Skip Intro         |
| Favoriten               | Favoriten              | Favorites                | Favourites         |
| Filmreihen / Reihe      | Filmreihen / Reihe     | Collections / Collection | Collections        |
| Listen / Liste          | Listen / Liste         | Playlists / Playlist     | Playlists          |
| Wünsche / wünschen      | Wünsche / wünschen     | Requests / Request       | Requests / Request |
| Neuigkeiten             | Neuigkeiten            | What’s New               | What’s New         |
| Ich (TV) / Profil       | Ich / Profil           | Me / Profile             | Me / Profile       |
| Profil wechseln         | Profil wechseln        | Switch Profile           | Switch Profile     |
| Wer schaut?             | Wer schaut?            | Who’s Watching?          | Who’s Watching?    |
| Kinderprofil            | Kinderprofil           | Kids Profile             | Kids Profile       |
| Besitzer                | Besitzer               | owner                    | owner              |
| Vollzugriff             | Vollzugriff            | Full Access              | Full Access        |
| Verwaltung              | Verwaltung             | Admin                    | Admin              |
| Haushalt                | Haushalt               | household                | household          |
| Einladung / -scode      | Einladung / -scode     | invitation / invite code | invitation / invite code |
| von unterwegs           | von unterwegs          | remote(ly), away from home | remote(ly)       |
| Vermittler              | Vermittler             | relay                    | relay              |
| Fingerabdruck           | Fingerabdruck          | fingerprint              | fingerprint        |
| Anmelden / Abmelden     | Anmelden / Abmelden    | Sign In / Sign Out       | Sign In / Sign Out |
| Überall abmelden        | Überall abmelden       | Sign Out Everywhere      | Sign Out Everywhere |
| Fernseher anmelden      | Fernseher anmelden     | Sign In a TV             | Sign In a TV       |
| Umwandeln / wird umgewandelt | dito              | convert / converting     | convert / converting |
| Direkt abspielbar       | Direkt abspielbar      | Direct Play              | Direct Play        |
| Fassung (Datei / App)   | Fassung                | Version                  | Version            |
| Qualität unterwegs      | Qualität unterwegs     | Remote quality           | Remote quality     |
| Tonsprache / Tonspur    | Tonsprache / Tonspur   | Audio Language / Audio   | Audio Language / Audio |
| Untertitel              | Untertitel             | Subtitles                | Subtitles          |
| für Hörgeschädigte      | für Hörgeschädigte     | SDH                      | SDH                |
| erzwungen               | erzwungen              | forced                   | forced             |
| Verlauf                 | Verlauf                | History                  | History            |
| Haken (gesehen)         | Haken                  | watched marks            | watched marks      |
| Besetzung               | Besetzung              | Cast                     | Cast               |
| Ähnliche Titel          | Ähnliche Titel         | More Like This           | More Like This     |
| Mediathek               | Mediathek              | library                  | library            |
| Herunterladen           | Herunterladen          | Download                 | Download           |
| Lizenz / Quelltext      | Lizenz / Quelltext     | License / Source Code    | Licence / Source Code |
| Grösse (Untertitel): Klein · Normal · Gross | Klein · Normal · Groß | Small · Medium · Large | Small · Medium · Large |
| ab 16                   | ab 16                  | 16+                      | 16+                |
| 1 Std. 58 Min.          | 1 Std. 58 Min.         | 1 hr 58 min              | 1 hr 58 min        |

Die englischen Begriffe folgen dem Katalog des Servers
(`server/internal/sprache/kataloge/en-US.json`: shows, collection,
playlist, request, Admin, Me, kids profile, convert). Legt die
Web-Oberfläche eine eigene BEGRIFFE.md an (`server/cmd/lumen/web/sprachen/`),
gilt bei Abweichungen deren Wahl, und diese Tabelle wird nachgezogen.

## Einen Text hinzufügen

1. In Kotlin `tx(R.string.name)` bzw. `txn(R.plurals.name, anzahl)`
   schreiben, nie festen Text. In lumen-core eine Kennung in `Tx`/`TxN`
   anlegen und `Texte.t(Tx.…)` benutzen; dann `kern_…` in
   `lumen-texte/…/values/kern.xml` und `KernUebersetzer.kt` ergänzen.
2. Den Text in `values/` (de-CH) eintragen, dann in `values-de/` (de-DE)
   und `values-en/` (en-US); in `values-de-rAT/` bzw. `values-en-rGB/` nur,
   wenn er dort anders lautet.
3. `python3 tools/sprache_android_pruefen.py --schreiben` schreibt die
   Schweizer Abschrift neu und prüft alles; ohne `--schreiben` prüft es nur.
   Lint behandelt fehlende Übersetzungen als Fehler (MissingTranslation).
