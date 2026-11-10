# Was Lumen über dich weiss

Lumen läuft auf dem Rechner oder NAS von jemandem aus deinem Haushalt, dem
**Besitzer**. Diese Seite sagt genau, was Lumen über jedes Profil speichert,
was der Besitzer davon in der Verwaltung sieht und was nicht. Sie sagt auch,
was jemand mit vollem Zugriff auf das NAS trotzdem herausfinden könnte.

Die Grundregel: **Der Besitzer sieht nicht, wer wann was wie lange schaut.**
Lumen speichert keinen Verlauf, die Verwaltung zeigt es nicht, und es gibt
keine Abfrage dafür. Tests im Code schlagen fehl, sobald jemand das ändert.

---

## Was Lumen über dein Profil speichert

| Was | Wozu | Sieht der Besitzer es? |
|---|---|---|
| Name, Profilbild | damit du dein Profil findest | ja |
| PIN oder Passwort | nur als Prüfwert (PBKDF2), nicht im Klartext | nein |
| Kinderprofil ja/nein, Altersfreigabe, freigegebene Bibliotheken, Gruppe | legt der Besitzer fest | ja, er stellt es ein |
| Deine Geräte: Name, Art (Fernseher, Handy …), **Tag** der Anmeldung, **Tag** der letzten Nutzung | damit du ein verlorenes Gerät abmelden kannst | nur die Anzahl deiner Geräte |
| Wo du in einem Film stehst, ob du ihn gesehen hast | für „Weiterschauen" und die Haken auf den Postern | **nein** |
| Reihenfolge von „Weiterschauen" | eine laufende Nummer, **keine Uhrzeit**; sie zählt nur weiter, wenn du einen anderen Titel anfängst, nicht mit der Zeit, die du schaust | **nein** |
| Bewertungen (Sterne), mit **Tag** statt Uhrzeit | für dich; im Haushalt nur sichtbar, wenn du das Häkchen setzt | nur freigegebene, wie alle im Haushalt |
| Merkliste und eigene Listen | für dich | **nein** |
| Wünsche („bitte diesen Film besorgen") | damit der Besitzer sie erfüllen kann | ja, mit deinem Namen — das ist der Sinn eines Wunsches |
| Neuigkeiten („2 neue Folgen") | für dich | **nein** |
| Einstellungen (Untertitel-Stil, Sprache …) | für dich | **nein** |

Was Lumen **nicht** speichert: wann du etwas geschaut hast, wie lange, wie
oft, auf welchem Gerät du welchen Titel geschaut hast, einen Verlauf, eine
Statistik.

## Was der Besitzer in der Verwaltung sieht

- **Aktive Wiedergaben:** wie viele gerade laufen und für jede nur die Last:
  direkt abgespielt oder umgewandelt, von welcher Qualität in welche
  („4K HEVC → 1080p H.264"), Bandbreite und warum umgewandelt wird. Dazu den
  Knopf „Beenden". **Nicht:** Titel, Profil, Gerät, Ort, Startzeit. Jede
  Wiedergabe hat dort eine eigene Zufallskennung, darum lässt sich auch nicht
  erkennen, ob zwei Wiedergaben aus demselben Profil kommen.
- **Protokoll:** Serverereignisse wie „Umwandlung gestartet · Bandbreite" oder
  „eine Datei liess sich nicht ausmessen". **Nicht:** welches Gerät, welche
  Person, welche Datei. Das Protokoll wird nach 14 Tagen gelöscht.
- **Konten:** Namen, Gruppen, Freigaben, Anzahl der Geräte.

- **PIN zurücksetzen:** Hast du deine PIN vergessen, kann die Verwaltung eine
  neue setzen. Damit sie sich danach nicht als du anmelden und deinen Stand
  lesen kann, löscht Lumen dabei alles Private deines Profils: Fortschritt,
  Gesehen-Haken, Merkliste, Favoriten, Bewertungen, deine privaten
  Listen und die gemerkten Spuren je Serie. Alle deine Geräte werden
  abgemeldet. Geteilte Listen bleiben, die sieht der Haushalt ohnehin. Beim
  nächsten Anmelden sagt Lumen dir, dass das passiert ist.

Unter **Ich → Geräte** siehst nur du, was gerade in deinem Profil läuft, und
kannst es dort selbst beenden (etwa einen vergessenen Fernseher).

## Was trotzdem möglich ist — ehrlich gesagt

Lumen läuft auf Hardware, die dem Besitzer gehört. Wer dort **vollen Zugriff
(root)** hat, kann Dinge sehen, die Lumen nicht zeigt:

- **Laufende Programme und offene Dateien.** Während ein Film läuft, hat Lumen
  die Filmdatei geöffnet; das Betriebssystem kann das anzeigen. Wird
  umgewandelt, ist in der Prozessliste der Pfad der Datei zu sehen.
- **Die Datenbank selbst.** Darin stehen die Dinge aus der Tabelle oben, also
  auch, wo du in welchem Film stehst. Uhrzeiten zum Schauen stehen dort nicht.
- **Netzwerkverkehr im Heimnetz** (mit Werkzeugen am Router oder NAS).
- **Sicherungen der Datenbank.** Lumen legt vor jedem Umbau der Datenbank bei
  einem Update eine Sicherung an (`lumen.db.vor-umbau`, nur für den Besitzer
  lesbar). Sie enthält denselben Stand wie die Datenbank zu diesem Zeitpunkt.

Was Lumen dagegen tut: Zwischendateien beim Umwandeln liegen in Ordnern mit
Zufallsnamen ohne Bezug zum Film oder zur Wiedergabe und werden danach
gelöscht. Untertitel werden im Arbeitsspeicher gewandelt, nicht als Datei mit
Zeitstempel abgelegt. Untertitel, die jemand von OpenSubtitles holt, legt
Lumen nicht neben den Film, sondern im Datenordner ab, mit festem Datum für
Datei und Ordner. Vorschaubilder fürs
Spulen entstehen nur im Durchgang über die ganze Mediathek, nicht beim
Abspielen eines bestimmten Films. Standbilder und Vorschau-Clips, die beim ersten Ansehen
einer Karte entstehen, bekommen ein festes Datum statt der echten Uhrzeit.
(Das Dateisystem selbst merkt sich trotzdem, wann eine Datei angelegt
wurde — wer root hat, kann das lesen.) Fehlermeldungen verlieren Dateipfade, bevor sie ins
Protokoll kommen.

Wenn dir das nicht reicht, ist die ehrliche Antwort: Schau auf einem Server,
den du selbst betreibst.

## Fernzugriff über den Vermittler

Wer von unterwegs schaut, geht über den Vermittler (einen kleinen Server mit
öffentlicher Adresse). Lumen verschlüsselt selbst, der Schlüssel bleibt zu
Hause. Der Vermittler sieht nur verschlüsselte Bytes: keine Adressen, keine
Titel, keine Namen. Wer ihn betreibt, sieht höchstens, **dass** gerade Daten
fliessen und wie viele.

Eine Einschränkung, ehrlich gesagt: Das gilt sicher für die Apps, weil sie
den Fingerabdruck des Zertifikats deines Servers festnageln. Im **Browser**
klickt man beim selbst ausgestellten Zertifikat einmal eine Warnung weg.
Wer den Vermittler betreibt, könnte in diesem Moment ein eigenes
Zertifikat unterschieben und mitlesen. Wenn du den Vermittler nicht selbst
betreibst, nimm unterwegs die Apps — oder gib Lumen ein öffentliches
Zertifikat (`--tls-cert`), dann gibt es keine Warnung zum Wegklicken.

## Chromecast

Wer in der Android-App auf einen Chromecast schickt, benutzt dafür den
**Default Media Receiver von Google** — die Empfänger-App, die Google für
Chromecast-Geräte bereitstellt und die der Chromecast von Google lädt. Eine
eigene Empfänger-App hat Lumen nicht.

Das Telefon gibt dem Chromecast dafür:

- den **Titel** des Films oder der Folge und die Adresse des Hintergrundbilds,
- die **Adresse des Stroms** und der Untertitel. Darin steht ein
  **Medienschlüssel**, nicht dein Token: Er öffnet nur Bilder, Videos und
  Untertitel und läuft ab (6 Stunden nach der letzten Benutzung, spätestens
  nach 48 Stunden, nach einem Neustart des Servers sofort).

Den Film holt der Chromecast selbst bei deinem Server im Heimnetz; über
Google läuft er nicht. Was Google und der Chromecast mit dem Titel und den
Angaben zur Wiedergabe tun, bestimmen Google und dessen Bedingungen für
Chromecast — darauf hat Lumen keinen Einfluss. Wer das nicht möchte, schaut
ohne Chromecast: Lumen schickt nichts dorthin, solange niemand „Abspielen"
mit verbundenem Chromecast wählt.

## Woran du das prüfen kannst

Der Quelltext liegt offen. Die wichtigsten Stellen:

- `server/internal/store/privacy_test.go`: Keine Abfrage auf Fortschritt,
  Bewertungen oder Merkliste ohne festes Konto, kein Zusammenzählen über
  Profile.
- `server/internal/stream/live_test.go`: Die Ansicht des Besitzers hat genau
  fünf Felder (Kennung, direkt/umgewandelt, Grund, Bandbreite, Qualität).
- `server/internal/api/wiedergabe_test.go`: Nach dem Abspielen liegt keine
  Datei auf der Platte, deren Name auf den Film zeigt.
- `server/internal/store/umbau_test.go`: Fortschritt ohne Uhrzeit, Geräte nur
  mit dem Tag.
