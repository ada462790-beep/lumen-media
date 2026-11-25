#!/usr/bin/env bash
# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
# SPDX-License-Identifier: AGPL-3.0-only
#
# Baut libmpv-2.dll für Windows x86-64 selbst — aus genau den Quellen in
# pakete.txt, mit dem MinGW-w64-GCC aus einem festen Debian-Stand — und legt
# daneben das Archiv mit genau diesen Quellen und diesen Bau-Dateien. Die
# GPL verlangt, dass der *entsprechende* Quelltext mit der DLL angeboten
# wird; hier ist das, was gebaut wird, und das, was im Archiv liegt,
# dasselbe Verzeichnis.
#
# Läuft als root in einem Debian-Container (Ablauf libmpv.yml):
#
#   bash bauen.sh werkzeug   # Werkzeuge aus snapshot.debian.org (fester Stand)
#   bash bauen.sh holen      # Quellen nach /bau/src (Tags und Commits geprüft)
#   bash bauen.sh quellen    # /bau/aus/libmpv-quellen.tar.xz
#   bash bauen.sh bauen      # /bau/aus/libmpv-2.dll
#   bash bauen.sh fertig     # Prüfsummen, LIESMICH.txt
#
# oder alles nacheinander: bash bauen.sh alles
#
# Wer die DLL aus dem Quellenarchiv nachbauen will: Archiv nach /bau
# auspacken (dann liegen /bau/src und /bau/bau-dateien da), im selben
# Debian-Abbild „bash /bau/bau-dateien/bauen.sh werkzeug" und „… bauen".

set -euo pipefail

HIER=$(cd "$(dirname "$0")" && pwd)
WURZEL=/bau
SRC=$WURZEL/src
PREFIX=$WURZEL/prefix
BAU=$WURZEL/b
AUS=$WURZEL/aus
HOST=x86_64-w64-mingw32
CC=$HOST-gcc-posix
CXX=$HOST-g++-posix
JOBS=$(nproc)

# Fester Stand von Debian für die Werkzeuge (Compiler, Binutils, Meson …).
# Das Abbild ist in libmpv.yml per Digest gepinnt; dieser Zeitpunkt muss
# zum Abbild passen (gleicher Tag oder später).
SNAPSHOT=20261123T000000Z
WERKZEUGE="gcc-mingw-w64-x86-64-posix g++-mingw-w64-x86-64-posix binutils-mingw-w64-x86-64
  mingw-w64-x86-64-dev mingw-w64-common meson ninja-build cmake nasm pkgconf python3 git
  ca-certificates make xz-utils diffutils file gcc libc6-dev"

export PKG_CONFIG_LIBDIR=$PREFIX/lib/pkgconfig
export PKG_CONFIG_PATH=
export LC_ALL=C.UTF-8 TZ=UTC

meld() { echo "::group::$*"; }
ende() { echo "::endgroup::"; }
fehler() { echo "::error::$*" >&2; exit 1; }

# Pakete aus pakete.txt: Name Quelle Tag Commit.
pakete() { grep -Ev '^[[:space:]]*(#|$)' "$HIER/pakete.txt"; }
commit_von() { pakete | awk -v n="$1" '$1 == n { print $4 }'; }

werkzeug() {
  meld "Werkzeuge (Debian-Snapshot $SNAPSHOT)"
  rm -f /etc/apt/sources.list /etc/apt/sources.list.d/*
  cat > /etc/apt/sources.list.d/snapshot.sources <<EOF
Types: deb
URIs: http://snapshot.debian.org/archive/debian/$SNAPSHOT
Suites: trixie trixie-updates
Components: main
Check-Valid-Until: no
Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg

Types: deb
URIs: http://snapshot.debian.org/archive/debian-security/$SNAPSHOT
Suites: trixie-security
Components: main
Check-Valid-Until: no
Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg
EOF
  export DEBIAN_FRONTEND=noninteractive
  local i
  for i in 1 2 3; do
    apt-get -o Acquire::Retries=5 -o Acquire::http::Timeout=60 update && break
    sleep 20
  done
  # shellcheck disable=SC2086
  apt-get -o Acquire::Retries=5 -o Acquire::http::Timeout=60 install -y --no-install-recommends $WERKZEUGE
  $CC --version | head -n1
  ende
}

holen() {
  mkdir -p "$SRC"
  local name url tag commit ist d i
  while read -r name url tag commit; do
    meld "Quelle $name ($tag $commit)"
    d=$SRC/$name
    rm -rf "$d"
    git init -q "$d"
    git -C "$d" remote add origin "$url"
    if [ "$tag" != - ]; then
      ist=$(git -C "$d" ls-remote origin "refs/tags/$tag^{}" | cut -f1)
      [ -n "$ist" ] || ist=$(git -C "$d" ls-remote origin "refs/tags/$tag" | cut -f1)
      [ "$ist" = "$commit" ] || fehler "$name: Tag $tag zeigt auf ${ist:-nichts}, pakete.txt sagt $commit"
    fi
    for i in 1 2 3; do
      git -C "$d" fetch -q --depth 1 origin "$commit" && break
      [ "$i" = 3 ] && fehler "$name: $commit nicht zu holen"
      sleep 10
    done
    git -C "$d" -c advice.detachedHead=false checkout -q FETCH_HEAD
    [ "$(git -C "$d" rev-parse HEAD)" = "$commit" ] || fehler "$name: falscher Commit"
    if [ "$name" = libplacebo ]; then
      # Nur die Untermodule, die der Bau braucht (nicht die der Beispiele).
      git -C "$d" submodule update -q --init --depth 1 \
        3rdparty/glad 3rdparty/jinja 3rdparty/markupsafe 3rdparty/fast_float 3rdparty/Vulkan-Headers
      git -C "$d" submodule status | sed 's/^/    /'
    fi
    ende
  done < <(pakete)
}

# Verzeichnis der Quellen und Werkzeuge — steht im Archiv und neben der DLL.
verzeichnis() {
  local name url tag commit
  echo "Quellen (Name, Git-Quelle, Tag, Commit):"
  while read -r name url tag commit; do
    printf '  %-14s %s %s %s\n' "$name" "$url" "$tag" "$commit"
    if [ "$name" = libplacebo ] && [ -d "$SRC/libplacebo/.git" ]; then
      git -C "$SRC/libplacebo" submodule status | awk '{ sub(/^[-+ U]/, "", $1); printf "    Untermodul %s %s\n", $2, $1 }'
    fi
  done < <(pakete)
  echo
  echo "Werkzeuge: Debian trixie, snapshot.debian.org $SNAPSHOT"
  if command -v dpkg-query > /dev/null; then
    # shellcheck disable=SC2086
    dpkg-query -W -f '  ${Package} ${Version}\n' $WERKZEUGE 2> /dev/null || true
  fi
}

quellen() {
  meld "Quellenarchiv"
  mkdir -p "$AUS"
  local stand
  stand=$(git -C "$SRC/mpv" log -1 --format=%ct)
  rm -rf "$WURZEL/bau-dateien"
  mkdir -p "$WURZEL/bau-dateien"
  cp "$HIER"/pakete.txt "$HIER"/bauen.sh "$HIER"/cross.txt "$HIER"/toolchain.cmake "$WURZEL/bau-dateien/"
  [ -f "$HIER/LIZENZ.txt" ] && cp "$HIER/LIZENZ.txt" "$WURZEL/bau-dateien/"
  {
    echo "Quelltext der libmpv-2.dll im Installer von Lumen für Windows."
    echo
    echo "src/          die Quellen, genau so, wie sie gebaut werden (ohne .git)"
    echo "bau-dateien/  das Bau-Skript (bauen.sh), Pin-Liste, Cross-Dateien"
    echo
    echo "Nachbauen: Archiv nach /bau auspacken, im Debian-Abbild aus"
    echo ".github/workflows/libmpv.yml von Lumen (debian:trixie, per Digest"
    echo "gepinnt) als root: bash /bau/bau-dateien/bauen.sh werkzeug && bash /bau/bau-dateien/bauen.sh bauen"
    echo
    verzeichnis
  } > "$WURZEL/LIESMICH.txt"
  tar -C "$WURZEL" --exclude-vcs --sort=name --mtime="@$stand" --owner=0 --group=0 --numeric-owner \
    -cf - LIESMICH.txt bau-dateien src | xz -T0 -6 > "$AUS/libmpv-quellen.tar.xz"
  ls -l "$AUS/libmpv-quellen.tar.xz"
  ende
}

meson_bau() { # Name, dann Meson-Optionen
  local name=$1
  shift
  meld "Bauen: $name"
  rm -rf "${BAU:?}/$name"
  meson setup "$BAU/$name" "$SRC/$name" --cross-file "$HIER/cross.txt" --prefix "$PREFIX" --libdir lib \
    --buildtype release --default-library static -Dprefer_static=true -Db_ndebug=true --wrap-mode nodownload "$@"
  ninja -C "$BAU/$name" -j "$JOBS"
  ninja -C "$BAU/$name" install > /dev/null
  ende
}

bau_zlib() {
  meld "Bauen: zlib"
  rm -rf "$BAU/zlib"
  cp -a "$SRC/zlib" "$BAU/zlib"
  rm -rf "$BAU/zlib/.git"
  make -C "$BAU/zlib" -f win32/Makefile.gcc -j "$JOBS" PREFIX=$HOST- CC=$CC AR=$HOST-gcc-ar-posix RC=$HOST-windres libz.a
  install -D -m644 "$BAU/zlib/libz.a" "$PREFIX/lib/libz.a"
  install -D -m644 "$BAU/zlib/zlib.h" "$PREFIX/include/zlib.h"
  install -D -m644 "$BAU/zlib/zconf.h" "$PREFIX/include/zconf.h"
  local v
  v=$(sed -n 's/^#define ZLIB_VERSION "\([^"]*\)".*/\1/p' "$BAU/zlib/zlib.h")
  mkdir -p "$PREFIX/lib/pkgconfig"
  printf 'prefix=%s\nlibdir=${prefix}/lib\nincludedir=${prefix}/include\n\nName: zlib\nDescription: zlib compression library\nVersion: %s\nLibs: -L${libdir} -lz\nCflags: -I${includedir}\n' \
    "$PREFIX" "$v" > "$PREFIX/lib/pkgconfig/zlib.pc"
  ende
}

FFMPEG_OPTIONEN=(
  --target-os=mingw32 --arch=x86_64 --cross-prefix=$HOST- --cc=$CC --cxx=$CXX
  --ar=$HOST-gcc-ar-posix --ranlib=$HOST-gcc-ranlib-posix
  --pkg-config=pkg-config --pkg-config-flags=--static
  --enable-static --disable-shared --disable-programs --disable-doc --disable-debug
  # Nichts automatisch einbauen — nur, was hier steht (LGPL, kein nonfree).
  --disable-autodetect --enable-w32threads
  --enable-d3d11va --enable-dxva2 --enable-schannel --enable-zlib --enable-libdav1d
  --disable-avdevice
  # Kodierer braucht mpv nur für Bildschirmfotos.
  --disable-encoders --enable-encoder=png,mjpeg
)

bau_ffmpeg() {
  meld "Bauen: FFmpeg"
  rm -rf "$BAU/ffmpeg"
  mkdir -p "$BAU/ffmpeg"
  (cd "$BAU/ffmpeg" && "$SRC/ffmpeg/configure" --prefix="$PREFIX" \
    --extra-cflags="-I$PREFIX/include" --extra-ldflags="-L$PREFIX/lib" "${FFMPEG_OPTIONEN[@]}") \
    || { tail -n 60 "$BAU/ffmpeg/ffbuild/config.log"; exit 1; }
  make -C "$BAU/ffmpeg" -j "$JOBS"
  make -C "$BAU/ffmpeg" install > /dev/null
  ende
}

cmake_bau() { # Name, Quellordner, dann CMake-Optionen
  local name=$1 quelle=$2
  shift 2
  rm -rf "${BAU:?}/$name"
  cmake -S "$quelle" -B "$BAU/$name" -G Ninja -DCMAKE_TOOLCHAIN_FILE="$HIER/toolchain.cmake" \
    -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX="$PREFIX" -DBUILD_SHARED_LIBS=OFF "$@"
}

bau_shaderc() {
  meld "Bauen: shaderc (mit glslang, SPIRV-Tools, SPIRV-Headers)"
  cmake_bau shaderc "$SRC/shaderc" \
    -DSHADERC_SKIP_TESTS=ON -DSHADERC_SKIP_EXAMPLES=ON -DSHADERC_SKIP_EXECUTABLES=ON \
    -DSHADERC_SKIP_COPYRIGHT_CHECK=ON -DSHADERC_ENABLE_WERROR_COMPILE=OFF \
    -DSHADERC_SPIRV_HEADERS_DIR="$SRC/spirv-headers" -DSHADERC_SPIRV_TOOLS_DIR="$SRC/spirv-tools" \
    -DSHADERC_GLSLANG_DIR="$SRC/glslang" \
    -DSPIRV_SKIP_TESTS=ON -DSPIRV_SKIP_EXECUTABLES=ON -DSPIRV_WERROR=OFF \
    -DENABLE_GLSLANG_BINARIES=OFF -DGLSLANG_TESTS=OFF -DENABLE_OPT=ON
  ninja -C "$BAU/shaderc" -j "$JOBS" shaderc_combined
  install -D -m644 "$BAU/shaderc/libshaderc/libshaderc_combined.a" "$PREFIX/lib/libshaderc_combined.a"
  mkdir -p "$PREFIX/include/shaderc"
  install -m644 "$SRC/shaderc/libshaderc/include/shaderc/"*.h "$SRC/shaderc/libshaderc/include/shaderc/"*.hpp "$PREFIX/include/shaderc/"
  # mpv und libplacebo suchen „shaderc"; statisch heisst das hier
  # shaderc_combined (alles in einer Bibliothek) plus libstdc++.
  printf 'prefix=%s\nlibdir=${prefix}/lib\nincludedir=${prefix}/include\n\nName: shaderc\nDescription: shaderc (statisch, combined)\nVersion: %s\nLibs: -L${libdir} -lshaderc_combined -lstdc++ -lpthread\nCflags: -I${includedir}\n' \
    "$PREFIX" "$(pakete | awk '$1 == "shaderc" { sub(/^v/, "", $3); print $3 }')" > "$PREFIX/lib/pkgconfig/shaderc.pc"
  ende
}

bau_spirv_cross() {
  meld "Bauen: SPIRV-Cross"
  cmake_bau spirv-cross "$SRC/spirv-cross" \
    -DSPIRV_CROSS_SHARED=OFF -DSPIRV_CROSS_STATIC=ON -DSPIRV_CROSS_CLI=OFF -DSPIRV_CROSS_ENABLE_TESTS=OFF \
    -DSPIRV_CROSS_ENABLE_GLSL=ON -DSPIRV_CROSS_ENABLE_HLSL=ON -DSPIRV_CROSS_ENABLE_MSL=OFF \
    -DSPIRV_CROSS_ENABLE_CPP=OFF -DSPIRV_CROSS_ENABLE_REFLECT=OFF -DSPIRV_CROSS_ENABLE_UTIL=OFF \
    -DSPIRV_CROSS_ENABLE_C_API=ON -DSPIRV_CROSS_FORCE_PIC=ON
  ninja -C "$BAU/spirv-cross" -j "$JOBS"
  ninja -C "$BAU/spirv-cross" install > /dev/null
  # mpv und libplacebo suchen „spirv-cross-c-shared"; hier statisch.
  local pc=$PREFIX/lib/pkgconfig/spirv-cross-c.pc
  [ -f "$pc" ] || pc=$PREFIX/share/pkgconfig/spirv-cross-c.pc
  [ -f "$pc" ] || fehler "spirv-cross-c.pc fehlt"
  # Die statische spirv-cross-c.pc nennt nur die C-Schnittstelle — die
  # Teile darunter (HLSL, GLSL, Kern) und libstdc++ gehören dazu.
  sed -e 's/^Name:.*/Name: spirv-cross-c-shared/' \
    -e '/^Libs:/ c\Libs: -L${libdir} -lspirv-cross-c -lspirv-cross-hlsl -lspirv-cross-glsl -lspirv-cross-core -lstdc++ -lpthread' \
    "$pc" > "$PREFIX/lib/pkgconfig/spirv-cross-c-shared.pc"
  cat "$PREFIX/lib/pkgconfig/spirv-cross-c-shared.pc"
  ende
}

bauen() {
  mkdir -p "$PREFIX/lib/pkgconfig" "$BAU" "$AUS"
  local anfang=$SECONDS
  bau_zlib
  # Zuerst die C++-Teile für die Shader (dauern am längsten, scheitern am
  # ehesten), dann libplacebo, dann Dekoder und Untertitel.
  bau_shaderc
  bau_spirv_cross
  meson_bau lcms2 -Dutils=false -Dfastfloat=false -Dthreaded=false -Dtests=disabled -Djpeg=disabled -Dtiff=disabled
  meson_bau libplacebo -Dvulkan=disabled -Dopengl=enabled -Dd3d11=enabled -Dshaderc=enabled -Dglslang=disabled \
    -Dlcms=enabled -Ddovi=enabled -Dlibdovi=disabled -Ddemos=false -Dtests=false -Dunwind=disabled -Dxxhash=disabled
  meson_bau dav1d -Denable_tools=false -Denable_examples=false -Denable_tests=false -Denable_docs=false \
    -Dxxhash_muxer=disabled
  bau_ffmpeg
  meson_bau freetype -Dzlib=system -Dharfbuzz=disabled -Dpng=disabled -Dbzip2=disabled -Dbrotli=disabled \
    -Dtests=disabled
  meson_bau fribidi -Ddocs=false -Dbin=false -Dtests=false
  meson_bau harfbuzz -Dauto_features=disabled -Dfreetype=enabled
  meson_bau libass -Dauto_features=disabled -Ddirectwrite=enabled -Dasm=enabled \
    -Drequire-system-font-provider=true

  meld "Bauen: mpv"
  rm -rf "$BAU/mpv"
  # Alles statisch in die eine DLL (auch libgcc, libstdc++, winpthreads):
  # neben Lumen.exe liegt nur libmpv-2.dll.
  meson setup "$BAU/mpv" "$SRC/mpv" --cross-file "$HIER/cross.txt" --prefix "$PREFIX" --libdir lib \
    --buildtype release --default-library shared -Dprefer_static=true -Db_ndebug=true --wrap-mode nodownload \
    -Dc_link_args=-static -Dcpp_link_args=-static \
    -Dlibmpv=true -Dcplayer=false -Dgpl=true -Dbuild-date=false -Dtests=false \
    -Dauto_features=disabled \
    -Dd3d11=enabled -Dshaderc=enabled -Dspirv-cross=enabled -Dd3d-hwaccel=enabled -Dd3d9-hwaccel=enabled \
    -Dgl=enabled -Dgl-win32=enabled -Dgl-dxinterop=enabled -Dplain-gl=enabled -Ddirect3d=enabled \
    -Dwasapi=enabled -Dwin32-threads=enabled -Dlcms2=enabled -Dzlib=enabled -Dvector=enabled \
    | tee "$BAU/mpv-setup.log"
  ninja -C "$BAU/mpv" -j "$JOBS"
  ende

  meld "DLL prüfen"
  local dll
  dll=$(find "$BAU/mpv" -maxdepth 1 -name 'libmpv-2.dll' | head -n1)
  [ -n "$dll" ] || { ls -l "$BAU/mpv"; fehler "libmpv-2.dll nicht gebaut"; }
  $HOST-strip --strip-unneeded -o "$AUS/libmpv-2.dll" "$dll"
  file "$AUS/libmpv-2.dll"
  # Nur Windows selbst darf fehlen — keine MinGW-Laufzeit-DLLs.
  $HOST-objdump -p "$AUS/libmpv-2.dll" | sed -n 's/^[[:space:]]*DLL Name: //p' | sort -u | tee "$BAU/importe.txt"
  if grep -Eiq '^(libwinpthread|libstdc\+\+|libgcc|zlib|libdav1d|av|sw|libass|libplacebo|libshaderc|libspirv)' "$BAU/importe.txt"; then
    fehler "libmpv-2.dll braucht fremde DLLs (siehe oben)"
  fi
  local exporte
  exporte=$($HOST-objdump -p "$AUS/libmpv-2.dll" | awk '/\[Ordinal\/Name Pointer\] Table/,/^$/' | grep -c ' mpv_' || true)
  echo "Exportierte mpv_-Funktionen: $exporte"
  [ "$exporte" -ge 30 ] || fehler "zu wenige mpv_-Exporte ($exporte)"
  # Die Quellen sind nach dem Bau unverändert — sonst stimmte das Archiv nicht.
  local name rest
  while read -r name rest; do
    if [ -n "$(git -C "$SRC/$name" status --porcelain --ignore-submodules=none 2> /dev/null)" ]; then
      git -C "$SRC/$name" status --short | head -n 20
      fehler "Der Bau hat $SRC/$name verändert"
    fi
  done < <(pakete)
  echo "Bauzeit: $(((SECONDS - anfang) / 60)) min"
  ende
}

fertig() {
  meld "Prüfsummen und LIESMICH"
  {
    echo "libmpv-2.dll für Lumen (Windows x86-64), selbst gebaut aus festen Quellen."
    echo
    echo "Dateien:"
    (cd "$AUS" && sha256sum libmpv-2.dll libmpv-quellen.tar.xz | sed 's/^/  /')
    echo
    echo "Grössen: libmpv-2.dll $(du -k "$AUS/libmpv-2.dll" | cut -f1) KiB, libmpv-quellen.tar.xz $(du -k "$AUS/libmpv-quellen.tar.xz" | cut -f1) KiB"
    echo
    if [ -f "$BAU/importe.txt" ]; then
      echo "Importiert (nur Windows):"
      sed 's/^/  /' "$BAU/importe.txt"
      echo
    fi
    if [ -f "$BAU/mpv-setup.log" ]; then
      echo "mpv-Funktionen:"
      grep -A1 'List of enabled features' "$BAU/mpv-setup.log" | tail -n1 | fold -s -w 76 | sed 's/^/  /'
      echo
    fi
    echo "FFmpeg: configure ${FFMPEG_OPTIONEN[*]}"
    echo
    verzeichnis
  } > "$AUS/LIESMICH.txt"
  (cd "$AUS" && sha256sum libmpv-2.dll libmpv-quellen.tar.xz > SHA256SUMS)
  cat "$AUS/LIESMICH.txt"
  ende
}

case "${1:-}" in
  werkzeug) werkzeug ;;
  holen) holen ;;
  quellen) quellen ;;
  bauen) bauen ;;
  fertig) fertig ;;
  alles) werkzeug; holen; quellen; bauen; fertig ;;
  *) echo "Aufruf: bauen.sh werkzeug|holen|quellen|bauen|fertig|alles" >&2; exit 2 ;;
esac
