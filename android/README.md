# Lumen für Android, Android TV und Fire TV

Ein Projekt, drei Geräteklassen. Die Logik steckt in `lumen-core` und ist
überall gleich; nur die Oberfläche unterscheidet sich — zu Recht: auf dem
Fernseher gibt es keinen Zeiger, sondern einen Fokus, der springt.

```
lumen-core/   Verbindung, Modelle, Anmeldung — reines Kotlin, kein Android
lumen-texte/  Übersetzungen der Sätze aus lumen-core, Wahl der Sprache
app/          Handy und Tablet (Compose)
tv/           Android TV und Fire TV (Compose für TV)
```

## Öffnen

In Android Studio den Ordner `android` öffnen. Gradle holt beim ersten Mal
alles Nötige.

## Der Teil, der wirklich geprüft ist

`lumen-core` hat **keine einzige Abhängigkeit**: eigener JSON-Leser, HTTP
über das JDK. Darum lässt es sich ohne Android, ohne Gerät und ohne Gradle
übersetzen und testen — und genau das ist passiert:

```bash
kotlinc src/main/kotlin src/test/kotlin -include-runtime -d tests.jar
java -jar tests.jar 192.168.1.20:8097
```

Die Tests laufen gegen einen **echten Server**: anmelden, falscher PIN,
Startseite, Titelseite, Abspielplan, Fortschritt speichern und nachprüfen,
Merkliste, Haken, Bewertung bleibt privat, Wunsch, Verwaltung, abmelden.
122 Behauptungen.

Dabei fiel ein Fehler auf, der sonst erst beim Vertippen aufgefallen wäre:
ein falscher PIN wurde als „Die Anmeldung gilt nicht mehr" gemeldet statt
als „das stimmt nicht". Derselbe Fehler steckte im Apple-Code.

Die Oberfläche (`app/`, `tv/`) braucht das Android-SDK und ist **nicht**
übersetzt. Dafür hält `tools/schnittstelle_pruefen.py` vom Projektstamm aus
beide Apps gegen den Server.

## Wie abgespielt wird

ExoPlayer versteht HLS von Haus aus. Weil der Server die vollständige
Abspielliste vorab ausliefert, funktioniert damit auch das Springen in einen
Film, der gerade erst umgewandelt wird.

Welche Formate das Gerät wirklich kann, wird beim Start aus dem Codec-
Katalog gelesen, nicht geraten — ein billiges Fernsehgerät ohne HEVC bekäme
sonst 4K-Material und zeigte nichts.

## Zwei Dinge zum Wissen

**Unverschlüsseltes HTTP ist erlaubt.** Android verbietet es seit Fassung 9,
und zwar wortlos. Lumen steht im eigenen Netz, meist unter einer IP-Adresse
und ohne Zertifikat — ohne diese Ausnahme käme keine Verbindung zustande.
Die naheliegende Lösung, nur private Adressbereiche freizugeben, geht nicht:
`<domain>` vergleicht Rechnernamen, keine Adressbereiche. Wer Lumen von
aussen erreichbar macht, sollte HTTPS davorsetzen.

**Die Symbole sind Platzhalter.** `ic_launcher` und das TV-Kästchen sind
schlichte Formen. Vor einer Veröffentlichung ersetzen.

---

Lizenz: AGPL-3.0-only — siehe [LICENSE](../LICENSE) im Wurzelverzeichnis.

## Sprachen

Deutsch (Schweiz, Deutschland, Österreich) und Englisch (USA, Vereinigtes
Königreich). Die Apps folgen der Sprache des Profils auf dem Server, vor der
Anmeldung der Vorgabe des Servers. Wo die Texte stehen, wie die Rückfallkette
aussieht und welche Begriffe gelten: [BEGRIFFE.md](BEGRIFFE.md). Geprüft wird
mit `python3 tools/sprache_android_pruefen.py` und mit Lint.

