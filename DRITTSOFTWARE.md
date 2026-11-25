# Drittsoftware und Datenquellen

*Stand: 22. November 2026 · [English version](THIRD_PARTY.md)*

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
| 7zip (7-Zip, Programm `7zz`) | 24.09-r0 | LGPL-2.0-only laut Alpine (7-Zip selbst: LGPL-2.1-or-later, einzelne Teile BSD-3-Clause); Alpine baut ohne den RAR-Code (`DISABLE_RAR=1`), die unRAR-Einschränkung gilt darum nicht | im Abbild; Lumen ruft es als eigenes Programm auf (ISO-Abbilder auspacken für „In MKV umwandeln“), Quelltext über `tools/gpl_quellen.sh` |
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
- Das **TMDB-Logo** (`web/tmdb.svg`) ist TMDBs offizielles Logo, unverändert,
  wie es TMDBs API-Bedingungen im „Über"-Teil verlangen. Es ist eine Marke
  von TMDB und fällt nicht unter Lumens Lizenz.

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
(MIT), `softprops/action-gh-release` (MIT). Für die Windows-App kommt NSIS
dazu (auf dem Windows-Rechner von GitHub, nötigenfalls über Chocolatey). Die Skripte in `tools/` sind
eigene Arbeit; `tools/symbol_bauen.py` benutzt Pillow (MIT-CMU), das nicht
ausgeliefert wird.

## 9. Windows-App (`desktop/windows/`)

| Name | Fassung | Lizenz (SPDX) | Quelle | Wie | Pflichten / erfüllt |
|---|---|---|---|---|---|
| Go-Standardbibliothek und -Laufzeit | 1.25 | BSD-3-Clause | <https://go.dev/LICENSE> | eingebaut (`Lumen.exe`) | Text im Anhang; THIRD_PARTY.md liegt im Installationsordner |
| github.com/wailsapp/go-webview2 (Pakete `edge`, `webviewloader`) | v1.0.23 | MIT (`webviewloader`: ISC) | <https://github.com/wailsapp/go-webview2> | eingebaut | Texte im Anhang von THIRD_PARTY.md |
| github.com/jchv/go-winloader | v0.0.0-20210711035445-715c2860da7e | ISC | <https://github.com/jchv/go-winloader> | eingebaut | Text im Anhang von THIRD_PARTY.md |
| golang.org/x/sys | v0.27.0 | BSD-3-Clause | <https://go.googlesource.com/sys> | eingebaut | derselbe Text wie für Go (Anhang) |
| github.com/tc-hib/winres (mit nfnt/resize, golang.org/x/image) | v0.3.1 | 0BSD-artig (ISC ohne Hinweispflicht); ISC; BSD-3-Clause | <https://github.com/tc-hib/winres> | nur Bau | legt Symbol, Manifest und Versionsangaben der `.exe` an (`werkzeuge/ressourcen`) |
| NSIS (Installer-Rahmen) | 3.x | zlib/libpng (einzelne Teile bzip2, CPL) | <https://nsis.sourceforge.io/License> | im Installer (der kleine Entpacker, der den Installer ausführt) | zlib verlangt keinen Hinweis in Binärdateien; genannt der Vollständigkeit halber |
| Microsoft Edge WebView2 Runtime | Evergreen | Microsoft-Lizenz (proprietär) | <https://developer.microsoft.com/microsoft-edge/webview2/> | **getrennt** – Teil von Windows bzw. von Microsoft geladen | Lumen liefert sie nicht aus und linkt sie nicht; sie ist eine Systemkomponente |
| WebView2-Bootstrapper (`MicrosoftEdgeWebview2Setup.exe`) | aktuell beim Bau | Microsoft, weitergeben erlaubt (WebView2 Distribution) | <https://developer.microsoft.com/microsoft-edge/webview2/#download> | im Installer, unverändert; läuft nur, wenn WebView2 fehlt | beim Bau von Microsoft geladen und auf Microsofts Unterschrift geprüft; steht nicht im Repository |
| libmpv (`libmpv-2.dll`: mpv mit FFmpeg, libplacebo, libass, HarfBuzz, FreeType, FriBidi, dav1d, Little CMS, shaderc mit glslang und SPIRV-Tools, SPIRV-Cross, zlib; alles statisch in einer DLL) | selbst gebaut aus festen Fassungen: [`desktop/windows/mpv/bau/pakete.txt`](desktop/windows/mpv/bau/pakete.txt) (Tag und Commit je Paket, u. a. mpv v0.42.0, FFmpeg n9.0.2, libplacebo v7.387.0); welche DLL eingebaut wird: [`desktop/windows/mpv/libmpv.txt`](desktop/windows/mpv/libmpv.txt) (Vorab-Veröffentlichung `libmpv-JJJJMMTT`, SHA-256) | **GPL-3.0-or-later** als Ganzes (mpv GPL-2.0-or-later; FFmpeg LGPL-2.1-or-later, ohne `--enable-gpl` und ohne `--enable-nonfree`; libplacebo und FriBidi LGPL-2.1-or-later; libass ISC; HarfBuzz und Little CMS MIT; FreeType FTL/GPL-2.0-or-later; dav1d BSD-2-Clause; glslang BSD-3-Clause u. a.; shaderc, SPIRV-Tools, SPIRV-Cross Apache-2.0; zlib zlib-Lizenz; dazu die MinGW-w64-Laufzeit: libgcc und libstdc++ GPL-3.0 mit GCC-Laufzeitausnahme, winpthreads MIT) | <https://github.com/mpv-player/mpv> und die Quellen in `pakete.txt` | im Installer neben `Lumen.exe`, unverändert; `Lumen.exe` lädt sie zur Laufzeit in den eigenen Prozess (eigener Player, `player_windows.go`) | siehe unten; gebaut in [`libmpv.yml`](.github/workflows/libmpv.yml), beim Bau der App mit Prüfsumme geladen, steht nicht im Repository |

Die eingebauten Lizenzen (MIT, ISC, BSD-3-Clause) sind mit der AGPL-3.0
vereinbar. Die WebView2-Laufzeit ist eine Komponente des Betriebssystems
(„System Library" im Sinne der GPL): Lumen ruft sie zur Laufzeit auf, wie
ein Programm Windows aufruft. `go-webview2` enthält auch eine Kopie von
Microsofts `WebView2Loader.dll`; die wird nur mit dem Bau-Schalter
`native_webview2loader` eingebunden, den Lumen nicht setzt — `Lumen.exe`
enthält sie nicht (der Lader ist in Go nachgebaut).

**libmpv und die AGPL.** `Lumen.exe` lädt `libmpv-2.dll` in den eigenen
Prozess und ruft ihre Funktionen auf — das ist, anders als bei FFmpeg im
Docker-Abbild, ein *kombiniertes Werk*. Die AGPL-3.0 (Abschnitt 13) und die
GPL-3.0 (Abschnitt 13) erlauben ausdrücklich, ein AGPL-Werk mit einem
GPL-3.0-Werk zu einem Werk zu verbinden; jeder Teil behält seine Lizenz.
Mit einer Bibliothek unter „GPL-2.0-only" ginge das nicht — die selbst
gebaute libmpv ist als Ganzes GPL-3.0-or-later: mpv ist GPL-2.0-or-later,
und die Apache-2.0-Teile (shaderc, SPIRV-Tools, SPIRV-Cross) lassen sich nur
mit GPL-3.0 verbinden. FFmpeg ist ohne `--enable-gpl` und ohne
`--enable-nonfree` gebaut, mit `--disable-autodetect`: Es kommt nur hinein,
was in [`bauen.sh`](desktop/windows/mpv/bau/bauen.sh) steht. Bauten mit
`--enable-nonfree` (etwa mit libfdk-aac) dürften nicht weitergegeben werden.
Die Zusatzerlaubnisse in `LICENSE-EXCEPTIONS` gelten nur für den Lumen-Teil,
nicht für libmpv.

**Quelltext von libmpv.** Wer `libmpv-2.dll` weitergibt, muss den
zugehörigen Quelltext anbieten (GPL-3.0 Abschnitt 6). Lumen baut die DLL
darum selbst (Ablauf [`libmpv.yml`](.github/workflows/libmpv.yml)): Jedes
Paket ist in [`pakete.txt`](desktop/windows/mpv/bau/pakete.txt) mit Tag
*und* Commit festgelegt, der Bau bricht ab, wenn ein Tag woanders hinzeigt,
und die Werkzeuge (MinGW-w64-GCC, Binutils, Meson …) kommen aus einem
festen Stand von Debian (Abbild per Digest, `snapshot.debian.org`).
Derselbe Lauf packt genau die gebauten Quellbäume, das Bau-Skript und ein
Verzeichnis aller Commits und Werkzeug-Fassungen in
`libmpv-quellen.tar.xz` (rund 55 MB) und prüft nach dem Bau, dass die
Quellen unverändert sind. DLL und Archiv hängen zusammen an einer eigenen
Vorab-Veröffentlichung `libmpv-JJJJMMTT`; beide Prüfsummen stehen in
[`libmpv.txt`](desktop/windows/mpv/libmpv.txt).
[`tools/mpv_quellen.sh`](tools/mpv_quellen.sh) holt zu jeder
Lumen-Veröffentlichung genau dieses Archiv, prüft die Prüfsumme und
`quellen.yml` hängt es als `lumen-media-vX-libmpv-quellen.tar.xz` an.

Der Installer legt neben `Lumen.exe` auch `libmpv-2.dll`, `LICENSE.txt`,
`LICENSE-EXCEPTIONS.txt`, `THIRD_PARTY.md` und `DRITTSOFTWARE.md` ab.

---

## Offene Punkte (Entscheid der Lumen-Autoren)

Die folgenden Punkte sind **Vorschläge**, kein geltender Lizenztext. Nichts
davon ist umgesetzt.

1. **Cast-SDK in der Android-Handy-App.** Erledigt am 12.11.2026: Die
   Lumen-Autoren haben die zusätzliche Erlaubnis nach AGPL §7 erteilt
   (Weg a), siehe [LICENSE-EXCEPTIONS](LICENSE-EXCEPTIONS).
2. **App Store.** Erledigt am 12.11.2026: Die Lumen-Autoren erlauben die
   Verteilung über App-Stores, solange der Quelltext frei bleibt (zusätzliche
   Erlaubnis nach AGPL §7, siehe [LICENSE-EXCEPTIONS](LICENSE-EXCEPTIONS)).
3. **Quelltext der GPL-Pakete im Abbild.** Erledigt: Zu jeder
   Veröffentlichung hängt der Ablauf `quellen.yml` das Archiv
   `lumen-media-vX-gpl-quellen.tar` mit Alpines Rezepten und den geprüften
   Quellarchiven aller GPL- und LGPL-Pakete des Abbilds an.
4. **TMDB-Logo.** Erledigt: klein und unverändert unter Einstellungen ›
   Datenquellen und Drittsoftware, mit Link auf themoviedb.org.
5. **Quelltext von libmpv (Windows-App).** Erledigt am 25.11.2026: libmpv
   wird selbst aus festen Fassungen gebaut (`libmpv.yml`,
   `desktop/windows/mpv/bau/`), und an jeder Veröffentlichung hängt genau
   das Quellenarchiv aus demselben Bau (siehe „Quelltext von libmpv").

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

### Erteilt: Ausnahme für App-Stores (AGPL §7)

Gilt seit dem 12.11.2026, siehe [LICENSE-EXCEPTIONS](LICENSE-EXCEPTIONS).

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

Beide Texte gelten wörtlich so; sie stehen in
[LICENSE-EXCEPTIONS](LICENSE-EXCEPTIONS).
