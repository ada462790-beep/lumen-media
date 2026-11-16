#!/usr/bin/env bash
# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
#
# Veröffentlicht unter der GNU Affero General Public License v3.0.
# Der vollständige Text steht in der Datei LICENSE.
# SPDX-License-Identifier: AGPL-3.0-only
#
# Eine kleine, aber echte Testbibliothek für den Simulator-Ablauf
# (.github/workflows/simulator.yml): kurze Videos aus ffmpeg, Namen wie
# echte Titel und NFO-Dateien statt TMDb — so braucht der Lauf weder Netz
# noch API-Schlüssel und sieht trotzdem aus wie eine Mediathek.
#
#     tools/simulator/bibliothek.sh /tmp/lumen-sim/medien
#
# Was drin ist und wozu:
#   - Filmreihe mit zwei Filmen (The Dark Knight Collection) — gehört auf
#     die Startseite und unter „Filmreihen".
#   - Filmreihe mit nur einem Film (Matrix Collection) — darf auf der
#     Startseite nicht als Reihe auftauchen.
#   - Avatar in deutscher und englischer Fassung (zwei Dateien) — die
#     Fassung muss wählbar sein.
#   - Der Pate mit zwei Tonspuren und Untertitelspuren in der Datei,
#     Inception mit Untertiteln daneben (.de.srt/.en.srt).
#   - Bewertungen: IMDb-Note mit Nachkommastelle; Rotten Tomatoes setzt
#     einrichten.py (frisch/faul), weil das sonst nur OMDb liefert.
#   - Breaking Bad mit Specials (Staffel 0) und zwei Staffeln, Folgen mit
#     Untertitelspur; Dark mit einer Staffel und .de.srt.

set -euo pipefail

ZIEL="${1:?Zielordner fehlt}"
DAUER="${DAUER:-30}"
mkdir -p "$ZIEL"
ZIEL="$(cd "$ZIEL" && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Schrift für Plakate: was es gibt. Ohne Schrift bleibt das Plakat einfarbig.
SCHRIFT=""
for s in /usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf \
         /System/Library/Fonts/Supplemental/Arial\ Bold.ttf \
         /System/Library/Fonts/Helvetica.ttc \
         /Library/Fonts/Arial.ttf; do
  [ -f "$s" ] && { SCHRIFT="$s"; break; }
done

cat > "$TMP/de.srt" <<'S'
1
00:00:01,000 --> 00:00:05,000
Das ist ein deutscher Untertitel.

2
00:00:06,000 --> 00:00:12,000
Und noch eine zweite Zeile, etwas länger als die erste.
S
cat > "$TMP/en.srt" <<'S'
1
00:00:01,000 --> 00:00:05,000
This is an English subtitle.

2
00:00:06,000 --> 00:00:12,000
And a second, slightly longer line.
S

# video <ausgabe> [dauer] — H.264/AAC, eine Tonspur.
video() {
  local out="$1" d="${2:-$DAUER}"
  mkdir -p "$(dirname "$out")"
  ffmpeg -nostdin -y -loglevel error \
    -f lavfi -i "testsrc2=size=640x360:rate=24:duration=$d" \
    -f lavfi -i "sine=frequency=440:duration=$d" \
    -map 0:v -map 1:a -c:v libx264 -preset veryfast -crf 34 -pix_fmt yuv420p \
    -c:a aac -b:a 64k -metadata:s:a:0 language="${SPRACHE:-ger}" "$out"
}

# video_spuren <ausgabe> [dauer] — MKV mit deutscher und englischer
# Tonspur und je einer Untertitelspur.
video_spuren() {
  local out="$1" d="${2:-$DAUER}"
  mkdir -p "$(dirname "$out")"
  ffmpeg -nostdin -y -loglevel error \
    -f lavfi -i "testsrc2=size=640x360:rate=24:duration=$d" \
    -f lavfi -i "sine=frequency=440:duration=$d" \
    -f lavfi -i "sine=frequency=660:duration=$d" \
    -i "$TMP/de.srt" -i "$TMP/en.srt" \
    -map 0:v -map 1:a -map 2:a -map 3 -map 4 \
    -c:v libx264 -preset veryfast -crf 34 -pix_fmt yuv420p -c:a aac -b:a 64k -c:s srt \
    -metadata:s:a:0 language=ger -metadata:s:a:1 language=eng \
    -metadata:s:s:0 language=ger -metadata:s:s:1 language=eng "$out"
}

# bild <ausgabe> <breite> <höhe> <farbe> <text>
bild() {
  local out="$1" w="$2" h="$3" farbe="$4" text="$5"
  mkdir -p "$(dirname "$out")"
  local filter="drawbox=x=0:y=ih*0.62:w=iw:h=ih*0.38:color=black@0.45:t=fill"
  if [ -n "$SCHRIFT" ]; then
    local t="${text//:/\\:}"; t="${t//\'/}"
    filter="$filter,drawtext=fontfile='$SCHRIFT':text='$t':fontcolor=white:fontsize=h/14:x=(w-text_w)/2:y=h*0.72"
  fi
  ffmpeg -nostdin -y -loglevel error -f lavfi -i "color=c=$farbe:s=${w}x${h}" \
    -vf "$filter" -frames:v 1 "$out" 2>/dev/null \
  || ffmpeg -nostdin -y -loglevel error -f lavfi -i "color=c=$farbe:s=${w}x${h}" -frames:v 1 "$out"
}

# film_nfo <pfad> <titel> <jahr> <imdb-note> <stimmen> <fsk> <reihe> <genre> <inhalt> [imdb-id]
film_nfo() {
  local reihe=""
  [ -n "$7" ] && reihe="<set><name>$7</name></set>"
  cat > "$1" <<X
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<movie>
  <title>$2</title>
  <year>$3</year>
  <plot>$9</plot>
  <genre>$8</genre>
  <mpaa>$6</mpaa>
  <ratings>
    <rating name="imdb" max="10" default="true"><value>$4</value><votes>$5</votes></rating>
  </ratings>
  <uniqueid type="imdb">${10:-}</uniqueid>
  <runtime>120</runtime>
  $reihe
  <director>Regie Beispiel</director>
  <actor><name>Darsteller Eins</name><role>Hauptrolle</role></actor>
  <actor><name>Darsteller Zwei</name><role>Nebenrolle</role></actor>
</movie>
X
}

F="$ZIEL/Filme"
S="$ZIEL/Serien"

# --- Filmreihe mit zwei Filmen
d="$F/The Dark Knight Collection/Batman Begins (2005)"
video_spuren "$d/Batman Begins (2005).mkv"
film_nfo "$d/Batman Begins (2005).nfo" "Batman Begins" 2005 8.2 1650000 "FSK 12" \
  "The Dark Knight Collection" "Action" "Bruce Wayne kehrt nach Gotham zurück und wird zu Batman." tt0372784
bild "$d/poster.jpg" 400 600 0x1c2b4a "Batman Begins"
bild "$d/fanart.jpg" 1280 720 0x1c2b4a "Batman Begins"
d="$F/The Dark Knight Collection/The Dark Knight (2008)"
video_spuren "$d/The Dark Knight (2008).mkv"
film_nfo "$d/The Dark Knight (2008).nfo" "The Dark Knight" 2008 9.0 2900000 "FSK 16" \
  "The Dark Knight Collection" "Action" "Batman tritt gegen den Joker an, der Gotham ins Chaos stürzen will." tt0468569
bild "$d/poster.jpg" 400 600 0x2a2a2a "The Dark Knight"
bild "$d/fanart.jpg" 1280 720 0x2a2a2a "The Dark Knight"

# --- Filmreihe mit nur einem Film
d="$F/Matrix (1999)"
video "$d/Matrix (1999).mp4"
film_nfo "$d/Matrix (1999).nfo" "Matrix" 1999 8.7 2100000 "FSK 16" \
  "Matrix Collection" "Science-Fiction" "Ein Hacker erfährt, dass seine Welt eine Simulation ist." tt0133093
bild "$d/poster.jpg" 400 600 0x0b3d1a "Matrix"

# --- Zwei Sprachfassungen als zwei Dateien
d="$F/Avatar (2009)"
SPRACHE=ger video "$d/Avatar (2009) (de).mkv"
SPRACHE=eng video "$d/Avatar (2009) (en).mkv"
film_nfo "$d/movie.nfo" "Avatar – Aufbruch nach Pandora" 2009 7.9 1400000 "FSK 12" \
  "" "Science-Fiction" "Ein querschnittsgelähmter Marine wird auf den Mond Pandora geschickt." tt0499549
bild "$d/poster.jpg" 400 600 0x0d4f6b "Avatar"
bild "$d/fanart.jpg" 1280 720 0x0d4f6b "Avatar"

# --- Tonspuren und Untertitel in der Datei
d="$F/Der Pate (1972)"
video_spuren "$d/Der Pate (1972).mkv"
film_nfo "$d/Der Pate (1972).nfo" "Der Pate" 1972 9.2 2000000 "FSK 16" \
  "" "Drama" "Die Geschichte der Mafia-Familie Corleone um Don Vito und seinen Sohn Michael." tt0068646
bild "$d/poster.jpg" 400 600 0x4a1c1c "Der Pate"

# --- Untertitel als Dateien daneben
d="$F/Inception (2010)"
SPRACHE=eng video "$d/Inception (2010).mp4"
cp "$TMP/de.srt" "$d/Inception (2010).de.srt"
cp "$TMP/en.srt" "$d/Inception (2010).en.srt"
film_nfo "$d/Inception (2010).nfo" "Inception" 2010 8.8 2500000 "FSK 12" \
  "" "Science-Fiction" "Ein Dieb, der Geheimnisse aus Träumen stiehlt, soll einen Gedanken einpflanzen." tt1375666
bild "$d/poster.jpg" 400 600 0x3b3b5c "Inception"
bild "$d/fanart.jpg" 1280 720 0x3b3b5c "Inception"

# --- Weiterschauen (einrichten.py setzt den Fortschritt)
d="$F/Interstellar (2014)"
video "$d/Interstellar (2014).mkv" 150
film_nfo "$d/Interstellar (2014).nfo" "Interstellar" 2014 8.7 2200000 "FSK 12" \
  "" "Science-Fiction" "Eine Gruppe von Forschern reist durch ein Wurmloch, um die Menschheit zu retten." tt0816692
bild "$d/poster.jpg" 400 600 0x2b1f0f "Interstellar"
bild "$d/fanart.jpg" 1280 720 0x2b1f0f "Interstellar"

# --- Faule Tomate, langer Titel
d="$F/Catwoman (2004)"
video "$d/Catwoman (2004).mp4"
film_nfo "$d/Catwoman (2004).nfo" "Catwoman" 2004 3.4 120000 "FSK 12" \
  "" "Action" "Eine Grafikdesignerin wird nach ihrem Tod als Katzenfrau wiedergeboren." tt0327554
d="$F/Die fabelhafte Welt der Amélie (2001)"
video "$d/Die fabelhafte Welt der Amélie (2001).mp4"
film_nfo "$d/Die fabelhafte Welt der Amélie (2001).nfo" "Die fabelhafte Welt der Amélie" 2001 8.3 780000 "FSK 6" \
  "" "Komödie" "Eine junge Kellnerin in Paris beschliesst, das Leben der Menschen um sie herum zu verbessern." tt0211915

# --- Serie mit Specials und zwei Staffeln
bb="$S/Breaking Bad (2008)"
mkdir -p "$bb"
cat > "$bb/tvshow.nfo" <<'X'
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<tvshow>
  <title>Breaking Bad</title>
  <year>2008</year>
  <plot>Ein Chemielehrer mit Krebsdiagnose beginnt, Methamphetamin herzustellen.</plot>
  <genre>Drama</genre>
  <genre>Krimi</genre>
  <mpaa>FSK 16</mpaa>
  <ratings><rating name="imdb" max="10" default="true"><value>9.5</value><votes>2200000</votes></rating></ratings>
  <uniqueid type="imdb">tt0903747</uniqueid>
</tvshow>
X
bild "$bb/poster.jpg" 400 600 0x2f4f1f "Breaking Bad"
bild "$bb/fanart.jpg" 1280 720 0x2f4f1f "Breaking Bad"
folge() { # <ordner> <serie> <s> <e> <titel> [spuren]
  local f="$1/$2 - S$(printf %02d "$3")E$(printf %02d "$4") - $5"
  if [ "${6:-}" = spuren ]; then video_spuren "$f.mkv"; else video "$f.mkv"; fi
  cat > "$f.nfo" <<X
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<episodedetails>
  <title>$5</title>
  <season>$3</season>
  <episode>$4</episode>
  <plot>Folge $4 der Staffel $3.</plot>
  <aired>20$(printf %02d $((8 + $3)))-01-$(printf %02d "$4")</aired>
</episodedetails>
X
}
folge "$bb/Specials" "Breaking Bad" 0 1 "Good Cop Bad Cop"
folge "$bb/Specials" "Breaking Bad" 0 2 "Wedding Day"
folge "$bb/Staffel 01" "Breaking Bad" 1 1 "Der Einstieg" spuren
folge "$bb/Staffel 01" "Breaking Bad" 1 2 "Die Katze ist im Sack" spuren
folge "$bb/Staffel 01" "Breaking Bad" 1 3 "Und der Sack ist im Fluss" spuren
folge "$bb/Staffel 02" "Breaking Bad" 2 1 "Sieben Dreissig Sieben" spuren
folge "$bb/Staffel 02" "Breaking Bad" 2 2 "Grilled" spuren

dk="$S/Dark (2017)"
mkdir -p "$dk"
cat > "$dk/tvshow.nfo" <<'X'
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<tvshow>
  <title>Dark</title>
  <year>2017</year>
  <plot>In der Kleinstadt Winden verschwinden Kinder – und vier Familien entdecken ein Geheimnis, das Generationen umspannt.</plot>
  <genre>Mystery</genre>
  <mpaa>FSK 16</mpaa>
  <ratings><rating name="imdb" max="10" default="true"><value>8.7</value><votes>480000</votes></rating></ratings>
  <uniqueid type="imdb">tt5753856</uniqueid>
</tvshow>
X
bild "$dk/poster.jpg" 400 600 0x1a1a24 "Dark"
folge "$dk/Staffel 01" "Dark" 1 1 "Geheimnisse"
folge "$dk/Staffel 01" "Dark" 1 2 "Lügen"
folge "$dk/Staffel 01" "Dark" 1 3 "Vergangenheit und Gegenwart"
cp "$TMP/de.srt" "$dk/Staffel 01/Dark - S01E01 - Geheimnisse.de.srt"

echo "Testbibliothek in $ZIEL:"
find "$ZIEL" -type f | sed "s|$ZIEL/||" | sort
du -sh "$ZIEL"
