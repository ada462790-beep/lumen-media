# Third-party software and data sources

*As of 12 November 2026 · [Deutsche Fassung](DRITTSOFTWARE.md)*

Lumen itself is licensed under the **AGPL-3.0-only** (see [LICENSE](LICENSE)).
This file lists everything that is **not** written by the Lumen authors: what
is built into Lumen, what ships in the Docker images and apps, what you obtain
separately yourself, and which online services Lumen queries.

The full license texts of the libraries inside the Lumen binary are in the
[appendix](#appendix-license-texts) below.

Column **"How"**:

- **built in** – part of the Lumen binary or app that the project distributes
- **in image** – included in the Docker image `ghcr.io/ada462790-beep/lumen-media`, but a separate program
- **separate** – not part of Lumen; you download it yourself from its publisher
- **service** – an online service your server queries (with your own key)
- **build only** – used only to build or test, not shipped

---

## 1. Lumen server (binaries `lumen`, `lumen-relay`)

| Name | Version | License (SPDX) | Source | How | Obligations / met |
|---|---|---|---|---|---|
| Go standard library and runtime | 1.25 (build image `golang:1.25-alpine`) | BSD-3-Clause | <https://go.dev/LICENSE> | built in | Notice in every copy: text in the appendix; this file ships in the image and the release archive |
| github.com/mattn/go-sqlite3 | v1.14.52 | MIT | <https://github.com/mattn/go-sqlite3> | built in (`lumen` only) | as above; text in `server/vendor/github.com/mattn/go-sqlite3/LICENSE` |
| SQLite (amalgamation, bundled in go-sqlite3) | 3.53.4 | blessing (public domain) | <https://sqlite.org/copyright.html> | built in (`lumen` only) | none |
| github.com/skip2/go-qrcode | v0.0.0-20200617195104-da1b6568686e | MIT | <https://github.com/skip2/go-qrcode> | built in | as above; text in `server/vendor/github.com/skip2/go-qrcode/LICENSE` |

All of these are compatible with the AGPL-3.0 (MIT, BSD-3-Clause and public
domain code may be included in an AGPL work).

On Linux, `lumen` links dynamically against the system C library (glibc in
the release archive, musl in the image) – System Libraries in the GPL sense.
On Windows, MinGW-w64 runtime parts are added (GCC Runtime Library Exception
or MIT/ZPL), which permit this.

## 2. Docker image `lumen-media`

The image is based on **Alpine Linux 3.22** (`alpine:3.22`). Apart from the
Lumen binary and `docker-start.sh`, everything comes from Alpine packages.
Lumen runs FFmpeg as a **separate program** and does not link it; FFmpeg
stays under its own license (an "aggregate" in the GPL sense, not a combined
work).

Versions in Alpine 3.22 (x86_64, as of 2026-11-12; the exact list for each
image is inside it at `/usr/share/licenses/lumen/alpine-pakete.txt`):

| Package | Version | License per Alpine | How |
|---|---|---|---|
| ffmpeg, ffmpeg-libav* | 6.1.2-r2 | GPL-2.0-or-later AND LGPL-2.1-or-later; built with `--enable-gpl --enable-version3` (so as a whole **GPL-3.0-or-later**), without `--enable-nonfree` | in image |
| x264-libs | 0.164.3108-r0 | GPL-2.0-or-later | in image (pulled in by ffmpeg) |
| x265-libs | 3.6-r0 | GPL-2.0-or-later | in image (pulled in by ffmpeg) |
| other ffmpeg codec libraries (dav1d, aom, opus, vorbis, vpx, lame, libass …) | per Alpine | BSD, LGPL, MIT and others | in image |
| libssl3 / libcrypto3 (OpenSSL) | 3.5.x | Apache-2.0 | in image |
| busybox | 1.37.0-r20 | GPL-2.0-only | in image (base) |
| musl | 1.2.5-r13 | MIT | in image (base) |
| alpine-baselayout, apk-tools | 3.7.0-r0, 2.14.13-r0 | GPL-2.0-only | in image (base) |
| zlib | 1.4-r0 | Zlib | in image |
| tzdata | 2026f-r0 | Public-Domain | in image |
| ca-certificates | 20261020-r0 | MPL-2.0 AND MIT | in image |
| su-exec | 0.2-r3 | MIT | in image |
| libva | 2.22.0-r1 | MIT | in image |
| mesa-va-gallium | 25.1.9-r0 | MIT AND SGI-B-2.0 AND BSL-1.0 | in image |
| intel-media-driver (x86_64 only) | 25.2.6-r0 | BSD-3-Clause AND MIT | in image |
| libva-intel-driver (x86_64 only) | 2.4.1-r2 | MIT | in image |

Note: there is no `onevpl-intel-gpu` package in Alpine 3.22; the Dockerfile
then falls back to `intel-media-driver` alone.

**GPL obligations and how Lumen meets them.** Whoever distributes an image
containing GPL programs must make the corresponding source available. The
image points to:

- the exact Alpine release and every package version in
  `/usr/share/licenses/lumen/alpine-pakete.txt` (generated at build time),
- the build recipes and patches: Alpine *aports*, branch `3.22-stable`,
  <https://gitlab.alpinelinux.org/alpine/aports/-/tree/3.22-stable>
  (mirror: <https://github.com/alpinelinux/aports/tree/3.22-stable>),
- the unmodified source archives: <https://distfiles.alpinelinux.org/distfiles/v3.22/>
  (e.g. `ffmpeg-6.1.2.tar.xz`, `x265_3.6.tar.gz`, `busybox-1.37.0.tar.bz2`).

See the open points below: pointing to someone else's server does not fully
satisfy GPL-2.0 section 3 in the strict reading.

**Relay image `lumen-media-relay`**: `scratch`, no Alpine packages; contains
only `lumen-relay` (Go, BSD-3-Clause, AGPL), `LICENSE` and `THIRD_PARTY.md`.

## 3. Web UI (`server/cmd/lumen/web`)

No third-party scripts, stylesheets, fonts or images.

- Every file carries the Lumen header (AGPL-3.0-only).
- Icons are Lumen's own simple SVG paths in the code (`I = {…}` in
  `app.js`); the Lumen logo is generated by `tools/symbol_bauen.py`.
- Font: only a reference to system fonts (`-apple-system`, `"SF Pro Text"`,
  `Inter`, `system-ui`). **No** font file is shipped; on Apple devices the
  browser uses the installed system font. No SF Symbols artwork, no copied
  Apple images.
- 🍅 is a system-font emoji. "IMDb" and "Rotten Tomatoes" are only named as
  the source (descriptive use, no logos).

## 4. Android apps (`android/`)

| Name | Version | License (SPDX) | Source | How |
|---|---|---|---|---|
| AndroidX Core, Lifecycle, Activity, WorkManager, Security-Crypto, MediaRouter | 1.15.0 / 2.8.7 / 1.9.3 / 2.9.1 / 1.1.0 / 1.7.0 | Apache-2.0 | <https://android.googlesource.com/platform/frameworks/support> | built in |
| Jetpack Compose (UI, Material 3, Material Icons Extended) | BOM 2024.12.01 | Apache-2.0 | as above | built in |
| AndroidX TV (tv-foundation, tv-material) | 1.0.0-alpha11 / 1.0.0 | Apache-2.0 | as above | built in (TV app) |
| AndroidX Media3 (ExoPlayer, HLS, UI, Session) | 1.5.1 | Apache-2.0 | <https://github.com/androidx/media> | built in |
| Coil (with OkHttp, Okio) | 2.7.0 | Apache-2.0 | <https://github.com/coil-kt/coil> | built in |
| Kotlin standard library, kotlinx-coroutines | 2.0.21 | Apache-2.0 | <https://github.com/JetBrains/kotlin> | built in |
| Google Tink (via security-crypto) | – | Apache-2.0 | <https://github.com/tink-crypto> | built in |
| desugar_jdk_libs | 2.1.3 | GPL-2.0-only WITH Classpath-exception-2.0 | <https://github.com/google/desugar_jdk_libs> | built in |
| **Google Play services Cast Framework** (with play-services-base, -basement, -cast, -tasks, datatransport) | 21.5.0 | **proprietary** (Android Software Development Kit License) | <https://developers.google.com/cast> | built in (**phone app only**) |
| JUnit | 4.13.2 | EPL-1.0 | <https://junit.org/junit4/> | build only |
| Android Gradle Plugin, Gradle, Kotlin compiler | 8.7.3 / – / 2.0.21 | Apache-2.0 | – | build only |

Apache-2.0 and the Classpath exception are compatible with the AGPL-3.0. For
the **Cast SDK** see "Open points" – the only real conflict. The TV app
contains no Cast and no Google Play library.

## 5. Apple apps (`apple/`)

- **No** third-party Swift packages. `LumenKit` is Lumen's own package.
- Uses only Apple system frameworks (SwiftUI, AVKit, AVFoundation,
  UIKit/AppKit, CryptoKit, Security, ImageIO, PhotosUI, Observation) – part
  of the operating system, not shipped.
- SF Symbols are only requested from the system via `Image(systemName:)`,
  not copied; Apple permits this in apps for Apple platforms.
- App icons are generated by `tools/symbol_bauen.py` (own artwork).
- XcodeGen and XCTest: build only.

## 6. Optional companion: updater (Watchtower)

| Name | Version | License | Source | How |
|---|---|---|---|---|
| Watchtower (maintained fork) | `nickfedor/watchtower:1.24.0` | Apache-2.0 | <https://github.com/nicholas-fedor/watchtower> | **separate** |

**Not part of Lumen.** Lumen neither ships nor builds Watchtower. The lines
in `docker-compose.yml` are commented out; whoever enables them pulls the
image themselves from the publisher's Docker Hub account
(`nickfedor/watchtower`). Its own license (Apache-2.0) and notices apply.
Watchtower derives from `containrrr/watchtower` (also Apache-2.0); the label
`com.centurylinklabs.watchtower.enable` is just a name Watchtower expects.

## 7. Online services

Lumen only queries these services when an admin enters **their own key**
(for Wikipedia: when TMDB has no biography). The terms of each service apply;
whoever creates the key accepts them.

| Service | What Lumen fetches | Terms | Notice in Lumen |
|---|---|---|---|
| **TMDB** (The Movie Database) | posters, descriptions, cast, episodes | <https://www.themoviedb.org/api-terms-of-use>; free for non-commercial use only; attribution required in the "About" section | Settings › "Data sources and third-party software" and Admin › Posters and descriptions: "This product uses the TMDB API but is not endorsed or certified by TMDB." (verbatim in English, translated in German UIs) |
| **OMDb** | IMDb and Rotten Tomatoes ratings | content under CC BY-NC 4.0, <https://www.omdbapi.com/legal.htm> | Settings › "Data sources and third-party software" |
| **Wikipedia** | short biographies | text under CC BY-SA 4.0, <https://foundation.wikimedia.org/wiki/Policy:Terms_of_Use> | Person page: "Source: Wikipedia" with a link to the article; license named in Settings |
| **OpenSubtitles** | subtitles | terms at <https://www.opensubtitles.com>, API: <https://opensubtitles.tawk.help/article/about-the-api> | Settings › "Data sources and third-party software" |
| **GitHub** (api.github.com) | checking for new versions | GitHub Terms of Service | – |
| TheTVDB, Trakt, IMDb | links to their pages only, no data | – | – |
| Google Cast (Default Media Receiver) | playback on Chromecast (Android) | Google's terms, see [DATENSCHUTZ.md](DATENSCHUTZ.md#chromecast) | – |

## 8. Tools and CI

GitHub Actions (used, not shipped): `actions/checkout`, `actions/setup-go`,
`actions/setup-java`, `actions/upload-artifact`, `actions/download-artifact`
(MIT), `docker/setup-qemu-action`, `docker/setup-buildx-action`,
`docker/login-action`, `docker/build-push-action` (Apache-2.0),
`gradle/actions/setup-gradle` (MIT), `softprops/action-gh-release` (MIT).
The scripts in `tools/` are Lumen's own; `tools/symbol_bauen.py` uses Pillow
(MIT-CMU), which is not shipped.

---

## Open points (for the Lumen authors to decide)

The following are **proposals**, not license terms in force. None of them is
implemented.

1. **Cast SDK in the Android phone app.** The release ships `lumen-handy.apk`
   containing the proprietary Google Play services library. The AGPL
   requires complete source for every part that is not a System Library;
   there is none for the Cast SDK. Anyone redistributing that APK therefore
   cannot fully comply with the AGPL. Options: (a) an additional permission
   under AGPL section 7 (proposal below), (b) a "frei" build flavor without
   Cast, (c) both.
2. **App Store.** The App Store terms are widely considered incompatible
   with the (A)GPL. Publishing there needs an exception from the copyright
   holders (proposal below). Section 3 of the [CLA](CLA.md) allows the Lumen
   authors to distribute contributions under other terms too, so they can
   grant such exceptions.
3. **Source for the GPL packages in the image.** Pointing to Alpine's
   servers is common practice, but GPL-2.0 section 3 (BusyBox, x264, x265)
   strictly requires the source to accompany the image or a written offer.
   Proposal: attach the source archives of the GPL/LGPL packages listed in
   `alpine-pakete.txt` to each release.
4. **TMDB logo.** Besides the sentence, TMDB asks for its logo (less
   prominent than Lumen's own). Lumen currently shows only the sentence; the
   logo is a third-party trademark and should only be added by decision and
   unmodified.

### Proposal: additional permission for Google Play services (AGPL section 7)

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

### Proposal: exception for app stores (AGPL section 7)

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

If wanted, both texts would go into their own section of `LICENSE` or a
file `LICENSE-EXCEPTIONS`, and into the README. Someone with legal
experience should review them first.

---

## Appendix: license texts

### Go (BSD-3-Clause)

```
Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

### github.com/mattn/go-sqlite3 (MIT)

```
The MIT License (MIT)

Copyright (c) 2014 Yasuhiro Matsumoto

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

### github.com/skip2/go-qrcode (MIT)

```
Copyright (c) 2014 Tom Harwood

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
```

### SQLite

SQLite is in the public domain: "The author disclaims copyright to this source code. In place of a legal notice, here is a blessing: May you do good and not evil. May you find forgiveness for yourself and forgive others. May you share freely, never taking more than you give." (<https://sqlite.org/copyright.html>)
