# Lumen

*[English summary](README.en.md)*

Dein eigener Streaming-Dienst für zu Hause. Filme, Serien, Musik und Fotos
von der eigenen Platte, auf jedem Gerät im Haus — ruhig, aufgeräumt, mit
einer Oberfläche, die aussieht, als hätte Apple sie gebaut. Lumen ist
freie Software (AGPL-3.0), läuft als ein einziger Docker-Container auf dem
NAS und braucht weder Konto noch Abo noch Cloud.

- **Ein Server, alle Geräte:** Browser, iPhone, iPad, Mac, Apple TV,
  Android, Android TV und Fire TV.
- **Erkennt deine Sammlung:** Filme, Serien, Filmreihen, mehrere Fassungen
  eines Films, Filme in Teilen, NFO-Dateien und lokale Poster.
- **Spielt alles ab:** direkt, wenn das Gerät die Datei kann, sonst
  umgewandelt — auf Wunsch mit Hardware-Beschleunigung. Untertitel,
  Tonspuren, Vorspann überspringen, Weiterschauen, Downloads.
- **Für jede Person:** eigene Profile, Merkliste, Bewertungen, geteilte
  Listen, Kinderprofile mit Altersgrenze.
- **Von unterwegs ohne Portfreigabe:** über einen kleinen Vermittler, der
  selbst nicht hineinsehen kann.
- **Umzug aus Emby oder Jellyfin:** Gesehen-Haken, Fortschritte und
  Favoriten kommen mit.

**Das Versprechen: Die Verwaltung sieht nie, wer was schaut.**
Du betreibst den Server, du bestimmst, wer dazugehört und was er sehen darf.
Aber wann jemand etwas anschaut, wie lange und was — das bleibt bei ihm.
Lumen speichert keinen Verlauf, die Verwaltung zeigt es nicht, und es gibt
keine Abfrage dafür. Tests sorgen dafür, dass das so bleibt.

Ehrlich dazu: Wer als Besitzer vollen Zugriff auf das NAS hat (root), kann
laufende Programme, gerade geöffnete Dateien und die Datenbank selbst
einsehen. Ganz verhindern lässt sich das auf fremder Hardware nicht. Lumen
hält die Spuren so klein wie möglich — was genau gespeichert wird und was
nicht, steht in [DATENSCHUTZ.md](DATENSCHUTZ.md).

---

## Loslegen

Lumen läuft am einfachsten als Docker-Container. Das fertige Abbild gibt es
für **amd64** (Intel/AMD) und **arm64** (64-Bit-ARM) — siehe
[Welches NAS passt](#welches-nas-passt).

Das Abbild ist öffentlich: **`ghcr.io/ada462790-beep/lumen-media`**. Keine
Anmeldung bei ghcr.io, kein Token — die mitgelieferte
[`docker-compose.yml`](docker-compose.yml) genügt. Wer Lumen schon mit dem
alten Abbild betreibt: siehe [Umzug vom alten Abbild](#umzug-vom-alten-abbild).

### Synology Schritt für Schritt (Container Manager, ohne SSH)

Gilt für DSM 7.2 mit dem Paket **Container Manager**. Kein Terminal, kein
git — alles in der Weboberfläche des NAS.

**1. Ordner anlegen.** In der **File Station** im freigegebenen Ordner
`docker` (gibt es ihn nicht, unter *Systemsteuerung › Freigegebener Ordner*
anlegen) den Ordner `lumen` und darin `daten` anlegen:

```
/volume1/docker/lumen          hier liegt die docker-compose.yml
/volume1/docker/lumen/daten    Datenbank, Poster, tägliche Sicherung
```

**2. Projekt erstellen.** *Container Manager › Projekt › Erstellen*:
Projektname `lumen`, Pfad `/docker/lumen`, Quelle *docker-compose.yml
erstellen*, und den Inhalt der [`docker-compose.yml`](docker-compose.yml)
aus diesem Repository hineinkopieren. Darin prüfen:

- **Medien:** die Zeile `/volume1/video:/volume1/video:rw` — links und
  rechts der Pfad zu deinen Filmen, so wie die File Station ihn zeigt
  (Freigabe `video` = `/volume1/video`). Weitere Ordner als weitere Zeilen.
- **Benutzer:** `PUID` und `PGID` (siehe [unten](#benutzer-puid-und-pgid)).
- **Zeitzone:** `TZ` — Vorgabe ist `Europe/Zurich`.

Ein Webportal über Web Station braucht es nicht. *Fertig* — der Container
Manager lädt das Abbild und startet Lumen.

**3. Firewall.** Ist die Firewall des NAS an (*Systemsteuerung › Sicherheit
› Firewall*), unter *Regeln bearbeiten* eine Regel anlegen: Port
**8097/TCP**, Quelle **nur dein Heimnetz** (etwa `192.168.1.0/24`),
*Zulassen*. Wer HTTPS auf 8920 nutzt, gibt diesen Port genauso frei — auch
nur fürs Heimnetz.

**4. Öffnen.** `http://<adresse-des-nas>:8097` im Browser. Beim ersten Start
legst du dich als Besitzer an und sagst Lumen, wo die Filme liegen: unter
„Durchsuchen" stehen die Ordner, die in der `docker-compose.yml` bei
`volumes` freigegeben sind, ganz oben.

**Aktualisieren:** *Projekt › lumen › Aktion › Erstellen*. Weil in der
Datei `pull_policy: always` steht, holt der Container Manager dabei das
neue Abbild; die Daten im Ordner `daten` bleiben. Alte, nicht mehr
benutzte Abbilder kann man unter *Image* löschen. Von selbst, jede Nacht:
siehe [Watchtower](#updates-von-selbst-watchtower).

#### Wenn das Abbild nicht lädt

- **Name prüfen:** Es heisst `ghcr.io/ada462790-beep/lumen-media:latest` —
  mit `-media`. Das alte Abbild ohne `-media` gibt es nicht öffentlich
  (siehe [Umzug](#umzug-vom-alten-abbild)).
- **„denied" oder „unauthorized"** heisst bei einem öffentlichen Abbild
  meist: Der Name ist falsch geschrieben, oder im Container Manager ist
  unter *Registry* noch eine alte Anmeldung mit abgelaufenem Token für
  `ghcr.io` eingetragen. Die Anmeldung entfernen, dann neu *Erstellen*.
- **Eigenes, privates Abbild** (etwa aus einem privaten Fork): Dann braucht
  Docker eine Anmeldung bei ghcr.io mit einem Token (classic) mit nur dem
  Bereich `read:packages` — im Container Manager unter *Registry* oder per
  `sudo docker login ghcr.io -u DEIN_GITHUB_NAME`.

### Kurzweg per SSH (Synology, Linux, jedes NAS)

```bash
sudo mkdir -p /volume1/docker/lumen/daten && cd /volume1/docker/lumen
# docker-compose.yml aus diesem Repository hierher kopieren, Pfade anpassen
sudo docker compose up -d
```

Aktualisieren: `sudo docker compose pull && sudo docker compose up -d`.

### Andere Plattformen

- **Linux mit Docker:** wie der Kurzweg; Pfade nach Geschmack, etwa
  `/srv/lumen/daten:/data` und `/srv/filme:/srv/filme:rw`. `PUID`/`PGID`
  ist meist `1000`/`1000` (`id` zeigt sie).
- **Unraid:** über das Plugin *Docker Compose Manager* mit derselben Datei,
  oder im Terminal wie beim Kurzweg. Üblich sind `PUID=99`, `PGID=100`,
  Daten unter `/mnt/user/appdata/lumen:/data`, Medien etwa
  `/mnt/user/Filme:/mnt/user/Filme:rw`.
- **Ohne Docker:** siehe [unten](#ohne-docker) — eine Datei, auch für
  32-Bit-ARM.

### Umzug vom alten Abbild

Lumen lag früher unter `ghcr.io/ada462790-beep/lumen` und war privat. Neue
Fassungen erscheinen nur noch unter dem öffentlichen
`ghcr.io/ada462790-beep/lumen-media`. Für eine bestehende Installation ist
das eine einzige Zeile — **die Daten bleiben erhalten**, denn sie liegen im
Datenordner (oder im Volume), nicht im Abbild.

1. In der `docker-compose.yml` die Zeile

   ```yaml
   image: ghcr.io/ada462790-beep/lumen:latest
   ```

   ersetzen durch

   ```yaml
   image: ghcr.io/ada462790-beep/lumen-media:latest
   ```

   Sonst nichts ändern — vor allem nicht die Zeilen bei `volumes`. Auf
   einem Synology: *Container Manager › Projekt › lumen › Stoppen*, dann
   *YAML-Konfiguration* bearbeiten (oder die Datei in der File Station
   unter `/docker/lumen` öffnen).
2. Neu starten: im Container Manager *Projekt › lumen › Aktion ›
   Erstellen*; per SSH `sudo docker compose pull && sudo docker compose up -d`.
3. Aufräumen, wenn alles läuft: Die Anmeldung bei ghcr.io (Container
   Manager › *Registry*, oder `sudo docker logout ghcr.io`), die
   Zugangsdaten bei Watchtower (`REPO_USER`/`REPO_PASS` oder die
   eingehängte `config.json`) und `LUMEN_GITHUB_TOKEN` braucht es nicht
   mehr. Das alte Abbild kann unter *Image* weg.

**Vermittler:** auf dem Mietserver in der `docker-compose.relay.yml`
genauso `ghcr.io/ada462790-beep/lumen-relay` durch
`ghcr.io/ada462790-beep/lumen-media-relay` ersetzen und neu starten. Das
Zertifikat und damit der Fingerabdruck bleiben.

**Ohne Docker:** einmal das Paket von der
[Veröffentlichungsseite](https://github.com/ada462790-beep/lumen-media/releases)
holen und wie gewohnt einsetzen. Ab dann sieht die Verwaltung dort nach
neuen Fassungen — ohne Token.

### Welches NAS passt

| Bauart | Abbild | Beispiele |
|---|---|---|
| Intel/AMD (x86-64) | ✓ `amd64` | Synology „+“-Modelle wie DS220+, DS224+, DS423+, DS723+, DS920+, DS923+, DS1522+; die meisten Eigenbau-NAS, Unraid, TrueNAS |
| 64-Bit-ARM | ✓ `arm64` | Synology mit Realtek RTD1296/RTD1619B (etwa DS218, DS220j, DS223, DS423) — sofern Synology dort den Container Manager anbietet; Raspberry Pi 4/5 mit 64-Bit-System |
| 32-Bit-ARM (armv7) | ✗ | ältere Einsteigermodelle. Kein Docker-Abbild; das Paket `lumen-linux-arm` ohne Docker geht |

Welcher Prozessor drinsteckt, steht unter *Systemsteuerung › Info-Center*.
Für das Umwandeln zählt die Rechenleistung: Ein ARM-Modell spielt direkt
ab, was das Gerät kann, kommt beim Umwandeln aber schnell an seine Grenze
(siehe `LUMEN_MAX_UMWANDLUNGEN` unten).

### Die Ordner: Daten und Medien

- **Daten (`/data`).** Datenbank, Poster, Zwischenspeicher und die
  tägliche Sicherung. In der mitgelieferten Datei ist das der Ordner
  `/volume1/docker/lumen/daten` — ein gewöhnlicher Ordner, den File
  Station und Hyper Backup sehen. Lumen macht ihn beim Start selbst für
  den Benutzer aus `PUID`/`PGID` beschreibbar.
- **Medien.** Ein Container sieht vom NAS nur, was bei `volumes` steht —
  am besten links und rechts derselbe Pfad, etwa
  `/volume1/video:/volume1/video:rw`. Das **`:rw`** (lesen und schreiben)
  ist gewollt: Liegt ein Film in Teilen vor (`Film a.avi`, `Film b.avi`,
  `CD1`, `CD2`), fügt Lumen sie auf Wunsch in der Verwaltung zu einer
  Datei zusammen, ohne neu zu rechnen. Die Teile wandern dabei in einen
  Unterordner `.lumen-teile`; gelöscht wird nichts. Sonst schreibt Lumen
  nichts in die Medienordner.

**Schon eine Installation mit `lumen-daten:/data`?** Ältere Fassungen der
`docker-compose.yml` legten die Daten in ein benanntes Volume. Das läuft
weiter, auch mit allen Updates — **behalte dann einfach deine bisherige
Datei**. Ersetzt du sie durch die neue, sieht Lumen einen leeren
Datenordner und beginnt von vorn (die alten Daten sind dann nicht weg,
nur nicht eingehängt). Umziehen geht so: siehe
[Sichern und Wiederherstellen](#vom-volume-in-einen-ordner-umziehen).

### Benutzer: PUID und PGID

Lumen läuft im Container nie als root, sondern als der Benutzer, den
`PUID` (Benutzernummer) und `PGID` (Gruppennummer) nennen. Dieser Benutzer
muss die Medienordner lesen dürfen — und, fürs Zusammenführen von Teilen,
auch schreiben.

**Auf einem Synology** hat der erste selbst angelegte Benutzer meist
`1026`, der nächste `1027` usw.; die Gruppe `users` ist `100`. Darum steht
in der mitgelieferten Datei `PUID=1026` und `PGID=100`.

- **Rechte prüfen (ohne SSH):** *Systemsteuerung › Freigegebener Ordner ›
  video › Bearbeiten › Berechtigungen*. Der Benutzer, dessen Nummer du
  einträgst, braucht dort *Lesen/Schreiben*.
- **Nummer herausfinden (ohne SSH):** *Systemsteuerung › Aufgabenplaner ›
  Erstellen › Geplante Aufgabe › Benutzerdefiniertes Skript*, Benutzer
  root, Skript `id DEIN_NAS_BENUTZER > /volume1/docker/lumen/id.txt`.
  Einmal ausführen, `id.txt` in der File Station öffnen
  (`uid=1026(…) gid=100(users)`), dann Aufgabe und Datei löschen. Mit SSH:
  `id DEIN_NAS_BENUTZER`.

**Was passiert, wenn es nicht stimmt:** Der Datenordner ist kein Problem —
Lumen passt ihn beim Start an. Die Medien aber nicht: Darf der Benutzer
einen Ordner nicht lesen, sagt Lumen das beim „Durchsuchen", und eine
Bibliothek dort bleibt leer. Fehlt nur das Schreibrecht, läuft alles
ausser dem Zusammenführen von Teilen („Lumen darf in diesen Ordner nicht
schreiben"). Nummern ändern und das Projekt neu *Erstellen* genügt.

### Updates von selbst (Watchtower)

Im Container ersetzt sich Lumen nicht selbst; die Verwaltung zeigt nur an,
dass es eine neue Fassung gibt. Wer nachts automatisch aktualisieren will,
nimmt den auskommentierten Dienst `watchtower` aus der
`docker-compose.yml` (und bei `lumen` die zwei `labels`-Zeilen). Läuft
gerade eine Wiedergabe, bricht sie ab — darum nachts um 4.

Weil das Abbild öffentlich ist, braucht Watchtower keine Zugangsdaten.
Nur wer ein eigenes, privates Abbild nutzt, trägt bei `watchtower`
`REPO_USER` (GitHub-Name) und `REPO_PASS` (Token mit `read:packages`) ein —
sonst scheitert das Update dort jede Nacht still.

### Ohne Docker

Lade das Paket für deine Plattform herunter, pack es aus und starte:

```bash
tar xzf lumen-linux-amd64.tar.gz
cd lumen-linux-amd64 && ./lumen
```

Das ist alles. Eine Datei, keine Installation, keine Laufzeitumgebung. Nur
**FFmpeg** muss auf dem Rechner sein — ohne läuft Lumen auch, kann dann aber
nur Dateien ausliefern, die das Gerät ohnehin abspielen kann. Pakete gibt
es für Linux (amd64, arm64, arm), macOS (Apple Silicon, Intel) und Windows.

Jede Einstellung geht als Schalter oder als Umgebungsvariable — siehe
[Einstellungen](#einstellungen-umgebungsvariablen).

**Aktuell bleiben:** In der Verwaltung unter „Updates" sieht Lumen täglich
nach, ob es eine neue Fassung gibt, und setzt sie auf Knopfdruck oder von
selbst in einem Zeitfenster (etwa 03:00–05:00) ein — aber nie, während
jemand schaut. Eingesetzt wird nur, was mit dem Schlüssel der
Veröffentlichung unterschrieben ist: `pruefsummen.txt` samt der Fassung,
für die sie gilt. Die alte Datei bleibt als `lumen.alt` liegen. Ein
GitHub-Token braucht es dafür nicht — das Repository ist öffentlich.
*Optional*, nur falls die Updates aus einem privaten Repository kommen
sollen: ein Token, das die Veröffentlichungen lesen darf (klassisch mit
Bereich `repo`, oder fein-granular mit Lesezugriff auf *Contents*), in der
Verwaltung eintragen oder `LUMEN_GITHUB_TOKEN` setzen. Im Container geht
das Einsetzen nicht: dort heisst Aktualisieren, das Abbild neu zu holen
(siehe oben).

Im Paket liegt neben `lumen` noch `lumen-relay` — der gehört nicht hierhin,
sondern auf eine Maschine mit öffentlicher Adresse. Siehe „Von unterwegs
schauen" weiter unten.

### Selbst bauen

```bash
cd server && make        # baut ./lumen für diesen Rechner
make test                # alle Tests
make release             # für alle Plattformen (braucht Zig, s. Makefile)
```

---

## Funktionen

<!--
  FUNKTIONEN — hier ergänzen.
  Jede Funktion ein Absatz, der mit **Name.** beginnt. Nur beschreiben,
  was im Stand von main wirklich drin ist; Geplantes gehört unter
  „Noch offen" ganz unten.
-->

**Mediathek.** Liest deine Ordner ein und versteht, wie Dateien in echten
Sammlungen heissen: `Die.Bucht.2019.German.1080p.BluRay.x264-GRUPPE.mkv` wird
zum Film „Die Bucht" von 2019. Staffelordner, Doppelfolgen, Mehrteiler,
Director's Cut, 4K und HDR erkennt es mit. Was es nicht sicher zuordnen kann,
meldet es als „nicht erkannt", statt still den falschen Film einzutragen.

**Fassungen und Qualität.** Liegt derselbe Film mehrfach vor — etwa als
MKV in 1080p und als alte AVI —, ist das ein Titel, nicht zwei. „Ansehen"
fragt dann, in welcher Qualität (Auflösung, Format, Grösse); wer verwaltet,
legt fest, welche Fassung für alle zuerst kommt.

**Filme in Teilen.** `Film a.avi`, `Film b.avi`, `Film c.avi` oder `CD1`,
`CD2` erkennt Lumen als einen Film. In der Verwaltung lassen sich die
Teile zu einer Datei zusammenführen — ohne neu zu rechnen, die Teile
wandern in den Unterordner `.lumen-teile`. Dafür sind die Medienordner
mit `:rw` eingehängt.

**Lokale Metadaten.** NFO-Dateien (Kodi, Emby, Jellyfin, tinyMediaManager)
und Bilder neben den Dateien (`poster.jpg`, `fanart.jpg`, Staffelposter)
übernimmt Lumen mit; was du dort gepflegt hast, geht beim Umzug nicht
verloren. Die Verwaltung kann jeden Titel auch von Hand bearbeiten —
was du änderst, bleibt gesperrt und wird nicht mehr überschrieben.

**Beschreibungen, Poster und Noten** holt es von TMDb; die IMDb-Note und
den Rotten-Tomatoes-Wert über OMDb. Das richtest du einmal ein, dann gilt
es für alle — niemand im Haushalt braucht ein eigenes Konto. Die
Altersfreigabe richtet sich nach der Region, die du einstellst. Auf jeder
Titelseite führen Links zu IMDb, TheMovieDb, TheTVDB und Trakt.

**Abspielen.** Kann das Gerät die Datei direkt, bekommt es sie direkt: keine
Last, volle Bildqualität. Sonst wandelt Lumen um — und zwar so, dass Springen
funktioniert: die Abspielliste steht vollständig da, bevor eine Sekunde
gerechnet ist, und FFmpeg setzt genau dort an, wo du hinspringst.

**Ton und Untertitel** wählt man im Player: jede Tonspur der Datei,
Untertitel eingebettet oder als `.srt` daneben (`Film.de.srt`,
`Film.en.forced.srt`). Die Sprache merkt sich Lumen pro Konto. Den
**Vorspann** überspringt ein Knopf — aus den Kapiteln der Datei, oder weil
jemand ihn einmal für die ganze Serie markiert hat —, und im Abspann steht
schon da, was als Nächstes kommt.

**Suchen** geht nach Titel, Schauspieler, Genre und Jahr, auch zusammen:
„Filme mit Tom Hanks aus den 90ern", „Komödie 2019".

Im Browser geht es ohne Fremdbibliothek: Lumen fragt, was der Browser kann,
und schickt genau das. Auch einem Browser ohne H.264 — manche Linux-Ausgaben
lassen die lizenzpflichtigen Decoder weg — liefert es ein Bild, nämlich VP9.

**Für jede Person.** Eigene Merkliste, eigene Bewertungen, eigener Stand im
Film. Merkliste, **Gesehen** und eigene Listen liegen unter einem Reiter
(Merkliste | Gesehen | Listen); „Gesehen" zeigt, was du fertig geschaut
hast — nur dir. Einladen geht mit einem Link, der sieben Tage gilt und nur
einmal. Kinderprofile sehen nur, was altersfrei ist und ausdrücklich
geprüft wurde.

**Verwaltung.** Bibliotheken, laufende Wiedergaben (nur die Last, nicht
wer was schaut), Protokoll, Aufräumen (doppelte Titel, fehlende Dateien),
Rechte pro Person oder Gruppe, Updates, tägliche Sicherung. Rechte in der
Verwaltung gibt es in drei Stufen: *keine* (nur schauen), *verwaltet mit*
(Bibliotheken, Konten ohne Verwaltung, Wünsche, Protokoll) und
*Vollzugriff* (alles wie der Besitzer, auch Gruppen, Rechte, Updates und
Sicherungen). Wer was schaut, sieht auch mit Vollzugriff niemand.

**Apps** für iPhone, iPad, Mac, Apple TV, Android, Android TV und Fire TV
— siehe [unten](#die-apps-bauen-ohne-mac-und-ohne-android-studio).

---

**Schauen.** Weiterschauen wie bei Netflix, auch mit der nächsten Folge
nach einer fertigen; Gesehen für Folge, Staffel oder ganze Serie;
Vorschaubilder beim Spulen; Untertitel suchen und herunterladen
(OpenSubtitles, Schlüssel in der Verwaltung); Downloads für unterwegs, wenn
die Verwaltung sie erlaubt.

**Musik und Fotos.** Eigene Bibliotheksarten mit Alben, Interpreten,
Wiedergabelisten und einem Player, der beim Blättern weiterläuft; Fotos
nach Jahren, Monaten und Alben mit Diashow. Der Aufnahmeort von Fotos ist
standardmässig ausgeblendet.

**Eigene Sammlungen** („Weihnachtsfilme“, „Marvel in Reihenfolge“) neben
den automatisch erkannten Filmreihen.

**Umzug aus Emby oder Jellyfin.** Jedes Profil holt unter Ich › Umzug seine
eigenen Gesehen-Haken, Fortschritte und Favoriten herüber. Das Passwort
wird nicht gespeichert, und die Verwaltung sieht davon nichts.

**Kinderprofile.** Beim Einladen wählbar, mit Altersgrenze; Gruppen und
Rechte können Grenzen nie lockerer machen. Wer verwaltet, braucht eine PIN
und hat selbst keine Grenzen.

### Hardware-Beschleunigung

Mit Intel- oder AMD-Grafik (die meisten Synology-Modelle mit Intel-CPU)
rechnet Lumen Filme deutlich schneller und stromsparender um. Dafür in der
Compose-Datei den Block zur Hardware einkommentieren: `/dev/dri` durchreichen
und die Gruppe von `renderD128` mitgeben (auf der Synology meist `937`,
sonst mit `ls -ln /dev/dri` nachsehen). NVIDIA braucht das NVIDIA Container
Toolkit und `gpus: all`. Was erkannt wurde, zeigt die Verwaltung unter
„Umwandeln“; klappt etwas nicht, fällt Lumen von selbst auf Software zurück.

## Einstellungen (Umgebungsvariablen)

Im Container stehen sie bei `environment`, ohne Docker gehen sie genauso
(oder als Schalter, zweite Spalte). Alles ist optional.

| Variable | Schalter | Vorgabe | Wofür |
|---|---|---|---|
| `TZ` | — | `Europe/Zurich` (in der Compose-Datei) | Zeitzone. **Anpassen**, falls du woanders wohnst: Danach richten sich Update-Fenster und nächtliche Sicherung |
| `PUID`, `PGID` | — | `1000` im Abbild, `1026`/`100` in der Compose-Datei | Benutzer, unter dem Lumen im Container läuft (nur Docker) |
| `LUMEN_ADDR` | `--addr` | `:8097` | Adresse und Port. Im Container `:8097` lassen und nur den Port nach aussen ändern |
| `LUMEN_DATA` | `--data` | `/data` im Container, sonst der Konfigurationsordner des Systems | Datenbank, Poster, Zwischenspeicher, Sicherungen |
| `LUMEN_UMWANDLUNG_DIR` | — | der Datenordner | Wohin die Zwischendateien beim Umwandeln kommen. Auf einem schwachen NAS gern in den Arbeitsspeicher: `tmpfs` (in der Compose-Datei vorbereitet) und `LUMEN_UMWANDLUNG_DIR=/umwandlung` |
| `LUMEN_MAX_UMWANDLUNGEN` | — | `2`, mit Hardware-Beschleunigung `4` | Wie viele Filme gleichzeitig umgewandelt werden dürfen. Auf einem ARM-NAS ohne Hardware eher `1` |
| `LUMEN_HARDWARE` | — | `auto` | Hardware-Beschleunigung: `auto`, `aus`, `vaapi`, `qsv` oder `nvenc`. Hat Vorrang vor der Einstellung in der Verwaltung (Umwandeln) |
| `LUMEN_VAAPI_GERAET` | — | `/dev/dri/renderD128` | Welches Grafikgerät VAAPI nutzt, falls es mehrere gibt |
| `LUMEN_FFMPEG`, `LUMEN_FFPROBE` | `--ffmpeg`, `--ffprobe` | `ffmpeg`, `ffprobe` | Pfad zu FFmpeg (im Abbild schon dabei) |
| `LUMEN_WEB` | `--web` | eingebaut | Web-App aus einem Ordner statt der eingebauten (zum Entwickeln) |
| `LUMEN_TLS` | `--tls` | aus | Jeder Wert schaltet den verschlüsselten Eingang (HTTPS) ein. Mit `LUMEN_RELAY` geht er von selbst an |
| `LUMEN_TLS_ADDR` | `--tls-addr` | `:8920` | Adresse des verschlüsselten Eingangs. Im Container den Port `8920` veröffentlichen, wenn Geräte im Heimnetz ihn direkt nutzen sollen |
| `LUMEN_TLS_NAME` | `--tls-name` | — | Name oder Adresse des NAS fürs eigene Zertifikat (im Container sieht Lumen nur seine Container-Adresse). Gilt auch als bekannter Name |
| `LUMEN_TLS_CERT`, `LUMEN_TLS_KEY` | `--tls-cert`, `--tls-key` | eigenes Zertifikat | Ein mitgebrachtes Zertifikat, etwa von Let's Encrypt; wird nach Erneuerung von selbst neu gelesen |
| `LUMEN_HOSTS` | — | — | Weitere Namen, unter denen Lumen erreichbar ist (Reverse-Proxy, DDNS wie `name.synology.me`), mit Komma getrennt. Sonst lehnt Lumen Anmeldung und Einrichtung unter diesem Namen ab (Schutz vor DNS-Rebinding) |
| `LUMEN_HEIMNETZ` | — | — | Welche Netze als „zu Hause“ gelten, etwa `192.168.1.0/24,fd00::/8`. Ohne Angabe gelten private Adressen — ausser den Gateways der Container-Netze, damit Anfragen von aussen über Docker nicht als „zu Hause“ durchgehen. Setzen, wenn Geräte im Heimnetz nach dem Update plötzlich als „unterwegs“ gelten |
| `LUMEN_OPENSUBTITLES_URL` | — | OpenSubtitles | Eigener Spiegel für den Untertitel-Download (selten nötig) |
| `LUMEN_RELAY` | `--relay` | aus | Adresse des Vermittlers für den Fernzugriff, etwa `meinserver.example:8444` |
| `LUMEN_RELAY_SECRET` | `--relay-secret` | — | Gemeinsames Geheimnis mit dem Vermittler |
| `LUMEN_RELAY_FINGERPRINT` | `--relay-fingerprint` | — | Fingerabdruck des Vermittler-Zertifikats (gibt der Vermittler beim Start aus) |
| `LUMEN_GITHUB_TOKEN` | — | — | *Optional.* Die Update-Prüfung braucht kein Token; nur für Updates aus einem privaten Repository (siehe „Aktuell bleiben"). Ein Token aus der Verwaltung hat Vorrang |
| `LUMEN_SOURCE_URL` | — | dieses Repository | Wo der Quelltext deiner Fassung liegt (AGPL, siehe [Lizenz](#lizenz)) |
| `LUMEN_TMDB_URL`, `LUMEN_TMDB_IMG_URL`, `LUMEN_OMDB_URL` | — | die echten Dienste | Andere Adressen für die Metadaten-Quellen — nur für Tests oder einen eigenen Spiegel. Absichtlich nicht in der Verwaltung einstellbar |
| `LUMEN_UPDATE_API` | — | `https://api.github.com` | Nur für Tests |

Der Vermittler (`lumen-relay`) kennt: `LUMEN_RELAY_SECRET` (Pflicht),
`LUMEN_RELAY_ADDR` (Besucher, `:8443`), `LUMEN_RELAY_TUNNEL` (Heimserver,
`:8444`), `LUMEN_RELAY_DATA` (Ordner fürs Zertifikat), `LUMEN_RELAY_NAME`,
`LUMEN_RELAY_CERT`/`LUMEN_RELAY_KEY` (eigenes Zertifikat),
`LUMEN_RELAY_STATUS` (Statusseite, Vorgabe aus),
`LUMEN_RELAY_MAX_VISITORS` und `LUMEN_RELAY_MAX_PER_IP` (Grenzen für
gleichzeitige Besucher).

---

## Sichern und Wiederherstellen

**Was Lumen selbst sichert.** Einmal am Tag, nach 4:30 Uhr, prüft Lumen
die Datenbank und legt eine Kopie in den Unterordner `sicherungen` des
Datenordners: `lumen-1.db` (neueste) und `lumen-2.db` (die davor). Genau
zwei — mehr wären ein Tagebuch darüber, wer wann wo im Film stand. Eine
beschädigte Datenbank überschreibt nie eine gute Sicherung. In der
Verwaltung unter „Sicherung" siehst du den Stand, kannst sofort sichern
und eine Sicherung wiederherstellen (Lumen startet dann neu).

**Was du dazu sichern solltest:** den ganzen Datenordner — auf einem
Synology `/volume1/docker/lumen/daten` mit **Hyper Backup** (Ordner in der
Auswahl anhaken). Am besten ein Zeitplan nach 5:00 Uhr: Dann ist die
Sicherung von Lumen frisch, und auf sie kommt es an — `lumen.db` selbst
ist im laufenden Betrieb in Bewegung, die Dateien in `sicherungen` nicht.
Poster und Bilder lassen sich jederzeit neu laden; wichtig sind
`sicherungen/` und `lumen.db`.

Zwei Dinge dabei: Die Sicherungen enthalten die Prüfwerte der PINs und das
Geheimnis der Authenticator-App — das Sicherungsziel verschlüsselt
ablegen. Und jede Version, die Hyper Backup aufhebt, ist wieder ein Stand
von „wer steht wo in welchem Film"; wer Lumens Zusage ernst nimmt, behält
dort nur wenige Versionen (*Rotation*).

**Wiederherstellen nach einem Fehler** (Datenbank kaputt, falsch
aufgeräumt): in der Verwaltung unter „Sicherung" die gewünschte wählen.
Startet Lumen gar nicht mehr, geht es auch von Hand: in der File Station
`daten/sicherungen/lumen-1.db` nach `daten/lumen.db.wiederherstellen`
kopieren und das Projekt neu starten. Lumen prüft die Datei und setzt sie
ein; die bisherige Datenbank wird dabei ersetzt.

**Zurück auf eine ältere Fassung** nach einem Update: Vor jedem Umbau der
Datenbank legt Lumen `daten/lumen.db.vor-umbau` an. Eine ältere Fassung
startet mit der umgebauten Datenbank absichtlich nicht (sie kennt den neuen
Aufbau nicht). Zurück geht es so: `lumen.db.vor-umbau` nach
`lumen.db.wiederherstellen` kopieren und die ältere Fassung starten — was
seit dem Update dazukam (Fortschritt, Merkliste …), fehlt dann.

**Auf ein neues NAS umziehen** oder nach einem Plattenausfall: Lumen
stoppen, den Datenordner aus Hyper Backup an dieselbe Stelle
zurückspielen, Projekt mit derselben `docker-compose.yml` erstellen. Die
Medien müssen unter denselben Pfaden liegen wie vorher, sonst meldet Lumen
die Titel als „fehlend".

### Vom Volume in einen Ordner umziehen

Nur für Installationen mit `lumen-daten:/data` (siehe oben), und nur wenn
du die Daten in File Station und Hyper Backup sehen willst. Sonst: einfach
die alte Datei behalten.

1. Ordner `/volume1/docker/lumen/daten` anlegen (File Station).
2. Lumen stoppen (*Projekt › lumen › Aktion › Stoppen*).
3. Die Daten kopieren — per SSH oder als einmalige geplante Aufgabe
   (Benutzer root, siehe oben). Den genauen Namen des Volumes zeigt
   `docker volume ls`; der Container Manager stellt den Projektnamen
   voran, meist `lumen_lumen-daten`:

   ```bash
   docker run --rm --entrypoint cp \
     -v lumen_lumen-daten:/von -v /volume1/docker/lumen/daten:/nach \
     ghcr.io/ada462790-beep/lumen-media:latest -a /von/. /nach/
   ```

4. In der `docker-compose.yml` die Zeile `- lumen-daten:/data` durch
   `- /volume1/docker/lumen/daten:/data` ersetzen; den Block `volumes:` /
   `lumen-daten:` ganz unten **stehen lassen**, damit das alte Volume nicht
   verwaist.
5. Projekt neu *Erstellen*, anmelden, prüfen: Konten, Bibliotheken,
   Merklisten sind da.
6. Erst wenn alles stimmt — ein paar Tage später — das alte Volume
   löschen (*Container Manager › Volume*) und den Block unten entfernen.

Geht etwas schief: Zeile zurück auf `lumen-daten:/data`, neu erstellen —
das Volume ist unberührt.

---

## Von unterwegs schauen

Zu Hause genügt die Adresse im Heimnetz. Von unterwegs steht der Router im
Weg, und der übliche Rat lautet: einen Port freigeben. Das heisst, den
eigenen Server ins offene Netz zu stellen, wo ihn jeder Scanner findet.

> **Port 8097 (und 8920) nie im Router freigeben.** Lumen unterscheidet
> „zu Hause" und „unterwegs" an der Absender-Adresse (siehe unten), und
> hinter Docker stimmt die nicht immer. Für unterwegs gibt es den
> Vermittler — oder Tailscale/WireGuard.

Lumen macht es umgekehrt. Auf einer Maschine mit öffentlicher Adresse — der
billigste Mietserver genügt — läuft ein kleines Programm, der **Vermittler**.
Der Heimserver wählt sich bei ihm ein und hält die Leitung offen. Kommt ein
Besucher beim Vermittler an, sagt der über diese Leitung Bescheid, und der
Heimserver baut von sich aus eine zweite Verbindung auf.

```
   dein Handy  ───►  Vermittler  ◄───  dein Server  ───►  Lumen
   (unterwegs)      (öffentlich)      (wählt sich         (zu Hause)
                                       nach aussen)
```

Für den Router ist das eine ganz gewöhnliche ausgehende Verbindung, wie sie
jeder Browser aufbaut. Es muss nichts freigegeben werden, und es gibt nichts,
was jemand von aussen anklopfen könnte.

**Der Vermittler sieht nicht, was du schaust.** Lumen verschlüsselt selbst,
der Schlüssel bleibt zu Hause. Durch den Vermittler laufen Bytes, mit denen
er nichts anfangen kann — keine Adressen, keine Titel, keine Namen. Das ist
kein Versprechen, sondern eine Folge der Bauweise, und ein Test schickt einen
Titel durch die ganze Kette und sieht nach, was beim Vermittler ankommt.
Dieselbe Regel wie überall sonst in Lumen: der Besitzer sieht nicht, wer was
schaut; wer den Vermittler betreibt, erst recht nicht.

### Einrichten

Auf dem Mietserver:

```bash
LUMEN_RELAY_SECRET="$(head -c 32 /dev/urandom | base64)" ./lumen-relay
```

Er gibt beim Start einen **Fingerabdruck** aus. Den und das Geheimnis trägst
du zu Hause ein:

```bash
./lumen \
  --relay meinserver.example:8444 \
  --relay-secret "dasselbe-geheimnis" \
  --relay-fingerprint "60:62:a4:…"
```

Danach ist Lumen unter `https://meinserver.example:8443` erreichbar.

**Mit Docker:** Auf dem Mietserver genügt die Datei
[`docker-compose.relay.yml`](docker-compose.relay.yml) — sie nimmt das
fertige, öffentliche Abbild `ghcr.io/ada462790-beep/lumen-media-relay`;
ein Quelltext und eine Anmeldung bei ghcr.io sind dort nicht nötig. Das
Geheimnis kommt in eine Datei
`.env` daneben (`LUMEN_RELAY_SECRET=…`), der Fingerabdruck steht nach dem
Start in `docker compose -f docker-compose.relay.yml logs`. Zu Hause die
drei Zeilen `LUMEN_RELAY`, `LUMEN_RELAY_SECRET` und
`LUMEN_RELAY_FINGERPRINT` in der `docker-compose.yml` einkommentieren.
Die Ports 8443 und 8444 öffnest du auf dem Mietserver, nicht zu Hause.

Sobald `LUMEN_RELAY` gesetzt ist, schaltet Lumen die Verschlüsselung von
selbst ein (auch den Eingang auf 8920). Für den Vermittler muss dieser
Port im Container nicht veröffentlicht sein — der Vermittler bekommt
einen eigenen, nur im Container erreichbaren Eingang.

### Was von unterwegs anders ist

Lumen erkennt, ob eine Anfrage von zu Hause kommt oder von unterwegs (über
den Vermittler, einen Reverse-Proxy oder eine Adresse ausserhalb des
Heimnetzes). Von unterwegs gilt:

- **Keine Profilauswahl.** Sie verriete die Namen im Haushalt. Man meldet
  sich mit Profilname und PIN oder Passwort an, mit einem Code vom
  Fernseher oder mit einer Einladung.
- **Ein Profil ohne PIN lässt sich von aussen nicht öffnen.** Wer ein
  Kinderprofil ohne PIN hat, meldet das Gerät einmal zu Hause an — danach
  funktioniert es auch unterwegs.
- **Durchprobieren lohnt sich nicht.** Nach drei falschen Versuchen wächst
  die Pause (30 Sekunden, 1, 2, 4 … Minuten, höchstens eine Stunde).
- **Einrichten geht nur zu Hause.**

Geräte, die schon angemeldet sind, merken davon nichts.

**Reverse-Proxy (Synology, Nginx Proxy Manager, Traefik, Caddy …):** Das
geht, aber Lumen behandelt dann jede Anfrage als „von unterwegs" — woran
es sie erkennt, ist die Kopfzeile `X-Forwarded-For`, die der Proxy setzen
**muss**. Den Namen, unter dem Lumen erreichbar ist, trägst du in
`LUMEN_HOSTS` ein (mehrere mit Komma), sonst lehnt Lumen die Anmeldung ab —
das schützt vor einem Angriff, bei dem eine fremde Webseite ihren Namen auf
dein NAS umbiegt (DNS-Rebinding).

*Synology mit DDNS* (`name.synology.me`) und eingebautem Reverse-Proxy:
*Systemsteuerung › Anmeldeportal › Erweitert › Reverse Proxy › Erstellen*,
Quelle `HTTPS`, `name.synology.me`, Port 443; Ziel `HTTP`, `localhost`,
Port `8097`. Unter *Benutzerdefinierte Kopfzeile* „WebSocket" hinzufügen
und dazu eine Zeile `X-Forwarded-For` mit dem Wert
`$proxy_add_x_forwarded_for`. In der `docker-compose.yml`:
`LUMEN_HOSTS=name.synology.me`. Danach von aussen (etwa übers Handynetz)
prüfen: Es darf **keine Profilauswahl** erscheinen, nur die Anmeldung mit
Name und PIN. Erscheint sie doch, kommt die Kopfzeile nicht an — dann den
Proxy abschalten, bis das stimmt.

**Achtung, Docker und die Absender-Adresse.** „Zu Hause" heisst für Lumen:
die Anfrage kommt von einer privaten Adresse (192.168.x, 10.x, 172.16–31.x,
fe80::, localhost) und ohne Proxy-Kopfzeilen. Docker reicht die echte
Adresse aber nicht immer durch: Bei IPv6-Zugriffen, bei manchen
Netzwerk-Einstellungen des NAS und bei einem Proxy auf demselben Gerät
sieht Lumen statt des Absenders die Adresse des Docker-Netzes (etwa
`172.17.0.1`) — und die ist privat. Eine Anfrage aus dem Internet gälte
dann als „zu Hause": mit Profilauswahl, ohne PIN-Pflicht, mit Einrichten.
Darum: Lumens Ports nie im Router freigeben, und einen Reverse-Proxy nur
mit `X-Forwarded-For` betreiben.
Zugangsschlüssel stehen bei Lumen nie in Adressen, also auch nicht im
Protokoll des Proxys; Bilder und Videos tragen nur einen kurzlebigen
Medienschlüssel, der zu niemandem zurückführt.

**Tailscale oder WireGuard** sind ein gleichwertiger Weg nach Hause, ganz
ohne Vermittler. Auch dann gelten die Regeln für unterwegs.

**Bandbreite:** Alles, was unterwegs geschaut wird, läuft zweimal durch den
Mietserver des Vermittlers (rein und raus). Ein Film in 1080p sind etwa
3–6 GB pro Stunde; das sollte der Tarif des Mietservers hergeben.

### Das Zertifikat

Sobald der Fernzugriff an ist, schaltet Lumen die Verschlüsselung von selbst
ein. Ein Heimserver hat aber selten einen Namen, für den eine öffentliche
Stelle ein Zertifikat ausstellt — also stellt Lumen sich selbst eines aus.

Die Apps nageln es fest: in der Verwaltung unter **Von aussen erreichbar**
steht ein Fingerabdruck, den trägt man beim Verbinden einmal ein, und danach
gilt genau dieses eine Zertifikat. Das ist strenger als das Übliche, nicht
lockerer — ein öffentliches Zertifikat akzeptiert jedes, das irgendeine der
hundert eingebauten Stellen unterschrieben hat.

Im Browser erscheint beim ersten Mal eine Warnung, durch die man einmal
hindurchklickt. Wer das nicht will, legt ein richtiges Zertifikat daneben
(`--tls-cert`/`--tls-key`, etwa von Let's Encrypt über DNS-01); Lumen nimmt
dann das und liest es neu ein, sobald es erneuert wurde — ein Neustart ist
nicht nötig. Mit einem öffentlichen Zertifikat in den Apps den Fingerabdruck
nicht festnageln: Er ändert sich mit jeder Erneuerung.

---

## Die Datenschutz-Zusage im Code

Das ist kein Abschnitt in den Einstellungen, sondern die Bauweise:

| Zusage | Wie sie abgesichert ist |
|---|---|
| Keine Abfrage über mehrere Profile hinweg | Ein Test liest den Quelltext und verlangt, dass jede Abfrage auf Fortschritt, Bewertungen und Merkliste genau ein Konto nennt |
| „Wer schaut am meisten" ist nicht beantwortbar | Ein Test verbietet `count`, `sum` und `GROUP BY` auf diesen Tabellen |
| Es gibt keinen Wiedergabe-Verlauf | Ein Test verbietet die entsprechenden Tabellen im Schema |
| Der Besitzer sieht nur die Last — nicht Titel, Person, Gerät, Ort oder Startzeit | Was er zu sehen bekommt, ist eine eigene Struktur; ein Test lässt genau deren fünf Felder zu und keines mehr |
| Auch im Betrieb nicht | Ein Test spielt wirklich einen Film ab und durchsucht die Antwort an den Besitzer nach Titel, Pfad, Kennung, Person und Gerät |
| Keine Spuren auf der Platte | Ein Test spielt einen Film samt Untertiteln ab und prüft jede neu entstandene Datei: kein Name darf auf den Film zeigen |
| Keine Uhrzeiten, die mit Schauen zusammenhängen | Der Fortschritt trägt eine laufende Nummer statt einer Uhrzeit; Geräte merken sich nur den Tag |
| Das Protokoll ist kein Verlauf | Beim Abspielen steht dort nur „Umwandlung gestartet" und der technische Grund — kein Gerät, keine Person, keine Datei |
| Bewertungen nur mit Freigabe | Ohne Häkchen taucht nichts im Haushalt auf |

Wer das ändern will, muss Tests löschen. Das soll man merken.

---

## Wie es gebaut ist

```
server/     Go — der Server, die Web-App ist mit eingebaut
  cmd/lumen/web/    Oberfläche (reines JavaScript, kein Bauschritt)
  cmd/lumen-relay/  Der Vermittler für den Fernzugriff
  internal/store/   Datenbank und Abfragen
  internal/scan/    Dateinamen lesen
  internal/meta/    TMDb, OMDb, Zuordnen
  internal/stream/  Abspielen, Umwandeln
  internal/relay/   Tunnel nach draussen, ohne Portfreigabe
  internal/cert/    Legt bei Bedarf ein eigenes Zertifikat an
  internal/api/     HTTP-Schnittstelle
```

Eine einzige Abhängigkeit (SQLite), sonst nur die Standardbibliothek. Die
Oberfläche steckt in der Binärdatei: wer Lumen weitergibt, gibt eine Datei
weiter — plus die Lizenz daneben, siehe unten. Die fertigen Pakete und das
Docker-Abbild haben sie schon drin.

```
apple/      Swift — iPhone, iPad, Mac und Apple TV
  LumenKit/   Verbindung, Modelle, Anmeldung, Wiedergabe (ohne Oberfläche)
  Lumen/      Oberfläche für iPhone, iPad, Mac
  LumenTV/    Oberfläche für Apple TV
tools/      Prüft, ob App und Server zusammenpassen; zeichnet die Symbole
```

```
android/    Kotlin — Handy, Tablet, Android TV und Fire TV
  lumen-core/ Verbindung, Modelle, Anmeldung — ohne Android, darum testbar
  app/        Oberfläche für Handy und Tablet
  tv/         Oberfläche für Android TV und Fire TV
```

---

## Lizenz

**AGPL-3.0-only.** Der ganze Text steht in [LICENSE](LICENSE), und jede
Quelldatei nennt ihn in der Kopfzeile.

Was das heisst, in Klartext:

- Du darfst Lumen benutzen, so lange und so oft du willst, privat und
  gewerblich. Kein Schlüssel, keine Anmeldung, keine Zählung.
- Du darfst es verändern und weitergeben — an Freunde, in ein Forum, auf
  deine Webseite.
- Wer es weitergibt oder betreibt, muss den Quelltext mitgeben, samt seiner
  Änderungen, unter derselben Lizenz.

Der letzte Punkt ist der Grund für die AGPL und nicht die GPL. Lumen ist ein
Dienst, den man über das Netz benutzt — bei der gewöhnlichen GPL könnte
jemand eine verbesserte Fassung als Bezahldienst betreiben und nichts
zurückgeben, weil er die Software nie *weitergibt*. §13 der AGPL schliesst
das: wer Lumen für andere über das Netz betreibt, schuldet denen den
Quelltext.

Lumen erfüllt das von selbst. Jede Antwort von `GET /api/v1/hello` nennt die
Lizenz und die Fundstelle, und in den Einstellungen steht unter **Über
Lumen** ein Link dorthin. Wenn du etwas änderst, trag deine eigene Fundstelle
ein — sonst zeigt Lumen auf diese hier:

```
LUMEN_SOURCE_URL=https://git.example.org/meine-lumen-fassung
```

Das Urheberrecht liegt bei den Lumen-Autoren — so steht es in jeder
Kopfzeile: „Copyright (C) 2026 Die Lumen-Autoren". Die AGPL gibt Rechte
dazu, sie nimmt keine weg. Ohne Gewährleistung — wie bei jeder freien
Software. Wer etwas beiträgt, behält das Urheberrecht an seinem Beitrag
und stimmt der [CLA](CLA.md) zu (siehe [Mitmachen](#mitmachen)).

FFmpeg wird nur aufgerufen, nicht eingebaut, und bleibt unter seiner eigenen
Lizenz. Eingebaute Fremdbibliotheken: `go-sqlite3` (MIT) und `go-qrcode`
(MIT, für den QR-Code der Authenticator-App).

---

## Mitmachen

Fehler melden und Wünsche äussern geht über die
[Issues](https://github.com/ada462790-beep/lumen-media/issues) — mit den
Vorlagen dort. **Sicherheitslücken bitte nicht als Issue**, sondern
vertraulich über *Security › Report a vulnerability*, siehe
[SECURITY.md](SECURITY.md).

Beiträge sind willkommen. Wie man beiträgt, welche Prüfskripte vor einem
Pull-Request laufen und welche Regeln gelten (deutsche Bezeichner, Port
8097, Übersetzungen in allen fünf Sprachen, die Datenschutzregeln), steht
in [CONTRIBUTING.md](CONTRIBUTING.md). Für jeden Pull-Request gilt die
[CLA](CLA.md): Du behältst dein Urheberrecht und erlaubst den
Lumen-Autoren, deinen Beitrag auch unter anderen Lizenzen weiterzugeben.
Im Pull-Request setzt du dafür ein Häkchen; ein kleiner Ablauf prüft es.
Es gilt der [Verhaltenskodex](CODE_OF_CONDUCT.md).

**Was in einem Pull-Request läuft:** „Prüfen" (Server, Tests, Übersetzungen,
Lizenzköpfe), „Apps übersetzen" (Android und Apple, wenn sich dort etwas
ändert) und „CLA". Alle drei nur mit Leserechten und ohne Geheimnisse —
ein Pull-Request aus einem Fork braucht nichts weiter.

---

## Die Apps bauen, ohne Mac und ohne Android Studio

Server und Weboberfläche lassen sich überall bauen. Die vier App-Oberflächen
nicht: Android braucht das Android-SDK, Apple braucht einen Mac mit Xcode.

Beides steht auf den Maschinen von GitHub bereit. Schieb das Projekt dorthin,
und `.github/workflows/apps.yml` übersetzt alles:

```bash
git push
```

Ein paar Minuten später liegen unter **Actions → Apps übersetzen → Artifacts**
zwei fertige Dateien — und bei einer Veröffentlichung hängen dieselben zwei
gleich mit an den Dateien dran:

| | |
|---|---|
| `lumen-handy.apk` | Handy und Tablet |
| `lumen-fernseher.apk` | Android TV und Fire TV |

Die APKs lassen sich direkt aufs Gerät schieben (`adb install lumen-handy.apk`)
oder auf dem Fernseher über eine Datei-App installieren — „Unbekannte Quellen"
muss dafür einmal erlaubt sein.

Es sind Debug-Fassungen, und das ist Absicht: eine Release-Fassung muss mit
einem Schlüssel signiert werden, und der gehört dem, der die App herausgibt
— dir, nicht einem Bauablauf. Zum Verschenken im Bekanntenkreis genügt die
Debug-Fassung. Für den Play Store brauchst du einen eigenen Schlüssel.

Für Apple wird **übersetzt, aber nicht verpackt**. Eine `.ipa` braucht ein
Entwicklerkonto und eine Signatur, und die gehören dir, nicht einem
Bauablauf. Der Ablauf sagt dir aber, ob der Code fehlerfrei durchgeht — und
genau das ist der Teil, den man ohne Mac nicht herausfindet.

### Eine Veröffentlichung anlegen

Zwei Wege, je nachdem, was du darfst:

```bash
git tag v0.1.0 && git push --tags
```

Oder unter **Actions → Veröffentlichung → Run workflow** die Fassungsnummer
eintragen (etwa `v0.1.0`) und starten. Das legt den Tag mit an. Lässt man
das Feld leer, wird nur gebaut und nichts veröffentlicht — praktisch, um
vorher zu sehen, ob alles durchgeht. Die Fassung muss genau so aussehen:
`v` und drei Zahlen; alles andere lehnt der Ablauf ab.

Veröffentlichen kann nur der Besitzer des Repositorys: Der Ablauf braucht
das Geheimnis `LUMEN_SIGNIER_SCHLUESSEL` (*Settings › Secrets and
variables › Actions*). In einem Fork fehlt es, und der Ablauf bricht an
dieser Stelle mit einer klaren Meldung ab — der Rest (bauen, testen) läuft
trotzdem.

Veröffentlicht wird erst, wenn alles durch ist: Tests, alle Plattformen,
die Android-Apps und die Apple-Tests (die nur dann nicht, wenn ausdrücklich
„ohne Mac" gewählt ist). `pruefsummen.txt` nennt in der ersten Zeile die
Fassung (`# lumen-version: v0.1.0`) und wird mit dem Geheimnis
`LUMEN_SIGNIER_SCHLUESSEL` unterschrieben; gleich danach prüft der Ablauf
die Unterschrift so, wie ein Server es beim Update tut — gegen den
öffentlichen Schlüssel, der in Lumen eingebaut ist. Passen die beiden
nicht zusammen, scheitert die Veröffentlichung dort und nicht später auf
jedem Server.

Die Docker-Abbilder haben einen eigenen Ablauf (**Abbilder**). Er startet
bei jedem geschobenen Versions-Tag, lässt zuerst die Tests laufen und
baut erst danach. `:latest` bekommt nur die neueste Fassung. Klemmt an
einem Abbild etwas, lässt es sich allein neu bauen (*Run workflow* mit der
Fassung), ohne den ganzen Veröffentlichungslauf samt sechs Plattformen zu
wiederholen. **Wichtig:** Wurde die Veröffentlichung von Hand gestartet
(Tag vom Ablauf angelegt), startet GitHub den Ablauf „Abbilder" nicht von
selbst — dann ihn einmal von Hand mit derselben Fassung starten.

Heraus kommt eine Seite mit allem darauf: der Server für sechs Plattformen,
der Vermittler, beide APKs, Prüfsummen. Die Zwischenergebnisse der Abläufe
(Artefakte) bleiben nur drei Tage liegen, damit der Speicher dafür nicht
voll läuft.

Lokal geht beides natürlich auch:

```bash
cd android && gradle :app:assembleDebug        # braucht das Android-SDK
cd apple && xcodegen && open Lumen.xcodeproj   # braucht einen Mac
```

---

## Noch offen

- Die Metadaten-Quellen sind gegen einen nachgebauten Dienst getestet, nicht
  gegen die echten. Einmal mit eigenem Schlüssel gegenprüfen.
