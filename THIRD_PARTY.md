# Third-party software and data sources

*As of 22 November 2026 · [Deutsche Fassung](DRITTSOFTWARE.md)*

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
| 7zip (7-Zip, program `7zz`) | 24.09-r0 | LGPL-2.0-only per Alpine (7-Zip itself: LGPL-2.1-or-later, some parts BSD-3-Clause); Alpine builds it without the RAR code (`DISABLE_RAR=1`), so the unRAR restriction does not apply | in image; Lumen runs it as a separate program (unpacking ISO images for “Convert to MKV”), source via `tools/gpl_quellen.sh` |
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
- The **TMDB logo** (`web/tmdb.svg`) is TMDB\'s official logo, unmodified, as
  TMDB\'s API terms require in the "About" section. It is a TMDB trademark and
  not covered by Lumen\'s license.

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
the **Cast SDK** the Lumen authors grant an additional permission under AGPL
section 7, see [LICENSE-EXCEPTIONS](LICENSE-EXCEPTIONS). The TV app
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
The Windows app is packaged with NSIS (on GitHub's Windows runner, via
Chocolatey if needed).
The scripts in `tools/` are Lumen's own; `tools/symbol_bauen.py` uses Pillow
(MIT-CMU), which is not shipped.

## 9. Windows app (`desktop/windows/`)

| Name | Version | License (SPDX) | Source | How | Obligations / met |
|---|---|---|---|---|---|
| Go standard library and runtime | 1.25 | BSD-3-Clause | <https://go.dev/LICENSE> | built in (`Lumen.exe`) | text in the appendix; THIRD_PARTY.md is installed next to the program |
| github.com/wailsapp/go-webview2 (packages `edge`, `webviewloader`) | v1.0.23 | MIT (`webviewloader`: ISC) | <https://github.com/wailsapp/go-webview2> | built in | texts in the appendix |
| github.com/jchv/go-winloader | v0.0.0-20210711035445-715c2860da7e | ISC | <https://github.com/jchv/go-winloader> | built in | text in the appendix |
| golang.org/x/sys | v0.27.0 | BSD-3-Clause | <https://go.googlesource.com/sys> | built in | same text as Go (appendix) |
| github.com/tc-hib/winres (with nfnt/resize, golang.org/x/image) | v0.3.1 | 0BSD-style (ISC without notice requirement); ISC; BSD-3-Clause | <https://github.com/tc-hib/winres> | build only | creates icon, manifest and version info of the `.exe` (`werkzeuge/ressourcen`) |
| NSIS (installer framework) | 3.x | zlib/libpng (some parts bzip2, CPL) | <https://nsis.sourceforge.io/License> | in the installer (the small stub that runs it) | zlib requires no notice in binaries; listed for completeness |
| Microsoft Edge WebView2 Runtime | Evergreen | Microsoft license (proprietary) | <https://developer.microsoft.com/microsoft-edge/webview2/> | **separate** – part of Windows or downloaded from Microsoft | Lumen neither ships nor links it; it is a system component |
| WebView2 bootstrapper (`MicrosoftEdgeWebview2Setup.exe`) | current at build time | Microsoft, redistribution permitted (WebView2 distribution) | <https://developer.microsoft.com/microsoft-edge/webview2/#download> | inside the installer, unmodified; only runs if WebView2 is missing | downloaded from Microsoft at build time and checked for Microsoft's signature; not in the repository |
| libmpv (`libmpv-2.dll`: mpv with FFmpeg, libplacebo, libass, HarfBuzz, FreeType, FriBidi, dav1d, Little CMS, shaderc with glslang and SPIRV-Tools, SPIRV-Cross, zlib; all statically linked into one DLL) | built by Lumen from fixed versions: [`desktop/windows/mpv/bau/pakete.txt`](desktop/windows/mpv/bau/pakete.txt) (tag and commit per package, e.g. mpv v0.42.0, FFmpeg n9.0.2, libplacebo v7.387.0); which DLL goes into the installer: [`desktop/windows/mpv/libmpv.txt`](desktop/windows/mpv/libmpv.txt) (pre-release `libmpv-YYYYMMDD`, SHA-256) | **GPL-3.0-or-later** as a whole (mpv GPL-2.0-or-later; FFmpeg LGPL-2.1-or-later, without `--enable-gpl` and without `--enable-nonfree`; libplacebo and FriBidi LGPL-2.1-or-later; libass ISC; HarfBuzz and Little CMS MIT; FreeType FTL/GPL-2.0-or-later; dav1d BSD-2-Clause; glslang BSD-3-Clause and others; shaderc, SPIRV-Tools, SPIRV-Cross Apache-2.0; zlib zlib license; plus the MinGW-w64 runtime: libgcc and libstdc++ GPL-3.0 with the GCC Runtime Library Exception, winpthreads MIT) | <https://github.com/mpv-player/mpv> and the sources in `pakete.txt` | in the installer next to `Lumen.exe`, unmodified; `Lumen.exe` loads it into its own process at run time (own player, `player_windows.go`) | see below; built in [`libmpv.yml`](.github/workflows/libmpv.yml), downloaded with checksum when the app is built, not in the repository |

The built-in licenses (MIT, ISC, BSD-3-Clause) are compatible with the
AGPL-3.0. The WebView2 Runtime is an operating system component (a "System
Library" in the GPL's sense) that Lumen calls at run time. `go-webview2`
also contains a copy of Microsoft's `WebView2Loader.dll`; it is only
embedded with the build tag `native_webview2loader`, which Lumen doesn't
set — `Lumen.exe` doesn't contain it (the loader is reimplemented in Go).

**libmpv and the AGPL.** `Lumen.exe` loads `libmpv-2.dll` into its own
process and calls its functions — unlike FFmpeg in the Docker image, this
is a *combined work*. AGPL-3.0 section 13 and GPL-3.0 section 13 expressly
permit combining an AGPL work with a GPL-3.0 work into a single work; each
part keeps its own license. A "GPL-2.0-only" library could not be
combined this way — Lumen's own libmpv build is GPL-3.0-or-later as a
whole: mpv is GPL-2.0-or-later, and the Apache-2.0 parts (shaderc,
SPIRV-Tools, SPIRV-Cross) can only be combined with GPL-3.0. FFmpeg is
built without `--enable-gpl` and without `--enable-nonfree`, with
`--disable-autodetect`: only what
[`bauen.sh`](desktop/windows/mpv/bau/bauen.sh) lists goes in. Builds with
`--enable-nonfree` (e.g. libfdk-aac) could not be distributed.
The additional permissions
in `LICENSE-EXCEPTIONS` cover the Lumen part only, not libmpv.

**Source of libmpv.** Whoever distributes `libmpv-2.dll` must offer the
corresponding source (GPL-3.0 section 6). That is why Lumen builds the DLL
itself (workflow [`libmpv.yml`](.github/workflows/libmpv.yml)): every
package is fixed in [`pakete.txt`](desktop/windows/mpv/bau/pakete.txt) by
tag *and* commit, the build stops if a tag points elsewhere, and the tools
(MinGW-w64 GCC, binutils, Meson …) come from a fixed Debian state (image
pinned by digest, `snapshot.debian.org`). The same run packs exactly the
source trees it builds, the build script and a list of all commits and tool
versions into `libmpv-quellen.tar.xz` (about 55 MB), and checks after the
build that the sources are unchanged. DLL and archive are attached together
to a pre-release `libmpv-YYYYMMDD`; both checksums are in
[`libmpv.txt`](desktop/windows/mpv/libmpv.txt).
[`tools/mpv_quellen.sh`](tools/mpv_quellen.sh) fetches exactly this archive
for every Lumen release and verifies its checksum, and `quellen.yml`
attaches it as `lumen-media-vX-libmpv-quellen.tar.xz`.

The installer puts `libmpv-2.dll`, `LICENSE.txt`, `LICENSE-EXCEPTIONS.txt`,
`THIRD_PARTY.md` and `DRITTSOFTWARE.md` next to `Lumen.exe`.

---

## Open points (for the Lumen authors to decide)

The following are **proposals**, not license terms in force. None of them is
implemented.

1. **Cast SDK in the Android phone app.** Resolved on 2026-11-12: the Lumen
   authors granted the additional permission under AGPL section 7 (option a),
   see [LICENSE-EXCEPTIONS](LICENSE-EXCEPTIONS).
2. **App Store.** Resolved on 2026-11-12: the Lumen authors allow
   distribution through app stores as long as the source stays available
   (additional permission under AGPL section 7, see
   [LICENSE-EXCEPTIONS](LICENSE-EXCEPTIONS)).
3. **Source for the GPL packages in the image.** Resolved: for every release
   the `quellen.yml` workflow attaches `lumen-media-vX-gpl-quellen.tar` with
   Alpine's build recipes and the verified source archives of every GPL and
   LGPL package in the image.
4. **TMDB logo.** Resolved: small and unmodified under Settings › Data
   sources and third-party software, linking to themoviedb.org.
5. **Source of libmpv (Windows app).** Resolved on 2026-11-25: libmpv is
   built from fixed versions by Lumen itself (`libmpv.yml`,
   `desktop/windows/mpv/bau/`), and every release carries exactly the
   source archive from that same build (see "Source of libmpv").

### Granted: additional permission for Google Play services (AGPL section 7)

In force since 2026-11-12, see [LICENSE-EXCEPTIONS](LICENSE-EXCEPTIONS).

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

### Granted: exception for app stores (AGPL section 7)

In force since 2026-11-12, see [LICENSE-EXCEPTIONS](LICENSE-EXCEPTIONS).

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

Both texts are in force as written; they are in
[LICENSE-EXCEPTIONS](LICENSE-EXCEPTIONS).

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

### github.com/wailsapp/go-webview2 (MIT)

```
MIT License

Copyright (c) 2020 John Chadwick
Some portions Copyright (c) 2017 Serge Zaitsev

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

### github.com/wailsapp/go-webview2/webviewloader (ISC)

```
ISC License (ISC)

Copyright (c) 2020 John Chadwick
Copyright (c) 2022 Wails Project Developers

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY
AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM
LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR
OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR
PERFORMANCE OF THIS SOFTWARE.
```

### github.com/jchv/go-winloader (ISC)

```
ISC License

Copyright © 2021, John Chadwick <john@jchw.io>

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY
AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM
LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR
OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR
PERFORMANCE OF THIS SOFTWARE.
```

golang.org/x/sys is under the same BSD-3-Clause text as Go ("Copyright 2009
The Go Authors"), see above.
