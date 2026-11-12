# Drittsoftware und Datenquellen

*Stand: 12. November 2026 · [English version](THIRD_PARTY.md)*

Lumen selbst steht unter der **AGPL-3.0-only** (siehe [LICENSE](LICENSE)).
Diese Datei listet alles, was **nicht** von den Lumen-Autoren stammt: was in
Lumen eingebaut ist, was in den Docker-Abbildern und Apps mitkommt, was du
selbst getrennt beziehst, und welche Online-Dienste Lumen abfragt.

Die vollständigen Lizenztexte der Bibliotheken, die in der Lumen-Binärdatei
stecken, stehen im Anhang von [THIRD_PARTY.md](THIRD_PARTY.md#appendix-license-texts).

Spalte **„Wie"**:

- **eingebaut** – steckt in der Lumen-Binärdatei bzw. App, die das Projekt verbreitet
- **im Abbild** – liegt im Docker-Abbild `ghcr.io/ada462790-beep/lumen-media`, ist aber ein eigenes Programm
- **getrennt** – nicht Teil von Lumen; du lädst es selbst vom Herausgeber
- **Dienst** – ein Online-Dienst, den dein Server abfragt (mit deinem eigenen Schlüssel)
- **nur Bau** – wird nur zum Bauen oder Prüfen gebraucht, nicht ausgeliefert

---

## 1. Lumen-Server (Binärdatei `lumen`, `lumen-relay`)

| Name | Fassung | Lizenz (SPDX) | Quelle | Wie | Pflichten / erfüllt |
|---|---|---|---|---|---|
| Go-Standardbibliothek und -Laufzeit | 1.25 (Bau-Abbild `golang:1.25-alpine`) | BSD-3-Clause | <https://go.dev/LICENSE> | eingebaut | Lizenzhinweis in jeder Kopie: Text im Anhang von THIRD_PARTY.md, die Datei liegt im Abbild und im Release-Paket |
| github.com/mattn/go-sqlite3 | v1.14.52 | MIT | <https://github.com/mattn/go-sqlite3> | eingebaut (nur `lumen`) | wie oben; Text in `server/vendor/github.com/mattn/go-sqlite3/LICENSE` |
| SQLite (Amalgamation, in go-sqlite3 enthalten) | 3.53.4 | blessing (gemeinfrei) | <https://sqlite.org/copyright.html> | eingebaut (nur `lumen`) | keine |
| github.com/skip2/go-qrcode | v0.0.0-20200617195104-da1b6568686e | MIT | <https://github.com/skip2/go-qrcode> | eingebaut | wie oben; Text in `server/vendor/github.com/skip2/go-qrcode/LICENSE` |

Alle Lizenzen sind mit der AGPL-3.0 vereinbar (MIT, BSD-3-Clause und
gemeinfreie Werke dürfen in ein AGPL-Werk aufgenommen werden).

Auf Linux ist `lumen` dynamisch gegen die C-Bibliothek des Systems gelinkt
(glibc im Release-Paket, musl im Abbild) – das sind Systembibliotheken im
Sinne der GPL. Auf Windows kommen Laufzeitteile von MinGW-w64 dazu
(GCC Runtime Library Exception bzw. MIT/ZPL), die das erlauben.

## 2. Docker-Abbild `lumen-media`

Das Abbild beruht auf **Alpine Linux 3.22** (`alpine:3.22`). Ausser der
Lumen-Binärdatei und `docker-start.sh` stammt alles aus den Paketen von
Alpine. Lumen ruft FFmpeg als **eigenes Programm** auf und linkt es nicht;
FFmpeg bleibt unter seiner eigenen Lizenz (das ist ein „Aggregat" im Sinne
der GPL, keine Verbindung zu einem Werk).

Fassungen nach Alpine 3.22 (x86_64, Stand 12.11.2026; die genaue Liste jedes
Abbilds liegt darin unter `/usr/share/licenses/lumen/alpine-pakete.txt`):

| Paket | Fassung | Lizenz laut Alpine | Wie |
|---|---|---|---|
| ffmpeg, ffmpeg-libav* | 6.1.2-r2 | GPL-2.0-or-later AND LGPL-2.1-or-later; gebaut mit `--enable-gpl --enable-version3` (also insgesamt **GPL-3.0-or-later**), ohne `--enable-nonfree` | im Abbild |
| x264-libs | 0.164.3108-r0 | GPL-2.0-or-later | im Abbild (von ffmpeg gezogen) |
| x265-libs | 3.6-r0 | GPL-2.0-or-later | im Abbild (von ffmpeg gezogen) |
| weitere Codec-Bibliotheken von ffmpeg (dav1d, aom, opus, vorbis, vpx, lame, libass …) | je nach Alpine | BSD, LGPL, MIT u. a. | im Abbild |
| libssl3 / libcrypto3 (OpenSSL) | 3.5.x | Apache-2.0 | im Abbild |
| busybox | 1.37.0-r20 | GPL-2.0-only | im Abbild (Basis) |
| musl | 1.2.5-r13 | MIT | im Abbild (Basis) |
| alpine-baselayout, apk-tools | 3.7.0-r0, 2.14.13-r0 | GPL-2.0-only | im Abbild (Basis) |
| zlib | 1.4-r0 | Zlib | im Abbild |
| tzdata | 2026f-r0 | Public-Domain | im Abbild |
| ca-certificates | 20261020-r0 | MPL-2.0 AND MIT | im Abbild |
| su-exec | 0.2-r3 | MIT | im Abbild |
| libva | 2.22.0-r1 | MIT | im Abbild |
| mesa-va-gallium | 25.1.9-r0 | MIT AND SGI-B-2.0 AND BSL-1.0 | im Abbild |
| intel-media-driver (nur x86_64) | 25.2.6-r0 | BSD-3-Clause AND MIT | im Abbild |
| libva-intel-driver (nur x86_64) | 2.4.1-r2 | MIT | im Abbild |

Hinweis: Das Paket `onevpl-intel-gpu` gibt es in Alpine 3.22 nicht; das
Dockerfile fällt dann auf `intel-media-driver` allein zurück.

**Pflichten aus der GPL und wie Lumen sie erfüllt.** Wer ein Abbild mit
GPL-Programmen weitergibt, muss den dazugehörigen Quelltext zugänglich
machen. Das Abbild nennt dafür:

- die genaue Alpine-Fassung und jede Paketfassung in
  `/usr/share/licenses/lumen/alpine-pakete.txt` (beim Bauen erzeugt),
- die Bauanleitungen und Patches: Alpine *aports*, Zweig `3.22-stable`,
  <https://gitlab.alpinelinux.org/alpine/aports/-/tree/3.22-stable>
  (Spiegel: <https://github.com/alpinelinux/aports/tree/3.22-stable>),
- die unveränderten Quellarchive: <https://distfiles.alpinelinux.org/distfiles/v3.22/>
  (dort liegen z. B. `ffmpeg-6.1.2.tar.xz`, `x265_3.6.tar.gz`, `busybox-1.37.0.tar.bz2`).

Siehe auch die offenen Punkte unten: Ein Verweis auf fremde Server erfüllt
GPL-2.0 §3 streng genommen nicht ganz.

**Vermittler-Abbild `lumen-media-relay`**: `scratch` ohne Alpine-Pakete;
enthält nur `lumen-relay` (Go, BSD-3-Clause, AGPL), `LICENSE` und
`THIRD_PARTY.md`.

## 3. Web-Oberfläche (`server/cmd/lumen/web`)

Keine fremden Skripte, Stylesheets, Schriften oder Bilder.

- Alle Dateien tragen die Lumen-Kopfzeile (AGPL-3.0-only).
- Symbole sind eigene, einfache SVG-Pfade im Code (`I = {…}` in `app.js`);
  das Lumen-Symbol entsteht mit `tools/symbol_bauen.py`.
- Schrift: nur ein Verweis auf Systemschriften (`-apple-system`,
  `"SF Pro Text"`, `Inter`, `system-ui`). Es wird **keine** Schriftdatei
  mitgeliefert; auf Apple-Geräten nimmt der Browser die vorhandene
  Systemschrift. Keine SF-Symbols-Grafiken, keine kopierten Apple-Bilder.
- 🍅 ist ein Emoji der Systemschrift. „IMDb" und „Rotten Tomatoes" werden
  nur als Namen der Quelle genannt (beschreibende Nennung, keine Logos).

## 4. Android-Apps (`android/`)

| Name | Fassung | Lizenz (SPDX) | Quelle | Wie |
|---|---|---|---|---|
| AndroidX Core, Lifecycle, Activity, WorkManager, Security-Crypto, MediaRouter | 1.15.0 / 2.8.7 / 1.9.3 / 2.9.1 / 1.1.0 / 1.7.0 | Apache-2.0 | <https://android.googlesource.com/platform/frameworks/support> | eingebaut |
| Jetpack Compose (UI, Material 3, Material Icons Extended) | BOM 2024.12.01 | Apache-2.0 | wie oben | eingebaut |
| AndroidX TV (tv-foundation, tv-material) | 1.0.0-alpha11 / 1.0.0 | Apache-2.0 | wie oben | eingebaut (TV-App) |
| AndroidX Media3 (ExoPlayer, HLS, UI, Session) | 1.5.1 | Apache-2.0 | <https://github.com/androidx/media> | eingebaut |
| Coil (mit OkHttp, Okio) | 2.7.0 | Apache-2.0 | <https://github.com/coil-kt/coil> | eingebaut |
| Kotlin-Standardbibliothek, kotlinx-coroutines | 2.0.21 | Apache-2.0 | <https://github.com/JetBrains/kotlin> | eingebaut |
| Google Tink (über security-crypto) | – | Apache-2.0 | <https://github.com/tink-crypto> | eingebaut |
| desugar_jdk_libs | 2.1.3 | GPL-2.0-only WITH Classpath-exception-2.0 | <https://github.com/google/desugar_jdk_libs> | eingebaut |
| **Google Play services Cast Framework** (mit play-services-base, -basement, -cast, -tasks, datatransport) | 21.5.0 | **proprietär** (Android Software Development Kit License) | <https://developers.google.com/cast> | eingebaut (**nur Handy-App**) |
| JUnit | 4.13.2 | EPL-1.0 | <https://junit.org/junit4/> | nur Bau |
| Android Gradle Plugin, Gradle, Kotlin-Compiler | 8.7.3 / – / 2.0.21 | Apache-2.0 | – | nur Bau |

Apache-2.0 und die Classpath-Ausnahme sind mit der AGPL-3.0 vereinbar. Für das
**Cast-SDK** erteilen die Lumen-Autoren eine zusätzliche Erlaubnis nach
AGPL §7, siehe [LICENSE-EXCEPTIONS](LICENSE-EXCEPTIONS).
Die TV-App enthält kein Cast und keine Google-Play-Bibliothek.

## 5. Apple-Apps (`apple/`)

- **Keine** fremden Swift-Pakete. `LumenKit` ist Lumens eigenes Paket.
- Benutzt nur Systemframeworks von Apple (SwiftUI, AVKit, AVFoundation,
  UIKit/AppKit, CryptoKit, Security, ImageIO, PhotosUI, Observation) – sie
  gehören zum Betriebssystem und werden nicht mitgeliefert.
- Symbole aus SF Symbols werden nur über `Image(systemName:)` vom System
  geholt, nicht kopiert; Apple erlaubt das in Apps für Apple-Plattformen.
- App-Symbole entstehen mit `tools/symbol_bauen.py` (eigene Grafik).
- XcodeGen und XCTest: nur Bau.

## 6. Optionaler Begleiter: Aktualisierer (Watchtower)

| Name | Fassung | Lizenz | Quelle | Wie |
|---|---|---|---|---|
| Watchtower (gepflegte Fassung) | `nickfedor/watchtower:1.24.0` | Apache-2.0 | <https://github.com/nicholas-fedor/watchtower> | **getrennt** |

**Nicht Teil von Lumen.** Lumen liefert Watchtower nicht aus und baut es
nicht. Die Zeilen in `docker-compose.yml` sind auskommentiert; wer sie
einschaltet, lädt das Abbild selbst vom Docker Hub des Herausgebers
(`nickfedor/watchtower`). Es gelten dessen Lizenz (Apache-2.0) und Hinweise.
Watchtower geht auf `containrrr/watchtower` zurück (ebenfalls Apache-2.0);
das Label `com.centurylinklabs.watchtower.enable` ist nur ein Name, den
Watchtower erwartet.

## 7. Online-Dienste

Lumen fragt diese Dienste nur ab, wenn die Verwaltung dafür **einen eigenen
Schlüssel** hinterlegt (bzw. bei der Wikipedia: wenn TMDb keine Biografie
hat). Es gelten die Bedingungen des jeweiligen Dienstes; wer den Schlüssel
anlegt, stimmt ihnen zu.

| Dienst | Was Lumen holt | Bedingungen | Hinweis in Lumen |
|---|---|---|---|
| **TMDB** (The Movie Database) | Poster, Beschreibungen, Besetzung, Folgen | <https://www.themoviedb.org/api-terms-of-use>; kostenlos nur nicht-kommerziell; Nennung im „Über"-Teil verlangt | Einstellungen › „Datenquellen und Drittsoftware" und Verwaltung › Poster und Beschreibungen: „This product uses the TMDB API but is not endorsed or certified by TMDB." (übersetzt in der Oberflächensprache, wörtlich auf Englisch) |
| **OMDb** | IMDb- und Rotten-Tomatoes-Wertungen | Inhalte unter CC BY-NC 4.0, <https://www.omdbapi.com/legal.htm> | Einstellungen › „Datenquellen und Drittsoftware" |
| **Wikipedia** | Kurzbiografien von Personen | Texte unter CC BY-SA 4.0, <https://foundation.wikimedia.org/wiki/Policy:Terms_of_Use> | Personenseite: „Quelle: Wikipedia" mit Link zum Artikel; Lizenz in den Einstellungen |
| **OpenSubtitles** | Untertitel | Bedingungen auf <https://www.opensubtitles.com>, API: <https://opensubtitles.tawk.help/article/about-the-api> | Einstellungen › „Datenquellen und Drittsoftware" |
| **GitHub** (api.github.com) | Prüfung auf neue Fassungen | GitHub-Nutzungsbedingungen | – |
| TheTVDB, Trakt, IMDb | nur Links auf deren Seiten, keine Daten | – | – |
| Google Cast (Default Media Receiver) | Wiedergabe auf dem Chromecast (Android) | Google-Bedingungen, siehe [DATENSCHUTZ.md](DATENSCHUTZ.md#chromecast) | – |

## 8. Werkzeuge und CI

GitHub Actions (nur benutzt, nicht ausgeliefert): `actions/checkout`,
`actions/setup-go`, `actions/setup-java`, `actions/upload-artifact`,
`actions/download-artifact` (MIT), `docker/setup-qemu-action`,
`docker/setup-buildx-action`, `docker/login-action`,
`docker/build-push-action` (Apache-2.0), `gradle/actions/setup-gradle`
(MIT), `softprops/action-gh-release` (MIT). Die Skripte in `tools/` sind
eigene Arbeit; `tools/symbol_bauen.py` benutzt Pillow (MIT-CMU), das nicht
ausgeliefert wird.

---

## Offene Punkte (Entscheid der Lumen-Autoren)

Die folgenden Punkte sind **Vorschläge**, kein geltender Lizenztext. Nichts
davon ist umgesetzt.

1. **Cast-SDK in der Android-Handy-App.** Erledigt am 12.11.2026: Die
   Lumen-Autoren haben die zusätzliche Erlaubnis nach AGPL §7 erteilt
   (Weg a), siehe [LICENSE-EXCEPTIONS](LICENSE-EXCEPTIONS).
2. **App Store.** Die Bedingungen des App Store vertragen sich nach
   verbreiteter Ansicht nicht mit der (A)GPL. Vor einer Veröffentlichung
   dort braucht es eine Ausnahme der Rechteinhaber (Vorschlag unten). Die
   [CLA](CLA.md) §3 erlaubt den Lumen-Autoren, Beiträge auch unter anderen
   Bedingungen weiterzugeben – also auch solche Ausnahmen zu erteilen.
3. **Quelltext der GPL-Pakete im Abbild.** Der Verweis auf Alpines Server
   ist üblich, aber GPL-2.0 §3 (BusyBox, x264, x265) verlangt streng
   genommen, dass der Quelltext mitgeliefert oder schriftlich angeboten
   wird. Vorschlag: zu jeder Veröffentlichung die Quellarchive der GPL- und
   LGPL-Pakete aus `alpine-pakete.txt` als Release-Datei ablegen.
4. **TMDB-Logo.** TMDB verlangt neben dem Satz auch das Logo (kleiner als
   das eigene). Lumen zeigt bisher nur den Satz; das Logo wäre ein fremdes
   Markenbild und gehört nur nach Entscheid und unverändert hinein.

### Erteilt: zusätzliche Erlaubnis für Google Play services (AGPL §7)

Gilt seit dem 12.11.2026, siehe [LICENSE-EXCEPTIONS](LICENSE-EXCEPTIONS).

> Additional permission under GNU AGPL version 3 section 7
>
> If you modify this Program, or any covered work, by linking or combining
> it with the Google Play services client libraries
> (`com.google.android.gms:*`) and the Google Android Data Transport
> libraries (`com.google.android.datatransport:*`), or a modified version
> of those libraries, containing parts covered by the terms of the Android
> Software Development Kit License Agreement, the licensors of this Program
> grant you additional permission to convey the resulting work.
> Corresponding Source for a non-source form of such a combination shall
> not include the source code for the parts of those libraries used as well
> as that of the covered work.

### Vorschlag: Ausnahme für App-Stores (AGPL §7)

> Additional permission under GNU AGPL version 3 section 7
>
> The licensors of this Program grant you additional permission to convey
> the covered work, or a work based on it, in object code form through an
> application store (such as Apple's App Store or Google Play) whose terms
> of service impose usage rules, digital rights management or restrictions
> on further distribution on recipients, provided that the Corresponding
> Source of the conveyed work is made available to recipients free of
> charge under the terms of this License, in the manner described in
> section 6. Restrictions imposed by the application store are not
> considered further restrictions under section 10 for the purposes of this
> permission.

Beide Texte gehörten, falls gewünscht, als eigener Abschnitt in `LICENSE`
oder in eine Datei `LICENSE-EXCEPTIONS` und in die README. Vorher sollte
sie jemand mit juristischer Erfahrung prüfen.
