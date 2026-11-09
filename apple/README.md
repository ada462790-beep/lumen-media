# Lumen für iPhone, iPad, Mac und Apple TV

Ein Projekt, vier Geräte. Die Logik steckt in `LumenKit` und ist auf allen
gleich; nur die Oberfläche unterscheidet sich — und das zu Recht: auf dem
Fernseher gibt es keinen Zeiger, sondern einen Fokus, der springt, und man
sitzt drei Meter entfernt.

```
LumenKit/     Verbindung zum Server, Modelle, Anmeldung, Wiedergabe
Lumen/        Oberfläche für iPhone, iPad und Mac
LumenTV/      Oberfläche für Apple TV
project.yml   erzeugt daraus das Xcode-Projekt
```

## Öffnen

```bash
brew install xcodegen
cd apple && xcodegen && open Lumen.xcodeproj
```

Dann in Xcode unter *Signing & Capabilities* die eigene Team-Kennung
eintragen und auf ein Gerät schieben.

Ohne XcodeGen geht es auch von Hand: in Xcode ein neues Multiplatform-App-
Projekt anlegen, `LumenKit` über *File → Add Package Dependencies → Add
Local* hinzufügen und die Ordner `Lumen` beziehungsweise `LumenTV` in die
jeweiligen Ziele ziehen.

## Prüfen, ohne zu bauen

`LumenKit` lässt sich ohne Xcode testen:

```bash
cd apple/LumenKit && swift test
```

Und vom Projektstamm aus hält ein Skript App und Server gegeneinander —
es findet falsche Pfade und falsche Feldnamen, bevor sie als leerer
Bildschirm auffallen:

```bash
python3 tools/schnittstelle_pruefen.py
```

Ein zweites prüft die Texte: jeder Text aus dem Code steht im Katalog und
ist in allen fünf Sprachen übersetzt, und kein deutscher Satz ist fest
verdrahtet:

```bash
python3 tools/sprache_apple_pruefen.py
```

## Sprachen

Deutsch (Schweiz, Deutschland, Österreich) und Englisch (USA,
Grossbritannien). Die Apps sprechen die Sprache des **Profils** auf dem
Server — wer am Fernseher der Familie sein Profil wählt, liest seine.
Vor der Anmeldung gilt die Vorgabe des Servers, bei einem älteren Server
die des Geräts. Gewählt wird unter Ich › Einstellungen › Sprache; die
Vorgabe des Servers stellt der Besitzer am Mac unter Verwaltung › „Name, Sprache,
Metadaten, Fernzugriff“ ein.

- Der Code ist Schweizer Hochdeutsch; die Kataloge `Sprache/Localizable.xcstrings`
  (Apps) und `LumenKit/Sources/LumenKit/Resources/Localizable.xcstrings`
  enthalten die übrigen vier Sprachen. Neue Texte legt Xcode beim Bauen
  selbst an; übersetzen im Katalog-Editor, Begriffe nach `BEGRIFFE.md`.
- `Text("…")` folgt `.environment(\.locale, …)`. Was ausserhalb einer
  Ansicht zu Text wird, schreibt man
  `String(localized: "…", bundle: .lumen, locale: .lumen)` (in LumenKit
  `bundle: .lumenKit`) — `Bundle.main` allein spräche die Sprache des Geräts.
- Zahlen und Daten mit `.locale(.lumen)`, nie fest „de_CH".
- Nach einem Wechsel baut sich die App neu auf; beim nächsten Start
  beschriftet auch das System (Menüleiste am Mac, Dialoge) in der Sprache
  des Profils.

## Was auf Apple-Geräten einfacher ist

Abgespielt wird über AVPlayer. Der versteht HLS von Haus aus, und weil der
Server die vollständige Abspielliste vorab ausliefert, funktioniert
Springen sofort — auch mitten in einen Film, der gerade erst umgewandelt
wird. Dazu kommen AirPlay, Bild-in-Bild und die Steuerung auf dem
Sperrbildschirm, ohne dass eine Zeile dafür nötig wäre.

Die Web-Fassung muss sich dafür mehr Mühe geben; sie hat keinen AVPlayer.

## Was überall gleich ist

Dieselben Funktionen wie im Browser, auf jedem Gerät so, wie es dort
üblich ist:

- **Navigation:** auf dem iPhone unten fünf Reiter — Startseite, Filme,
  Serien, Suche, Ich; unter „Ich" Merkliste und Gesehen, Favoriten,
  Listen, Filmreihen, Neuigkeiten (der Zähler hängt am Reiter), Wünsche,
  Downloads, Profil schützen und die Einstellungen. Auf iPad und Mac alles
  in der Seitenleiste links (am Mac dazu die Verwaltung). Auf dem Apple TV
  oben sechs Punkte — Startseite, Filme, Serien, Merkliste, Suche und der
  eigene Name, darunter der Rest.
- **Mac:** der Player läuft in einem eigenen Fenster (Vollbild mit ⌃⌘F);
  ⌘F springt zur Suche, die Leertaste spielt und hält an, ⌘, öffnet die
  Einstellungen.
- **Downloads** (iPhone, iPad, Mac): auf der Titelseite, wenn der Server
  es für das Profil erlaubt; geladen wird im Hintergrund. Offline gespielt
  merkt sich die App die Stelle und reicht sie nach, sobald der Server
  wieder erreichbar ist.
- **Filme und Serien:** die Genres des Servers als Filter, das Raster lädt
  beim Scrollen seitenweise nach (`Raster` in LumenKit).
- **Suche:** Personen, Genres, die Deutung der Eingabe, Treffer — und wenn
  nichts passt, gleich wünschen.
- **Titelseite:** Bild, Technik-Zeile, Abspielen/Weiter bei, Von vorn,
  Merken, Zur Liste, gesehen, Bewertung, Im Haushalt, Staffeln und Folgen,
  Besetzung (führt zu „alles mit …"), Filmreihe, Ähnliche Titel.
- **Player:** Ton und Untertitel, Untertitel-Darstellung, Vorspann
  überspringen, Als Nächstes mit Countdown, Nächste Folge, Folgen-Feld.
- **Profil:** Profil wechseln, Ton- und Untertitelsprache, Untertitel-
  Darstellung, Folgen automatisch weiter, Vorspann überspringen,
  Profilbild, Privatsphäre, Verlauf; auf iPhone, iPad und Mac auch hell
  oder dunkel.
- **Mehrere Server:** jede App merkt sich eine Liste (Name, Adresse,
  Fingerabdruck; das Token je Server getrennt im Schlüsselbund). Beim Start
  geht es zum zuletzt benutzten; „Server wechseln" steht im Profil und in
  der Profilauswahl. Wer von einer älteren Fassung kommt, bleibt angemeldet.
- **Anmelden mit Code:** auf dem Apple TV der Hauptweg — sechs Ziffern, die
  man am Handy oder im Browser unter Profil → Fernseher anmelden eingibt;
  auf iPhone, iPad und Mac als „Mit Code von einem anderen Gerät".
- **Bewegte Vorschau:** auf dem Mac beim Drüberfahren, auf dem Apple TV
  beim Fokus — auf iPhone und iPad nicht.

## Verwaltung

Die Verwaltung gehört an den Schreibtisch. Auf dem Mac: Bibliotheken
prüfen, Wünsche bearbeiten, Aufräumen (samt aufgelöster Filmreihen),
Konten ansehen und einladen, aktive Wiedergaben, Protokoll,
Verwaltungssperre. Auf dem Apple TV gibt es keine Verwaltung; iPhone und
iPad behalten nur, was schon da war.

## Was noch fehlt

- Downloads für unterwegs
- In der Verwaltung des Macs: Bibliotheken anlegen samt Ordnerwähler,
  Metadaten-Schlüssel, Gruppen und Updates (nur Besitzer) — dafür der Browser
- Eine Apple-Watch-Fernbedienung wäre nett, ist aber kein Muss

---

Lizenz: AGPL-3.0-only — siehe [LICENSE](../LICENSE) im Wurzelverzeichnis.
