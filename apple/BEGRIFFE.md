# Begriffe der Apple-Apps

Dieselben Wörter wie im Browser — die Liste dort ist
`server/cmd/lumen/web/sprachen/BEGRIFFE.md`, die Texte stehen in
`sprachen/en-US.json` und `en-GB.json`. Wer hier etwas ändert, ändert es
dort mit — sonst heisst dieselbe Sache am iPhone anders als im Browser.

Die Texte stehen in zwei String-Katalogen:

- `apple/Sprache/Localizable.xcstrings` — iPhone, iPad, Mac und Apple TV
- `apple/LumenKit/Sources/LumenKit/Resources/Localizable.xcstrings` — was
  LumenKit selbst sagt (Fehler, „3 neue Folgen", „1 Std. 58 Min.")

Prüfen: `python3 tools/sprache_apple_pruefen.py`.

## Die fünf Sprachen

| Profil-Sprache | Ordner im Bündel | Schreibweise |
|---|---|---|
| Deutsch (Schweiz), `de-CH` | `de-CH` (Quellsprache) | „ss": heisst, Grösse, ausserhalb |
| Deutsch (Deutschland), `de-DE` | `de` | „ß": heißt, Größe, außerhalb |
| Deutsch (Österreich), `de-AT` | `de-AT` | „ß" wie Deutschland; „das E-Mail", „das SMS"; Monate mit „Jänner" (kommt aus der Locale) |
| English (United States), `en-US` | `en` | Movies, TV Shows, Favorites, color |
| English (United Kingdom), `en-GB` | `en-GB` | Films, TV Programmes, Favourites, colour |

**ß nie blind:** nur nach langem Vokal oder Doppellaut. „dass", „muss",
„lassen", „Schlüssel", „Passwort", „Adresse" bleiben mit ss.
Ersetzt werden in den Apps: aussen → außen, ausser → außer, ausserhalb →
außerhalb, gross → groß, Grösse → Größe, heisst → heißt, liess → ließ,
schliessen → schließen.

## Zahlen, Daten, Grössen

Nie fest „de_CH": immer `Locale.lumen` (die Sprache des Profils).

| | de-CH | de-DE / de-AT | en-US | en-GB |
|---|---|---|---|---|
| Grösse | 4.2 GB | 4,2 GB | 4.2 GB | 4.2 GB |
| Tag | 23.12.1968 | 23.12.1968 | 12/23/1968 | 23/12/1968 |
| Dauer | 1 Std. 58 Min. | 1 Std. 58 Min. | 1 hr 58 min | 1 hr 58 min |
| Folge | S1 F3 | S1 F3 | S1 E3 | S1 E3 |
| Altersfreigabe | ab 16 | ab 16 | 16+ | 16+ |
| Grenze im Konto | bis FSK 12 | bis FSK 12 | up to age 12 | up to age 12 |

## Wörter

| Deutsch (de-CH) | en-US | en-GB |
|---|---|---|
| Startseite | Home | Home |
| Filme / Film | Movies / Movie | Films / Film |
| Serien / Serie | TV Shows / Show | TV Programmes / Programme |
| Staffel / Folge | Season / Episode | Season / Episode |
| Specials (Staffel 0) | Specials | Specials |
| Merkliste | Watchlist | Watchlist |
| Merken | Add to Watchlist | Add to Watchlist |
| Gesehen | Watched | Watched |
| Als gesehen markieren | Mark as Watched | Mark as Watched |
| Weiterschauen | Continue Watching | Continue Watching |
| Weiter (Folge angefangen) | Resume | Resume |
| Weiter bei 12:34 | Resume at 12:34 | Resume at 12:34 |
| Als Nächstes | Up Next | Up Next |
| Ansehen | Watch | Watch |
| Von vorn | Start Over | Start Over |
| Vorspann überspringen | Skip Intro | Skip Intro |
| Favoriten / Favorit | Favorites / Favorite | Favourites / Favourite |
| Filmreihen / Reihe | Franchises / Franchise | Franchises / Franchise |
| Listen / Zur Liste | Lists / Add to List | Lists / Add to List |
| Im Haushalt zeigen | Share with Household | Share with Household |
| Wünsche / Wunsch | Requests / Request | Requests / Request |
| Neuigkeiten | What’s New | What’s New |
| Mediathek | Library | Library |
| Bibliothek | Library | Library |
| Ich | Me | Me |
| Einstellungen | Settings | Settings |
| Profil / Profilbild | Profile / Profile Picture | Profile / Profile Picture |
| Kinderprofil | Kids Profile | Kids Profile |
| Haushalt | Household | Household |
| Besitzer | Owner | Owner |
| Mitverwalter / Verwaltet mit | Co-admin | Co-admin |
| Vollzugriff | Full Access | Full Access |
| Verwaltung | Admin | Admin |
| Verwaltungssperre | Admin Lock | Admin Lock |
| Aktive Wiedergaben | Active Streams | Active Streams |
| Wiedergabe | Playback | Playback |
| Protokoll | Log | Log |
| Aufräumen | Cleanup | Cleanup |
| Sicherung | Backup | Backup |
| Umwandeln / wird umgewandelt | Convert / converting | Convert / converting |
| Direkt abspielbar | Plays directly | Plays directly |
| Fassung / Qualitäten (eines Films) | Version / Versions | Version / Versions |
| Anderen Titel zuweisen | Match a Different Title | Match a Different Title |
| nicht erkannt | not recognized | not recognised |
| Fernzugriff / Vermittler | Remote Access / Relay | Remote Access / Relay |
| Fingerabdruck | Fingerprint | Fingerprint |
| Fernseher anmelden | Sign In a TV | Sign In a TV |
| Mit Code anmelden | Sign In with Code | Sign In with Code |
| Abmelden / Überall abmelden | Sign Out / Sign Out Everywhere | Sign Out / Sign Out Everywhere |
| Verlauf löschen | Clear History | Clear History |
| Darstellung: Dunkel / Hell / Wie das Gerät | Appearance: Dark / Light / Match Device | gleich |
| Sprache: Wie der Server | Language: Same as Server | gleich |
| Ton / Tonspur / Untertitel | Audio / Audio Track / Subtitles | gleich |
| erzwungen / für Hörgeschädigte | forced / SDH | forced / SDH |
| Lizenz | License | Licence |
| Farbe | Color | Colour |

## Ton

Kurze Beschriftungen (Knöpfe, Titel, Menüs) auf Englisch in Title Case wie
bei Apple („Add to Watchlist", „Sign In a TV"); Sätze in normaler
Schreibweise. Du-Form auf Deutsch, „you" auf Englisch; keine Ausrufezeichen.
Anführungszeichen: „…" auf Deutsch, “…” auf Englisch.

Die Namen der Sprachen stehen in der Auswahl immer in der Sprache selbst
(„Deutsch (Österreich)", „English (United Kingdom)") — so findet man seine,
auch wenn man die eingestellte nicht lesen kann.
