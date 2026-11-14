# Lumen

*[English summary](README.en.md)*

Dein eigener Streaming-Dienst für zu Hause. Filme und Serien von der
eigenen Platte, auf jedem Gerät im Haus — ruhig, aufgeräumt, mit einer
Oberfläche, die aussieht, als hätte Apple sie gebaut. Lumen ist freie
Software (AGPL-3.0), läuft als ein einziger Docker-Container auf dem NAS
und braucht weder Konto noch Abo noch Cloud.

Lumen ist bewusst ein Server für **Filme und Serien** — und nur dafür.
Musik- und Fotobibliotheken gibt es nicht.

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

> **Anleitung als PDF:** [Lumen-Installationsanleitung.pdf](docs/Lumen-Installationsanleitung.pdf) —
> Schritt für Schritt auf einem Synology-NAS, ohne Terminal (zum Ausdrucken).

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
- **Netz:** `network_mode: host` bleibt so stehen. Lumen läuft damit im
  Netz des NAS und sieht die echten Adressen der Geräte — nur so weiss es,
  wer zu Hause ist (siehe [unten](#zu-hause-oder-unterwegs-docker-und-die-adressen)).
  Ein `ports:`-Block gehört dann nicht dazu; Lumen hört direkt auf Port
  8097 des NAS.

Ein Webportal über Web Station braucht es nicht. *Fertig* — der Container
Manager lädt das Abbild und startet Lumen.

**3. Firewall.** Ist die Firewall des NAS an (*Systemsteuerung › Sicherheit
› Firewall*), unter *Regeln bearbeiten* eine Regel anlegen: Port
**8097/TCP**, Quelle **nur dein Heimnetz** (etwa `192.168.1.0/24`),
*Zulassen*. Wer HTTPS auf 8920 nutzt, gibt diesen Port genauso frei — auch
nur fürs Heimnetz. Mit `network_mode: host` gilt die Firewall des NAS für
Lumen wie für jeden anderen Dienst; ohne diese Regel kommt dann niemand
an Lumen heran.

**4. Öffnen.** `http://<adresse-des-nas>:8097` im Browser. Beim ersten Start
legst du dich als Besitzer an und sagst Lumen, wo die Filme liegen: unter
„Durchsuchen" stehen die Ordner, die in der `docker-compose.yml` bei
`volumes` freigegeben sind, ganz oben.

**Aktualisieren:** *Projekt › lumen › Aktion › Erstellen*. Weil in der
Datei `pull_policy: always` steht, holt der Container Manager dabei das
neue Abbild; die Daten im Ordner `daten` bleiben. Alte, nicht mehr
benutzte Abbilder kann man unter *Image* löschen. Per Knopfdruck in der
Verwaltung oder von selbst nachts: siehe
[Updates per Knopfdruck](#updates-per-knopfdruck).

#### Zu Hause oder unterwegs: Docker und die Adressen

Ob ein Gerät zu Hause ist, erkennt Lumen an seiner Adresse. Zu Hause gibt
es die Profilauswahl, das Einrichten, die Übernahme aus Emby oder Jellyfin
und keine Qualitätsgrenze für unterwegs. Im Bridge-Betrieb (der alte
Block `ports: - "8097:8097"`) reicht Docker auf manchen NAS — auch auf
Synology — jede Verbindung über das Gateway des Container-Netzes weiter
(`172.17.0.1` o. ä.). Dann sieht Lumen für alle Geräte dieselbe Adresse
und behandelt sicherheitshalber alle als unterwegs. Typische Meldung: „Die
Übernahme aus Emby oder Jellyfin geht nur zu Hause im Heimnetz."

Was Lumen sieht, steht in der **Verwaltung unter „Dieses Gerät"**: die
Adresse, ob sie als zu Hause gilt und warum. Kommen Anfragen über das
Gateway von Docker, steht dort ein Hinweis.

**Umstellen** (Container Manager › Projekt › lumen › *Stoppen*, dann
*YAML-Konfiguration*):

1. Beim Dienst `lumen` den Block `ports:` samt seinen Zeilen löschen und
   stattdessen `network_mode: host` eintragen (auf derselben Höhe wie
   `restart`). `LUMEN_ADDR=:8097` bleibt.
2. Ist der Aktualisierer eingeschaltet: bei Lumen
   `LUMEN_AKTUALISIERER_URL=http://127.0.0.1:8080`, beim Aktualisierer
   `ports:` mit der Zeile `- "127.0.0.1:8080:8080"` (siehe
   [Updates per Knopfdruck](#updates-per-knopfdruck)).
3. Läuft Emby oder Jellyfin auf demselben NAS mit HTTPS und hat Lumen
   HTTPS an (`LUMEN_TLS` oder Fernzugriff): `LUMEN_TLS_ADDR=:8921`, denn
   8920 ist dann schon belegt.
4. Firewall des NAS: Port 8097 fürs Heimnetz erlauben (Schritt 3 oben).
5. *Speichern*, dann *Aktion › Erstellen*. In der Verwaltung unter
   „Dieses Gerät" nachsehen: Dort sollte jetzt die Adresse des Geräts
   stehen (etwa `192.168.1.35`) und „Gilt als zu Hause".

Sicher bleibt es trotzdem: Anfragen vom NAS selbst (etwa über seinen
Reverse-Proxy), aus anderen Containern, über den Router, mit
`X-Forwarded-For` oder über den Vermittler gelten weiter als unterwegs.

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

### Updates per Knopfdruck

Im Container kann sich Lumen nicht selbst ersetzen — das Abbild wird als
Ganzes getauscht. Das übernimmt ein kleiner Begleiter, der
**Aktualisierer** ([Watchtower](https://github.com/nicholas-fedor/watchtower)
in der gepflegten Fassung von Nick Fedor). Ist er eingerichtet, zeigt die
Verwaltung unter *Updates* den Knopf **Jetzt aktualisieren**, und „Von
selbst aktualisieren" geht auch im Container.

Was beim Drücken passiert: Lumen fragt nach, falls gerade jemand schaut,
und will deine PIN oder dein Passwort noch einmal. Dann schreibt es die
Datenbank sauber auf die Platte, trägt das Update ins Protokoll ein und
sagt dem Aktualisierer „jetzt". Der holt das neue Abbild, stoppt Lumen
(30 Sekunden Zeit zum sauberen Herunterfahren), startet es mit dem neuen
Abbild und löscht das alte. Die Seite zeigt solange „Wird aktualisiert" und
meldet sich, sobald die neue Fassung läuft. Nachts im Zeitfenster
(Verwaltung › Updates › *Von selbst aktualisieren*) läuft es genauso —
aber nur, wenn niemand schaut.

**Sicherheit:** Nur der Aktualisierer bekommt Zugriff auf Docker
(`/var/run/docker.sock`), Lumen nicht. Er hört nur auf `127.0.0.1:8080`
des NAS selbst (andere Geräte erreichen ihn nicht) und nimmt nur Anfragen
mit dem geheimen Token an. Lumen kann ihm damit genau eines sagen: „aktualisiere jetzt".
Er tauscht nur Container mit dem Label
`com.centurylinklabs.watchtower.enable=true` — also nur Lumen. Das Token
steht in keiner Antwort, keinem Protokoll und keiner Fehlermeldung; die
Adresse lässt sich nur in der `docker-compose.yml` festlegen, nicht in der
Oberfläche.

In der `docker-compose.yml` dieses Repositorys sind die Zeilen beim Dienst
`lumen` schon drin; der Aktualisierer selbst steht auskommentiert darunter,
weil er Zugriff auf Docker bekommt — einschalten soll ihn jeder bewusst.
Wer ihn will (oder eine ältere Datei hat), geht so vor:

**1. Das Token anlegen** — eine Zeile in der Datei `.env` neben der
`docker-compose.yml`. Ohne SSH: *Systemsteuerung › Aufgabenplaner ›
Erstellen › Geplante Aufgabe › Benutzerdefiniertes Skript*, Benutzer
**root**, unter *Aufgabeneinstellungen* dieses Skript:

```sh
grep -qs LUMEN_AKTUALISIERER_TOKEN /volume1/docker/lumen/.env || \
  echo "LUMEN_AKTUALISIERER_TOKEN=$(openssl rand -hex 32)" >> /volume1/docker/lumen/.env
```

Speichern, die Aufgabe markieren, *Ausführen* — danach kann sie wieder
weg. Per SSH ist es derselbe Befehl. Es geht auch jede andere zufällige
Zeichenfolge aus 64 Buchstaben und Ziffern. Wer keine `.env` mag, schreibt
das Token an beiden Stellen unten direkt statt `${LUMEN_AKTUALISIERER_TOKEN:-}`
hin.

**2. Die `docker-compose.yml` ergänzen:** *Container Manager › Projekt ›
lumen*, *Stoppen*, dann *YAML-Konfiguration* bearbeiten. Beim Dienst
`lumen` dazu (`labels` auf derselben Höhe wie `restart`, die zwei Zeilen
unter `environment`):

```yaml
    labels:
      - com.centurylinklabs.watchtower.enable=true
    environment:
      # … was schon da ist, und dazu:
      - LUMEN_AKTUALISIERER_URL=http://127.0.0.1:8080
      - LUMEN_AKTUALISIERER_TOKEN=${LUMEN_AKTUALISIERER_TOKEN:-}
```

Und unter `services:` einen zweiten Dienst, auf derselben Höhe wie
`lumen:` (zwei Leerzeichen eingerückt):

```yaml
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
```

Lumen läuft im Netz des NAS (`network_mode: host`) und erreicht den
Aktualisierer darum über `127.0.0.1:8080`, nicht über seinen Namen. Die
Zeile `127.0.0.1:8080:8080` gibt den Port nur auf dem NAS selbst frei,
nicht im Heimnetz. Ist 8080 auf dem NAS schon belegt, links eine andere
Zahl nehmen (etwa `127.0.0.1:18080:8080`) und bei Lumen
`http://127.0.0.1:18080`. Wer Lumen noch im Bridge-Betrieb mit `ports:`
laufen hat, kann bei `http://aktualisierer:8080` bleiben.

**3. Speichern, dann *Aktion › Erstellen*.** Der Container Manager holt
den Aktualisierer und startet beide. In der Verwaltung unter *Updates*
erscheint der Knopf, sobald es eine neuere Fassung gibt (*Jetzt prüfen*).

Gut zu wissen:

- Von selbst tut der Aktualisierer nichts — mit eingeschalteter
  HTTP-Schnittstelle sind seine eigenen Zeitpläne aus. Nachts aktualisiert
  Lumen über sein Zeitfenster, weil nur Lumen weiss, ob gerade jemand
  schaut. (Wer trotzdem einen festen Zeitplan will:
  `WATCHTOWER_HTTP_API_PERIODIC_POLLS=true` und
  `WATCHTOWER_SCHEDULE=0 0 4 * * *` — dann aber ohne Rücksicht auf
  laufende Wiedergaben.)
- Die Fassung des Aktualisierers ist fest (`1.24.0`). Ab und zu eine
  neuere eintragen und *Erstellen* — er aktualisiert sich nicht selbst.
- Der Synology meldet beim Update womöglich, der Container „lumen" sei
  unerwartet gestoppt worden. Das ist der Aktualisierer und harmlos.
- Klappt es nicht, sagt die Seite warum: *Token abgelehnt* (die beiden
  Token-Zeilen müssen denselben Wert bekommen — steht die `.env` am
  richtigen Ort?), *nicht erreichbar* (läuft `lumen-aktualisierer` im
  selben Projekt, mit `127.0.0.1:8080:8080` bei `ports`?), *kein neueres Abbild* (es wird nach einer
  Veröffentlichung noch ein paar Minuten gebaut). Näheres steht im
  Protokoll des Containers `lumen-aktualisierer`.
- Weil das Abbild öffentlich ist, braucht der Aktualisierer keine
  Zugangsdaten. Nur wer ein eigenes, privates Abbild nutzt, trägt dort
  `REPO_USER` (GitHub-Name) und `REPO_PASS` (Token mit `read:packages`) ein.

Ohne Aktualisierer bleibt es beim Weg oben: *Projekt › lumen › Aktion ›
Erstellen* (mit `pull_policy: always`) oder per SSH
`sudo docker compose pull && sudo docker compose up -d`.

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
Verwaltung eintragen oder `LUMEN_GITHUB_TOKEN` setzen. Im Container setzt
Lumen nichts selbst ein: dort holt der Aktualisierer das neue Abbild (siehe
[Updates per Knopfdruck](#updates-per-knopfdruck)).

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

**Filme in Teilen.** `Film a.avi`, `Film b.avi`, `Film c.avi` (mit oder
ohne Jahr im Namen, sobald mindestens zwei Teile im selben Ordner liegen)
oder `CD1`, `CD2` erkennt Lumen als einen Film. In der Verwaltung lassen sich die
Teile zu einer Datei zusammenführen — ohne neu zu rechnen, die Teile
wandern in den Unterordner `.lumen-teile`. Dafür sind die Medienordner
mit `:rw` eingehängt.

**Lokale Metadaten.** NFO-Dateien (Kodi, Emby, Jellyfin, tinyMediaManager)
und Bilder neben den Dateien (`poster.jpg`, `fanart.jpg`, `logo.png`,
`landscape.jpg`, Staffelposter, Folgenbilder auch in `metadata/`)
übernimmt Lumen mit; was du dort gepflegt hast, geht beim Umzug nicht
verloren. Aus der NFO kommen auch Besetzung (mit Bildern aus `.actors/`),
Regie, Drehbuch, Studio, Land, Schlagworte, Tagline und die Altersfreigabe
in jeder Emby-Schreibweise (`DE-16`, `CH-12`, `FSK 16`, `PG-13` …); eine
Sammlung aus `<set>` wird zur Filmreihe mit ihrem Namen aus Emby.
**Ohne TMDb-Schlüssel** gilt ein Titel mit vollständiger NFO (Titel,
Inhalt, Jahr) als erkannt; Kennungen aus NFO und Namen werden trotzdem
gespeichert, und kommt später ein Schlüssel dazu, holt Lumen diese Titel
einmal nach. Extras (Ordner `trailers/`, `featurettes/`, `behind the
scenes/`, `deleted scenes/`, `interviews/`, `scenes/`, `shorts/`, `clips/`,
`extras/`, `other/` oder Endungen wie `-trailer`, `-featurette`, `-clip`)
werden nie zu Filmen; die Titelseite bietet sie unter „Extras" zum
Abspielen an. Die Verwaltung kann jeden Titel auch von Hand bearbeiten —
was du änderst, bleibt gesperrt und wird nicht mehr überschrieben.

**Zuordnen: Welcher Film ist das?** Lumen sucht bei TMDb mit dem Titel und
Jahr aus dem Datei- oder Ordnernamen. Bringt das nichts Sicheres, versucht
es weitere Schreibweisen: ohne Fassungswörter („Director's Cut",
„Extended", „Kinofassung", „3D", „IMAX" …), „Herr der Ringe, Der"
umgestellt, „&" als „und"/„and", römische Zahlen als Ziffern und
umgekehrt („Teil II" / „Teil 2"), ohne Satzzeichen, „oe" als „ö" und
umgekehrt, die englischen Titel und die Alternativtitel von TMDb, das Jahr
eins daneben. Genommen wird ein Treffer nur, wenn Titel und Jahr gut genug
passen und kein zweiter gleich gut ist — lieber „nicht erkannt" als der
falsche Film. Ist TMDb gerade nicht erreichbar, bleibt der Titel
„ausstehend" und kommt beim nächsten Durchgang wieder dran.

Am sichersten ist eine **Kennung im Namen** — dann sucht Lumen gar nicht.
Diese Schreibweisen (von Radarr, Sonarr, Jellyfin und Emby) versteht es,
im Dateinamen oder im Ordner des Films bzw. der Serie:

| Schreibweise | Beispiel |
|---|---|
| IMDb | `The Matrix (1999) {imdb-tt0133093}` oder `[imdbid-tt0133093]` |
| TMDb | `The Matrix (1999) {tmdb-603}` oder `[tmdbid-603]` |
| TheTVDB (Serien) | `Breaking Bad (2008) {tvdb-81189}` oder `[tvdbid-81189]` |
| IMDb frei im Namen | `Matrix.1999.tt0133093.German.1080p.mkv` |

Die Kennung gehört bei Serien an den Serienordner, nicht an die Folgen.
Auch NFO-Dateien mit Kennung gelten (`<uniqueid type="imdb|tmdb|tvdb">`,
`<imdbid>`, `<tmdbid>`, `<tvdbid>`, `<id>` oder eine IMDb-/TMDb-Adresse in
der Datei) — so, wie Emby sie mit eingeschaltetem NFO-Speicher als
`movie.nfo`, `<Dateiname>.nfo` oder `tvshow.nfo` schreibt. Vorrang hat,
was von Hand zugewiesen ist, dann die NFO, dann die Kennung im Namen, dann
die Suche.

**Nicht erkannt? Von Hand zuweisen.** Unter Verwaltung › Aufräumen stehen
alle Filme und Serien, die nicht erkannt sind oder noch warten — mit
Pfad, dem Titel und Jahr, nach denen gesucht wurde, und dem Grund (keine
Treffer, zu unsicher, zwei gleich gute, TMDb nicht erreichbar). Dazu, was
der letzte Durchlauf mit Absicht ausgelassen hat: Dateien, die wie ein
Trailer oder Sample heissen, Extras-Ordner, ISO-Abbilder und andere
Endungen, die Lumen nicht abspielt, und Unlesbares. „Suchen" sucht frei
bei TMDb, „ID eingeben" nimmt eine Kennung oder einen Link:

- IMDb: `tt0133093` oder `https://www.imdb.com/title/tt0133093/`
- TMDb: `603` (Film oder Serie, je nach Titel),
  `https://www.themoviedb.org/movie/603-the-matrix`,
  `https://www.themoviedb.org/tv/1399-game-of-thrones?language=de`
- TheTVDB: `tvdb:121361`, `https://thetvdb.com/?tab=series&id=121361`,
  `https://thetvdb.com/dereferrer/series/121361`. Ein Link mit Namen
  statt Nummer (`thetvdb.com/series/game-of-thrones`) genügt nicht — die
  Nummer steht auf der Seite der Serie unter „TheTVDB.com Series ID".

IMDb- und TheTVDB-Kennungen löst Lumen über TMDb auf; einen eigenen
TheTVDB-Schlüssel braucht es nicht. Vor dem Zuweisen zeigt Lumen den
Eintrag mit Titel, Jahr und Poster; nach „Nehmen" gilt der Titel als von
Hand zugewiesen, kein Durchlauf ändert das mehr, Metadaten, Bilder und bei
Serien alle Folgen kommen neu. Dasselbe geht auf jeder Titelseite unter
Verwaltung › „Anderen Titel zuweisen".

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
**Vorspann** überspringt ein Knopf — aus den Kapiteln der Datei, weil
jemand ihn markiert hat, oder weil Lumen ihn selbst erkannt hat (siehe
[Vorspann und Abspann erkennen](#vorspann-und-abspann-erkennen)) —, und im
Abspann steht schon da, was als Nächstes kommt. Hat die Datei **Kapitel**,
stehen sie als Marken auf der Zeitleiste und im Menü „Kapitel" zum Springen.

**Suchen** geht nach Titel, Schauspieler, Genre und Jahr, auch zusammen:
„Filme mit Tom Hanks aus den 90ern", „Komödie 2019". Filmreihen, eigene
Sammlungen und Folgen (nach ihrem Titel) findet sie mit.

Im Browser geht es ohne Fremdbibliothek: Lumen fragt, was der Browser kann,
und schickt genau das. Auch einem Browser ohne H.264 — manche Linux-Ausgaben
lassen die lizenzpflichtigen Decoder weg — liefert es ein Bild, nämlich VP9.

**Für jede Person.** Eigene Merkliste, eigene Bewertungen, eigener Stand im
Film. Merkliste, **Gesehen** und eigene Listen liegen unter einem Reiter
(Merkliste | Gesehen | Listen); „Gesehen" zeigt, was du fertig geschaut
hast — nur dir. Einladen geht mit einem Link, der sieben Tage gilt und nur
einmal. Kinderprofile sehen nur, was altersfrei ist und ausdrücklich
geprüft wurde.

**Wünsche mit Titelvorschlägen.** Fehlt ein Film oder eine Serie, wünscht
man ihn sich unter „Wünsche" oder direkt aus der Suche („„X" wünschen").
Beim Tippen erscheinen darunter die passenden Einträge von TMDb — mit
Poster, Jahr und ob Film oder Serie, mit Pfeiltasten und Enter wählbar.
Gewählt wird ein Treffer (dann kennt der Wunsch die TMDb- und die
IMDb-Kennung), oder man wünscht mit Enter bzw. „Nur den Namen wünschen"
nur den Namen. Steht ein Titel schon in einer Bibliothek, die man sehen
darf, heisst es „Schon da — ansehen"; hat ihn jemand schon gewünscht,
„Schon gewünscht" — wer, steht nicht da, und ein zweites Wünschen
schliesst sich dem ersten an, statt einen doppelten Wunsch anzulegen.
Kinderprofile sehen nur Vorschläge, deren Freigabe bei TMDb zu ihrer
Altersgrenze passt (ohne bekannte Freigabe keine). Kommt der Titel später
in die Bibliothek (gleiche TMDb- oder IMDb-Kennung, sonst gleicher Titel
und Jahr), ist der Wunsch von selbst erfüllt: mit „Ansehen" in der
Wunschliste und als Neuigkeit für alle, die ihn sich gewünscht haben —
aber nur für die, die den Titel auch sehen dürfen. Liegt er in einer
Bibliothek, die ein Kind nicht sieht, bleibt sein Wunsch offen. Wer sich
angeschlossen hat, sieht die Notiz der Verwaltung an die erste Person
nicht; wird deren Konto gelöscht, geht der Wunsch an die Person, die sich
zuerst angeschlossen hat.
Die Verwaltung sieht zu jedem Wunsch Poster, Jahr, Art und Verweise auf
IMDb und TMDb. Lumen fragt TMDb sparsam (Zwischenspeicher, Bremse je
Profil), die Poster laufen über den eigenen Server; der Browser spricht
nie selbst mit TMDb. Ohne TMDb-Schlüssel gibt es keine Vorschläge,
gewünscht wird dann mit dem Namen. Die Apps wünschen weiterhin mit dem
Namen.

**Verwaltung.** Bibliotheken, laufende Wiedergaben (nur die Last, nicht
wer was schaut), Protokoll, Aufräumen (Filme in Teilen, fehlende Dateien,
nicht erkannte Titel, ausgelassene Dateien),
Rechte pro Person oder Gruppe, Updates, tägliche Sicherung. Rechte in der
Verwaltung gibt es in drei Stufen: *keine* (nur schauen), *verwaltet mit*
(Bibliotheken, Konten ohne Verwaltung, Wünsche, Protokoll) und
*Vollzugriff* (alles wie der Besitzer, auch Gruppen, Rechte, Updates und
Sicherungen). Wer was schaut, sieht auch mit Vollzugriff niemand.

Die Verwaltung gibt es **nur am Computer** (Browser oder Mac-App), mit
Absicht: Auf Handy und Tablet bleibt Lumen zum Schauen, und ein Tipp
daneben ändert dort keine Rechte. Wer `#/verwaltung` auf dem Handy öffnet,
bekommt einen Hinweis statt der Seiten. Einladungen lassen sich unter
*Verwaltung › Konten* auch wieder zurückziehen, solange sie niemand
eingelöst hat; der Code für den Fernseher hat 16 Zeichen in Vierergruppen
(Bindestriche und Kleinbuchstaben sind egal, ebenso 0/O und 1/I).

**Bedienung mit Tastatur und Fernbedienung.** Pfeiltasten bewegen den
Fokus zwischen Karten und Knöpfen (auch mit dem Steuerkreuz eines
Fernseher-Browsers), Enter öffnet, Backspace oder die Zurück-Taste führt
eine Seite zurück, F6 springt zwischen Inhalt und Navigation, `/` in die
Suche. Im Player: Leertaste/K, J/L oder ←/→, ↑/↓, F (Vollbild), P (Bild im
Bild), M, N, S, C, Esc. Die Schrift folgt der Textgrösse von Browser und
System; unter *Ich › Darstellung* gibt es zusätzlich „Gross" und „Sehr
gross".

**Apps** für iPhone, iPad, Mac, Apple TV, Android, Android TV und Fire TV
— siehe [unten](#die-apps-bauen-ohne-mac-und-ohne-android-studio).

---

**Schauen.** Weiterschauen wie bei Netflix, auch mit der nächsten Folge
nach einer fertigen; Gesehen für Folge, Staffel oder ganze Serie;
Vorschaubilder beim Spulen; Untertitel suchen und herunterladen
(OpenSubtitles, Schlüssel in der Verwaltung); Downloads für unterwegs, wenn
die Verwaltung sie erlaubt.

**Eigene Sammlungen** („Weihnachtsfilme“, „Marvel in Reihenfolge“) neben
den automatisch erkannten Filmreihen.

**Umzug aus Emby oder Jellyfin.** Jedes Profil holt unter Ich › Umzug seine
eigenen Gesehen-Haken, Fortschritte und Favoriten herüber. Das Passwort
wird nicht gespeichert, und die Verwaltung sieht davon nichts.

Wer die Mediathek verwaltet, kann am Schreibtisch dabei auch **die
Zuordnungen übernehmen** („Zuordnungen (IMDb/TMDb/TVDb) übernehmen" beim
Anmelden; auch über den Knopf unter Aufräumen erreichbar): Was in Emby
mit „Identifizieren" richtiggestellt wurde, gilt dann auch in Lumen — für
alle Profile. Lumen findet die Titel über die Dateien, nicht über den
Namen: Emby und Lumen sehen dieselbe Platte meist unter verschiedenen
Pfaden (`/media/Filme/…` im Emby-Container, `/volume1/video/Filme/…` auf
dem NAS), darum zählt das gemeinsame Ende des Pfads (Dateiname und bis zu
zwei Ordner darüber, bei Serien der Serienordner); ist das mehrdeutig,
entscheidet die Dateigrösse, sonst bleibt der Titel unverändert. Die
Vorschau zeigt, wie viele Zuordnungen neu sind, wie viele schon stimmen,
wie viele in Lumen von Hand zugewiesen sind (die bleiben, ausser du wählst
„überschreiben") und welche Emby-Titel Lumen nicht gefunden hat.
Übernommene Titel gelten als von Hand zugewiesen; Bilder und Folgen
kommen danach im Hintergrund. Ins Protokoll kommt nur die Anzahl.

**Kinderprofile.** Beim Einladen wählbar, mit Altersgrenze; Gruppen und
Rechte können Grenzen nie lockerer machen. Wer verwaltet, braucht eine PIN
und hat selbst keine Grenzen. Unter Verwaltung › Konten lässt sich bei
jedem Konto ohne Verwaltung nachträglich ändern, was es darf
(„Bearbeiten": Kinderprofil, Altersgrenze, Bibliotheken, gleichzeitige
Wiedergaben, Herunterladen) — und ein Konto lässt sich dort auch wieder
entfernen. Serien bekommen ihre Altersfreigabe von TMDb wie Filme.

**Bildschirmzeit für Kinderprofile.** Unter Verwaltung › Konten ›
„Bildschirmzeit" bekommt ein Kinderprofil je ein Zeitfenster und eine
Tagesgrenze für Schultage (Montag bis Freitag) und das Wochenende — etwa
„Schultage 07:00–19:30, höchstens 90 Minuten; Wochenende 08:00–20:30,
120 Minuten". Ein Fenster darf über Mitternacht gehen (18:00–02:00); die
Zeit nach Mitternacht zählt dann noch zum Vortag, mit dessen Tagesgrenze —
um Mitternacht gibt es kein zweites Budget. Die Verwaltung sieht ungefähr,
wie viel heute genutzt ist, in Viertelstunden abgerundet („heute genutzt:
ungefähr 45 von 90 Min."), und das als älteren Stand: Die Zahl erneuert
sich erst eine Weile, nachdem das Kind aufgehört hat (oder spätestens nach
einer Stunde). Wer die Liste immer wieder lädt, soll nicht ablesen können,
wann ein Kind zu schauen beginnt oder aufhört. Mit „heute einmalig mehr"
(15, 30 oder 60 Min., mit „Rückgängig") wächst die Tagesgrenze und das
Zeitfenster — ist es schon zu, ab jetzt. Die Verwaltung gibt es nur am
Computer, nicht auf Handy und Tablet.

- Durchgesetzt wird auf dem Server, nicht in den Apps: Ausserhalb der Zeit
  beginnt keine Wiedergabe und kein Download (HTTP 403, Code
  `bildschirmzeit`, mit einem Satz wie „Für heute ist Schluss — morgen ab
  07:00 geht es weiter."). Läuft gerade etwas, endet es nach ein bis zwei
  Minuten Nachfrist; Browser, iPhone, Apple TV und Android zeigen dann die Meldung.
- Gezählt wird nur Video, solange es wirklich läuft, auf die Sekunde: von
  Meldung zu Meldung der Geräte (eine Pause zählt nicht, nach dem Anhalten
  läuft nichts nach). Zwei Geräte gleichzeitig zählen einmal —
  Bildschirmzeit ist Uhrzeit, nicht die Summe der Geräte.
- Viel schneller als in Echtzeit gibt es für ein Kinderprofil mit
  Bildschirmzeit keinen Film: Direktstrom, Browser-Strom und HLS-Segmente
  kommen höchstens doppelt so schnell, wie sie im Mittel laufen (Anlauf:
  ein Viertel der Laufzeit, 20 bis 60 Sekunden) — genug Luft für Szenen
  mit hoher Bitrate. Holt ein Programm trotzdem schneller, als die Uhr
  läuft, zählt die ausgelieferte Filmzeit statt der Uhrzeit.
- Es gilt die Uhr des Servers (Zeitzone aus `TZ`); um Mitternacht beginnt
  ein neuer Tag, ausser mitten in einem Fenster über Mitternacht. Die Uhr
  eines Geräts zählt nie. Lässt sich die Regel nicht lesen, bleibt das
  Profil gesperrt, bis die Verwaltung sie neu speichert.
- Das Kind sieht unter „Ich", wie viel heute noch geht, und beim Schauen
  einen leisen Hinweis bei zehn und bei zwei Minuten.
- Gespeichert wird je Profil nur die Zahl für heute — kein Titel, keine
  Uhrzeit, kein Verlauf. Was in diesen Minuten lief, sieht niemand.
- Was von einem Download läuft, sieht der Server nicht. Darum gibt es mit
  einer Tagesgrenze keine Downloads; mit einem blossen Zeitfenster nur im
  Fenster. Downloads sind für Kinderprofile ohnehin ab Werk aus.

### Vorspann und Abspann erkennen

Folgen einer Staffel teilen sich die Titelmusik. Lumen hört sich darum im
Hintergrund jede Folge an und sucht, was sie mit ihren Nachbarfolgen
gemeinsam hat — so findet es Vorspann und Abspann auch ohne Kapitel in der
Datei, und auch dann, wenn der Vorspann erst nach einer Szene („Cold Open")
kommt.

So geht es:

- Von jeder Folge liest FFmpeg den Ton der ersten zehn Minuten und der
  letzten fünf (bei kurzen Folgen das erste Drittel und das letzte
  Viertel), in Mono und mit 5512 Hz. Video wird gar nicht erst decodiert.
  Daraus rechnet Lumen einen kleinen Fingerabdruck — 32 Bit je
  Zehntelsekunde — und vergisst den Ton gleich wieder. Chromaprint braucht es
  dafür nicht (das FFmpeg im Container hat es nicht).
- Jede Folge wird mit bis zu vier Nachbarfolgen derselben Staffel
  verglichen (hat die Staffel zu wenige, mit den nächsten Folgen der
  Serie). Ein gemeinsamer Abschnitt von 15 bis 120 Sekunden am Anfang ist
  ein Vorspann-Kandidat, einer von mindestens 10 Sekunden, der bis kurz vor
  das Ende reicht, ein Abspann-Kandidat.
- Es gewinnt, was die meisten Nachbarn an derselben Stelle wiederfinden.
  Eine Rückblende („Was bisher geschah") teilt eine Folge nur mit der
  einen, aus der sie stammt — das reicht nie. Benutzt wird ein Ergebnis
  nur ab einer Mindestsicherheit; lieber gar kein „Vorspann überspringen"
  als eines, das in die Handlung springt.
- Was gilt, in dieser Reihenfolge: Vorspann von Hand an der Folge
  markiert, Kapitel der Datei, von Hand an der Serie markiert, erkannt.
  Beim Abspann: von Hand an der Folge, Kapitel, dann erkannt. Liegt die
  Erkennung bei einer Folge daneben, markiert man den Vorspann an der Folge
  (`PUT /api/v1/admin/items/{folge}/intro`) oder an der Serie. Wer die
  Verwaltung darf, findet im Player der Web-App unter den Spuren auch
  „Kein Vorspann bei dieser Folge/Serie", „Abspann beginnt hier" und „Kein
  Abspann bei dieser Folge/Serie" (`{"keiner": true}` an `…/intro` oder
  `…/abspann`): „keiner" an der Serie schaltet auch Kapitel und Erkennung
  ab. Die Apps bekommen dann einfach keinen Vorspann oder Abspann.
- Hat eine Folge mehrere Fassungen, gelten Kapitel und Erkanntes der
  Datei, die läuft (`jeDatei` in der Antwort von `/items/{id}`); oben in
  der Antwort stehen wie bisher die Zeiten der ersten Datei.

Die Arbeit läuft wie Vorschaubilder und Untertitel: nacheinander, mit
niedrigster Rechen- und Plattenpriorität, und jede Datei genau einmal —
das Ergebnis steht in der Datenbank und gilt, bis sich die Datei ändert.
Sie wartet, solange irgendetwas abgespielt wird (auch direkt oder im
Browser), und läuft ab Werk nur nachts zwischen 1 und 7 Uhr (Uhr des
Servers). Je Folge sind es auf einem schwachen NAS einige Sekunden bis
wenige Dutzend, vor allem fürs Lesen von der Platte. Ein- und ausschalten,
„Nur nachts (1–7 Uhr)" abwählen und sehen, wie weit sie ist, kann man in
der Verwaltung unter „Umwandeln".

Fehlt eine Datei gerade (Netzlaufwerk nicht eingehängt), überspringt die
Erkennung sie, ohne etwas zu speichern. Lässt sich eine Datei nicht lesen
oder dauert es zu lange, versucht sie es später noch einmal — nach einer
Stunde, nach sechs, nach einem Tag, danach wöchentlich. Im Protokoll steht
je Durchgang höchstens eine Zeile mit den Zahlen, ohne Dateinamen.

Grenzen:

- Es braucht mindestens zwei Folgen derselben Serie. Eine einzelne Folge
  wird untersucht, sobald eine zweite dazukommt.
- Serien ohne gemeinsame Titelmusik (jede Folge mit eigenem Vorspann,
  Abspann über wechselnden Liedern) erkennt Lumen nicht — dann gibt es
  eben keinen Knopf, und „Als Nächstes" kommt wie bisher kurz vor Schluss.
- Gesucht wird nur in den ersten zehn Minuten; ein noch späterer Vorspann
  wird nicht gefunden.
- Ist der Vorspann nur teilweise gleich (Szenen mit Dialog über der Musik,
  verschieden lange Fassungen), findet Lumen eher den gemeinsamen Kern:
  Der Sprung landet dann ein wenig zu früh, nicht mitten in der Handlung.
- Filme werden nicht untersucht; dort gibt es keine nächste Folge, und ein
  falsch erkannter Abspann wäre ärgerlicher als keiner.
- Eine Folge in mehreren Dateien (Teil 1/2) bekommt einen Vorspann, aber
  keinen erkannten Abspann.

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
| `LUMEN_ADDR` | `--addr` | `:8097` | Adresse und Port. Mit `network_mode: host` (Vorlage) der Port auf dem NAS; im Bridge-Betrieb `:8097` lassen und nur den Port nach aussen ändern |
| `LUMEN_DATA` | `--data` | `/data` im Container, sonst der Konfigurationsordner des Systems | Datenbank, Poster, Zwischenspeicher, Sicherungen |
| `LUMEN_UMWANDLUNG_DIR` | — | der Datenordner | Wohin die Zwischendateien beim Umwandeln kommen. Auf einem schwachen NAS gern in den Arbeitsspeicher: `tmpfs` (in der Compose-Datei vorbereitet) und `LUMEN_UMWANDLUNG_DIR=/umwandlung` |
| `LUMEN_MAX_UMWANDLUNGEN` | — | nach Rechner: ohne Hardware `1` bei höchstens zwei Kernen, `2` bis sieben Kerne, darüber ein Viertel der Kerne; mit Hardware-Beschleunigung `4` (auf zwei Kernen `2`) | Wie viele Filme gleichzeitig umgewandelt werden dürfen. Auf einem ARM-NAS ohne Hardware eher `1` |
| `LUMEN_HARDWARE` | — | `auto` | Hardware-Beschleunigung: `auto`, `aus`, `vaapi`, `qsv` oder `nvenc`. Hat Vorrang vor der Einstellung in der Verwaltung (Umwandeln) |
| `LUMEN_VAAPI_GERAET` | — | `/dev/dri/renderD128` | Welches Grafikgerät VAAPI nutzt, falls es mehrere gibt |
| `LUMEN_FFMPEG`, `LUMEN_FFPROBE` | `--ffmpeg`, `--ffprobe` | `ffmpeg`, `ffprobe` | Pfad zu FFmpeg (im Abbild schon dabei) |
| `LUMEN_WEB` | `--web` | eingebaut | Web-App aus einem Ordner statt der eingebauten (zum Entwickeln) |
| `LUMEN_TLS` | `--tls` | aus | `1` (oder `ja`, `true`) schaltet den verschlüsselten Eingang (HTTPS) ein, `0`/`nein` lässt ihn aus. Mit `LUMEN_RELAY` geht er von selbst an |
| `LUMEN_TLS_ADDR` | `--tls-addr` | `:8920` | Adresse des verschlüsselten Eingangs. Mit `network_mode: host` liegt er direkt auf dem NAS — läuft dort Emby oder Jellyfin mit HTTPS, ist 8920 belegt, dann etwa `:8921`. Im Bridge-Betrieb den Port veröffentlichen, wenn Geräte im Heimnetz ihn direkt nutzen sollen |
| `LUMEN_TLS_NAME` | `--tls-name` | — | Name oder Adresse des NAS fürs eigene Zertifikat (im Container sieht Lumen nur seine Container-Adresse). Gilt auch als bekannter Name |
| `LUMEN_TLS_CERT`, `LUMEN_TLS_KEY` | `--tls-cert`, `--tls-key` | eigenes Zertifikat | Ein mitgebrachtes Zertifikat, etwa von Let's Encrypt; wird nach Erneuerung von selbst neu gelesen |
| `LUMEN_HOSTS` | — | — | Weitere Namen, unter denen Lumen erreichbar ist (Reverse-Proxy, DDNS wie `name.synology.me`), mit Komma getrennt. Sonst lehnt Lumen Anmeldung und Einrichtung unter diesem Namen ab (Schutz vor DNS-Rebinding) |
| `LUMEN_HEIMNETZ` | — | — | Welche Netze als „zu Hause“ gelten, etwa `192.168.1.0/24,fd00::/8`. Ohne Angabe gelten private Adressen — ausser den Gateways der eigenen Netze, dem NAS selbst und anderen Containern, damit Anfragen von aussen über Docker oder einen Proxy nicht als „zu Hause“ durchgehen. Gelten Geräte im Heimnetz als „unterwegs“, zuerst in der Verwaltung unter „Dieses Gerät“ nachsehen (meist hilft `network_mode: host`, siehe [hier](#zu-hause-oder-unterwegs-docker-und-die-adressen)) |
| `LUMEN_OPENSUBTITLES_URL` | — | OpenSubtitles | Eigener Spiegel für den Untertitel-Download (selten nötig) |
| `LUMEN_RELAY` | `--relay` | aus | Adresse des Vermittlers für den Fernzugriff, etwa `meinserver.example:8444` |
| `LUMEN_RELAY_SECRET` | `--relay-secret` | — | Gemeinsames Geheimnis mit dem Vermittler |
| `LUMEN_RELAY_FINGERPRINT` | `--relay-fingerprint` | — | Fingerabdruck des Vermittler-Zertifikats (gibt der Vermittler beim Start aus) |
| `LUMEN_GITHUB_TOKEN` | — | — | *Optional.* Die Update-Prüfung braucht kein Token; nur für Updates aus einem privaten Repository (siehe „Aktuell bleiben"). Ein Token aus der Verwaltung hat Vorrang |
| `LUMEN_AKTUALISIERER_URL` | — | — | Adresse des Aktualisierers für Updates per Knopfdruck im Container, etwa `http://127.0.0.1:8080` mit `network_mode: host` oder `http://aktualisierer:8080` im Bridge-Betrieb (siehe [Updates per Knopfdruck](#updates-per-knopfdruck)). Nur hier einstellbar, nicht in der Verwaltung |
| `LUMEN_AKTUALISIERER_TOKEN` | — | — | Geheimes Token für den Aktualisierer, dasselbe wie dort `WATCHTOWER_HTTP_API_TOKEN` (am besten über die Datei `.env`). Wird nie angezeigt, nie protokolliert und nie an Geräte geschickt |
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
`sicherungen/`, `lumen.db` und — wenn du HTTPS oder den Fernzugriff nutzt —
`eigenes-zertifikat.pem` und `eigener-schluessel.pem`. Gehen die beiden
verloren, ändert sich der Fingerabdruck, und jede App muss den Server neu
kennenlernen.

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

**Musik und Fotos aus älteren Fassungen.** Frühere Fassungen kannten
auch Bibliotheken für Musik und Fotos. Beim ersten Start einer neueren
Fassung entfernt Lumen solche Bibliotheken samt Hörstand, Musik-Favoriten,
Musiklisten und den zwischengespeicherten Covern und Vorschaubildern
(`daten/mediathek`) — einmal, und nur, was Lumen dort selbst angelegt
hat. Die Dateien auf der Platte bleiben, wo sie sind;
Filme und Serien sind nicht betroffen. Vorher legt Lumen wie bei jedem
Umbau `lumen.db.vor-umbau` an.

**Zurück auf eine ältere Fassung** nach einem Update: Vor jedem Umbau der
Datenbank legt Lumen `daten/lumen.db.vor-umbau` an. Eine ältere Fassung
startet mit der umgebauten Datenbank absichtlich nicht (sie kennt den neuen
Aufbau nicht). Zurück geht es so: `lumen.db.vor-umbau` nach
`lumen.db.wiederherstellen` kopieren und die ältere Fassung starten — was
seit dem Update dazukam (Fortschritt, Merkliste …), fehlt dann. Im
Container dabei in der Projektdatei statt `:latest` die alte Fassung
eintragen (etwa `ghcr.io/ada462790-beep/lumen-media:v0.1.24`), sonst holt
`pull_policy: always` gleich wieder die neueste. Den Stand vor dem
vorletzten Update hebt Lumen als `lumen.db.vor-umbau.alt` auf. Scheitert ein
Umbau mittendrin, steht daneben `lumen.db.umbau-offen`; beim nächsten Start
macht Lumen beim erreichten Schritt weiter und lässt die Sicherung von vor
dem Update unangetastet. Die Marke verschwindet, sobald der Umbau durch ist.

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

### PIN vergessen

Hat ein Profil seinen PIN vergessen, setzt die Verwaltung ihn in der App
zurück (*Verwaltung › Konten*). Lumen zeigt dann einmal einen Einmal-Code
mit sechs Ziffern. Mit ihm meldet sich die Person an und legt dabei ihre
eigene PIN fest (zweimal, gegen Tippfehler) — die Verwaltung kennt sie
danach nicht. Erst dann sagt Lumen der Person, dass ihre PIN zurückgesetzt
und ihr Verlauf gelöscht wurde. Der Einmal-Code gilt 72 Stunden; danach
vergibt die Verwaltung einen neuen. App-Versionen vor dieser Version können
das Festlegen nicht abschliessen — dann in der Web-App anmelden oder die
App zuerst aktualisieren.
Hat der Besitzer seinen eigenen PIN oder
den PIN der Verwaltungssperre vergessen, geht das **nur auf dem Server
selbst** — mit Absicht, sonst könnte es jeder im Heimnetz. Auf einem
Synology geht es auch **ohne SSH**, über den Aufgabenplaner (wie beim
Herausfinden von PUID/PGID oben):

1. *Systemsteuerung › Aufgabenplaner › Erstellen › Geplante Aufgabe ›
   Benutzerdefiniertes Skript*, Benutzer **root**, Zeitplan egal.
2. Unter *Aufgabeneinstellungen* dieses Skript (Name und neuen PIN
   anpassen; die Zahl steht zweimal, weil Lumen sie zur Kontrolle noch
   einmal verlangt):

   ```sh
   printf '2580\n2580\n' | /usr/local/bin/docker exec -i lumen lumen zuruecksetzen --konto Alex --verwaltung > /volume1/docker/lumen/zuruecksetzen.txt 2>&1
   ```

3. Die Aufgabe einmal *Ausführen*, in der File Station
   `docker/lumen/zuruecksetzen.txt` öffnen (dort steht „Erledigt: …"),
   dann **Aufgabe und Datei löschen** — im Skript steht der neue PIN.
   Danach in Lumen gleich einen eigenen PIN wählen.

Ohne `--verwaltung` bleibt die Verwaltungssperre, wie sie ist. Mit SSH
geht dasselbe im Terminal:

```sh
sudo docker exec -it lumen lumen zuruecksetzen                 # Profile auflisten
sudo docker exec -it lumen lumen zuruecksetzen --konto Alex    # neuen PIN setzen
```

`--art password` setzt ein Passwort statt eines PINs, `--verwaltung` hebt
zusätzlich den Verwaltungs-PIN auf. Ohne Docker:
`lumen zuruecksetzen --data /pfad/zu/lumen-daten --konto Alex`.

Beim Besitzer bleibt alles erhalten; alle seine Geräte werden abgemeldet.
Setzt man so das Profil von jemand anderem zurück, werden dessen Verlauf,
Merkliste und Bewertungen gelöscht — genau wie in der App —, und der
gesetzte PIN gilt nur als Einmal-Code (72 Stunden). Mit `--art password`
legt die Person danach ein eigenes Passwort statt einer PIN fest.

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
selbst ein (auch den Eingang auf 8920). Für den Vermittler braucht es
diesen Port nicht — der Vermittler bekommt einen eigenen Eingang, der nur
auf `127.0.0.1` erreichbar ist und jede Anfrage als „unterwegs" behandelt,
auch mit `network_mode: host`.

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
fe80::) und ohne Proxy-Kopfzeilen. Docker reicht die echte Adresse im
Bridge-Betrieb aber nicht immer durch: Bei IPv6-Zugriffen, bei manchen
NAS (auch Synology) und bei einem Proxy auf demselben Gerät sieht Lumen
statt des Absenders das Gateway des Docker-Netzes (etwa `172.17.0.1`).
Das zählt nie als zu Hause — sonst gälte eine Anfrage aus dem Internet als
„zu Hause": mit Profilauswahl, ohne PIN-Pflicht, mit Einrichten. Genauso
wenig zählen der NAS selbst (seine Adressen und, mit `network_mode: host`,
`127.0.0.1`), andere Container und der Router. Die Kehrseite: Sieht Lumen
nur das Gateway, gilt auch das Sofa als unterwegs — die Lösung ist
`network_mode: host` (siehe [Docker und die Adressen](#zu-hause-oder-unterwegs-docker-und-die-adressen)).
Eine öffentliche IPv6-Adresse gilt ebenfalls nie als zu Hause; im
Heimnetz Lumen darum über die IPv4-Adresse des NAS öffnen. Und: Lumens
Ports nie im Router freigeben, einen Reverse-Proxy nur mit
`X-Forwarded-For` betreiben.
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
| Der Besitzer sieht nur die Last — nicht Titel, Person, Gerät, Ort oder Startzeit | Was er zu sehen bekommt, ist eine eigene Struktur mit zwei Feldern (Einmalkennung, direkt oder umgewandelt); ein Test lässt genau diese zu und keines mehr |
| Auch im Betrieb nicht | Ein Test spielt wirklich einen Film ab und durchsucht die Antwort an den Besitzer nach Titel, Pfad, Kennung, Person und Gerät |
| Keine Spuren auf der Platte | Ein Test spielt einen Film samt Untertiteln ab und prüft jede neu entstandene Datei: kein Name darf auf den Film zeigen |
| Keine Uhrzeiten, die mit Schauen zusammenhängen | Der Fortschritt trägt eine laufende Nummer statt einer Uhrzeit; Geräte merken sich nur den Tag |
| Das Protokoll ist kein Verlauf | Beim Abspielen steht dort nur „Umwandlung gestartet" und der technische Grund — kein Gerät, keine Person, keine Datei |
| Bewertungen nur mit Freigabe | Nur mit „Bewertungen im Haushalt zeigen" taucht etwas im Haushalt auf; ausschalten nimmt auch die bisherigen zurück; Gäste (von unterwegs eingeladen) sehen nichts davon |
| Zurücksetzen heisst vergessen | Nach „PIN zurücksetzen" ist der private Stand auch aus der WAL-Datei und den beiden Sicherungen gelöscht; die Verwaltung vergibt nur einen Einmal-Code, die neue PIN legt die Person selbst fest |
| Bildschirmzeit ist kein Verlauf | Je Kinderprofil steht nur eine Zahl für heute in der Datenbank; ein Test verlangt, dass jede Abfrage darauf genau ein Konto nennt, und gestern wird beim Zählen gelöscht; die Verwaltung sieht die Minuten nur in Viertelstunden und erst eine Weile, nachdem das Kind aufgehört hat; ein Test zählt während des Schauens weiter und verlangt, dass die Zahl der Verwaltung dabei stehen bleibt |

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

### Lizenzen und Drittsoftware

Was nicht von den Lumen-Autoren stammt, steht mit Fassung, Lizenz und
Fundstelle in [DRITTSOFTWARE.md](DRITTSOFTWARE.md) (englisch:
[THIRD_PARTY.md](THIRD_PARTY.md)). Kurz:

- In die Binärdatei eingebaut sind nur `go-sqlite3` (MIT, mit SQLite,
  gemeinfrei), `go-qrcode` (MIT, für den QR-Code der Authenticator-App)
  und die Go-Standardbibliothek (BSD-3-Clause). Ihre Lizenztexte liegen dem
  Release-Paket und dem Docker-Abbild bei.
- FFmpeg wird nur aufgerufen, nicht eingebaut, und bleibt unter seiner
  eigenen Lizenz (im Abbild: das Alpine-Paket, GPL). Welche Alpine-Pakete in
  einem Abbild stecken und wo ihr Quelltext liegt, steht darin unter
  `/usr/share/licenses/lumen/`.
- Die Android-App enthält Googles Cast-Bibliothek (nicht quelloffen). Das
  erlaubt eine zusätzliche Erlaubnis nach AGPL §7 in
  [LICENSE-EXCEPTIONS](LICENSE-EXCEPTIONS); dort steht auch die Erlaubnis
  für App-Stores.
- Der Aktualisierer (Watchtower, Apache-2.0) ist **nicht Teil von Lumen**:
  Wer ihn einschaltet, lädt ihn selbst vom Docker Hub seines Herausgebers.
- Poster und Beschreibungen kommen von TMDB, mit deinem eigenen Schlüssel.
  This product uses the TMDB API but is not endorsed or certified by TMDB.
  Wertungen kommen über OMDb (CC BY-NC 4.0), Biografien teils aus der
  Wikipedia (CC BY-SA 4.0). In der App steht das unter **Einstellungen ›
  Datenquellen und Drittsoftware**.

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
