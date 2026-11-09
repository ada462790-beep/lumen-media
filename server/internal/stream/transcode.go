// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

// Umwandeln in HLS.
//
// Der Gedanke dahinter: die Abspielliste steht von Anfang an vollständig da,
// weil die Laufzeit bekannt ist. Wird das Bild neu gerechnet, ist jedes
// Segment genau vier Sekunden lang; wird es nur kopiert, beginnt jedes an
// einem Schlüsselbild der Quelle (siehe schluesselbilder.go). So oder so
// steht vorher fest, welches Segment zu welcher Minute gehört. Produziert
// wird aber erst, wenn jemand danach fragt.
//
// Daraus folgt das, was beim Umwandeln sonst fehlt: echtes Spulen. Springt
// jemand in die Mitte, fragt das Gerät nach Segment 450 — und wir starten
// FFmpeg genau dort, statt die halbe Datei bis dahin durchzurechnen.
//
// Damit das aufgeht, muss jedes Segment für sich decodierbar sein und
// genau dort liegen, wo die Liste es verspricht — auch nach einem
// Neustart. Darum:
//
//   - -copyts: die Zeitstempel im Segment sind die der Datei, nicht „ab
//     Neustart bei null". Sonst begänne nach einem Sprung jedes Segment
//     wieder bei 0:00, und der Player sprang zurück oder hing.
//   - Schlüsselbilder beim Umwandeln genau im Raster, absolut gerechnet.
//   - Der Segmentierer schneidet an ausdrücklich genannten Stellen
//     (-segment_times), nicht nach eigenem Gutdünken.
//   - Ein Segment gilt als fertig, sobald es in der Liste steht, die der
//     Segmentierer erst nach dem Schliessen der Datei ergänzt.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SegmentSeconds ist die Länge eines Segments beim Umwandeln. Vier
// Sekunden sind der übliche Kompromiss: kurz genug, dass die Wiedergabe
// schnell anläuft, lang genug, dass nicht ständig nachgefragt wird.
const SegmentSeconds = 4

// readAhead sagt, wie weit FFmpeg vorauslaufen darf, bevor es anhält.
// Ohne Bremse würde ein angetippter Film im Hintergrund komplett
// durchgerechnet — auf einem NAS wäre das die halbe Nacht Volllast, auch
// in der Pause. Die Bremse (bremsen) friert FFmpeg ein, sobald es so weit
// voraus ist, und lässt es weiterlaufen, wenn das Gerät nachrückt.
const readAhead = 30 // Segmente, also etwa zwei Minuten

// neustartAbstand: Liegt das gewünschte Segment mehr als so viele Segmente
// vor dem, was schon fertig ist, setzt FFmpeg dort neu an. Bis dahin
// aufzuholen dauerte länger als ein Neustart — früher wartete das Gerät
// bis zu 45 Sekunden und bekam dann einen Fehler.
const neustartAbstand = 3

// laufLaenge: so viele Segmente schneidet ein Lauf höchstens. Die Grenzen
// stehen in der Befehlszeile, und die ist nicht beliebig lang. Wer weiter
// schaut, bekommt nahtlos einen neuen Lauf.
const laufLaenge = 1500

// zurueckBehalten: so viele Segmente hinter der Abspielstelle bleiben
// liegen (für einen kurzen Sprung zurück). Ältere werden gelöscht, damit
// ein langer Film nicht Gigabytes an Zwischendateien hinterlässt.
const zurueckBehalten = 8

// segmentWarten: so lange wartet eine Anfrage höchstens auf ihr Segment.
const segmentWarten = 60 * time.Second

// MinFrei ist der Platz, der für eine Umwandlung mindestens frei sein muss.
// Läuft die Platte voll, kann auch die Datenbank nicht mehr schreiben.
const MinFrei = 2 << 30

// ErrKeinPlatz: zu wenig Platz für die Zwischendateien.
var ErrKeinPlatz = errors.New("auf dem server ist zu wenig platz für eine umwandlung frei")

// ErrUeberholt: Während diese Anfrage wartete, hat das Gerät woanders
// hingesprungen; ihr Segment wird nicht mehr gerechnet.
var ErrUeberholt = errors.New("das gerät ist inzwischen woanders")

type Transcoder struct {
	ff      *FFmpeg
	workDir string

	// Protokoll nimmt Meldungen für das Protokoll der Verwaltung entgegen —
	// nur Technisches, nie ein Titel.
	Protokoll func(stufe, text string)

	mu     sync.Mutex
	jobs   map[string]*job   // je Wiedergabe höchstens einer
	stufen map[string]int    // wie weit eine Wiedergabe zurückgefallen ist
	inits  map[string][]byte // Kopf der fMP4-Segmente je Wiedergabe
	plaene map[string]Job    // der Auftrag je Wiedergabe, festgelegt beim Master
}

func NewTranscoder(ff *FFmpeg, workDir string) (*Transcoder, error) {
	dir := filepath.Join(workDir, "umwandlung")
	// Reste eines früheren Laufs wegräumen: Segmente sind nichts, was man
	// aufbewahrt, und sie würden sonst die Platte vollschreiben.
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Transcoder{ff: ff, workDir: dir, jobs: map[string]*job{},
		stufen: map[string]int{}, inits: map[string][]byte{}, plaene: map[string]Job{}}, nil
}

// Job beschreibt, was umgewandelt werden soll.
type Job struct {
	SessionID  string
	SourcePath string
	DurationMS int64
	Plan       Plan
	AudioIndex int // -1 = die erste Tonspur
	SubPath    string

	// QuellCodec ist das Bildformat der Datei (für fMP4 und CODECS).
	QuellCodec string
	// TonKopieren: Das Gerät kann den Ton der gewählten Spur selbst
	// (AC3 auf dem Apple TV etwa). Dann bleibt er, wie er ist — samt 5.1.
	TonKopieren bool
	TonCodec    string
	// TonKanaele: so viele Kanäle bekommt gewandelter Ton (0 = 2).
	TonKanaele int
	// Einbrennen: ein Bild-Untertitel (PGS, DVD), der ins Bild gerechnet
	// wird — er lässt sich nicht als Text ausliefern.
	MitEinbrennen bool
	Einbrennen    int
	// Tonemap: HDR für ein Gerät ohne HDR umrechnen.
	Tonemap bool
	// Details sind die genauen Angaben zum Bild (darf nil sein).
	Details *BildDetails
	// Grenzen sind die Segmentanfänge in Sekunden. nil = Raster von
	// SegmentSeconds (beim Umwandeln immer).
	Grenzen []float64
}

// Merken legt den Auftrag einer Wiedergabe fest. Liste und Segmente
// werden über relative Adressen geholt — ohne die Abfrage, mit der das
// Gerät beim Master seine Fähigkeiten nannte. Neu entschieden käme dort
// womöglich etwas anderes heraus (MPEG-TS statt fMP4, Neurechnen statt
// Kopieren), und die Segmente passten nicht zur Liste.
func (t *Transcoder) Merken(j Job) {
	t.mu.Lock()
	t.plaene[j.SessionID] = j
	t.mu.Unlock()
}

// Gemerkt liefert den festgelegten Auftrag einer Wiedergabe.
func (t *Transcoder) Gemerkt(sessionID string) (Job, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	j, ok := t.plaene[sessionID]
	return j, ok
}

// Kopie sagt, ob das Bild unverändert bleibt.
func (j Job) Kopie() bool { return j.Plan.VideoCodec == "copy" }

// FMP4 sagt, ob die Segmente fragmentiertes MP4 statt MPEG-TS sind.
// Apple spielt HEVC über HLS nur so; H.264 bleibt bei TS, das jedes Gerät
// seit jeher kann.
func (j Job) FMP4() bool {
	return j.Kopie() && !strings.EqualFold(j.QuellCodec, "h264") && j.QuellCodec != ""
}

func (j Job) endung() string {
	if j.FMP4() {
		return ".m4s"
	}
	return ".ts"
}

// SegmentGrenzen sind die Segmentanfänge dieser Wiedergabe.
func (j Job) SegmentGrenzen() []float64 {
	if j.Kopie() && len(j.Grenzen) > 0 {
		return j.Grenzen
	}
	return RasterGrenzen(float64(j.DurationMS) / 1000)
}

func (j Job) dauer() float64 { return float64(j.DurationMS) / 1000 }

func (j Job) start() float64 {
	if j.Details != nil {
		return j.Details.Start
	}
	return 0
}

type job struct {
	Job
	grenzen []float64
	dir     string
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	fromSeg int // bei welchem Segment dieser Lauf begonnen hat
	bisSeg  int // exklusiv: bis wohin er schneidet
	stufe   int
	verf    string // Hardware-Verfahren, "" = Software

	mu        sync.Mutex
	lastError string
	finished  bool
	fertigBis int   // höchstes fertiges Segment (fromSeg-1 = noch keins)
	gelesen   int64 // so weit ist die Liste gelesen
	rest      string

	angefragt    int  // das zuletzt angefragte Segment
	geloeschtBis int  // Segmente darunter sind weg
	eingefroren  bool // FFmpeg ist gerade angehalten
	geliefert    bool
}

// ---------------------------------------------------------------- Abspielliste

// Playlist baut die vollständige Liste. Sie ändert sich nicht mehr, egal
// wie weit die Umwandlung ist.
func (t *Transcoder) Playlist(j Job) string {
	g := j.SegmentGrenzen()
	dauern := segmentDauern(g, j.dauer())
	ziel := 1
	for _, d := range dauern {
		if c := int(math.Ceil(d - 0.001)); c > ziel {
			ziel = c
		}
	}
	var b strings.Builder
	if j.FMP4() {
		b.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n")
	} else {
		b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n")
	}
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", ziel)
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-INDEPENDENT-SEGMENTS\n")
	if j.FMP4() {
		b.WriteString("#EXT-X-MAP:URI=\"init.mp4\"\n")
	}
	end := j.endung()
	for i, d := range dauern {
		fmt.Fprintf(&b, "#EXTINF:%.6f,\nseg%05d%s\n", d, i, end)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

// segmentDauern rechnet aus den Anfängen die Längen; das letzte Segment
// reicht bis zum Ende. Ein winziger Rest unter 0,1 s fällt weg.
func segmentDauern(g []float64, dauer float64) []float64 {
	var out []float64
	for i, a := range g {
		e := dauer
		if i+1 < len(g) {
			e = g[i+1]
		}
		if e-a <= 0.1 && i > 0 {
			continue
		}
		out = append(out, e-a)
	}
	if len(out) == 0 {
		out = []float64{math.Max(dauer, 0.1)}
	}
	return out
}

// Master nennt dem Gerät die eine Fassung, die es bekommt. Mehrere Stufen
// wären hier sinnlos: der Server kennt die Leitung des Geräts schon und
// rechnet ohnehin nur eine.
//
// Was hier steht, muss stimmen: AVPlayer wählt danach den Decoder und
// lehnt eine Fassung ab, deren CODECS er nicht kann — früher stand hier
// fest „H.264 High, 16:9, AAC", auch bei kopiertem HEVC mit AC3.
//
// spuren sind fertige EXT-X-MEDIA-Zeilen (Untertitel); sind welche da,
// hängt die Fassung an der Gruppe „subs".
func (t *Transcoder) Master(j Job, mediaURL, spuren string) string {
	d := j.Details
	if d == nil {
		d = &BildDetails{Codec: j.QuellCodec}
	}
	var codecs string
	var breite, hoehe int
	var bw int
	if j.Kopie() {
		codecs = CodecKennung(d.Codec, d.Profil, d.Level)
		if codecs == "" {
			codecs = CodecKennung(j.QuellCodec, "", 0)
		}
		breite, hoehe = d.Breite, d.Hoehe
		if breite == 0 {
			breite, hoehe = AusgabeGroesse(0, 0, j.Plan.Height)
		}
		// Spitzen einer Blu-ray liegen deutlich über dem Durchschnitt.
		bw = j.Plan.Bitrate * 13 / 10
		if bw <= 0 {
			bw = 20_000_000
		}
	} else {
		breite, hoehe = AusgabeGroesse(d.Breite, d.Hoehe, j.Plan.Height)
		codecs = fmt.Sprintf("avc1.6400%02X", H264Level(hoehe, d.FPS))
		bw = ZielBitrate(hoehe, j.Plan.Bitrate) * 3 / 2
		if j.Plan.Bitrate > 0 && bw > j.Plan.Bitrate {
			bw = j.Plan.Bitrate
		}
	}
	ton := "mp4a.40.2"
	tonRate := 160_000
	if j.TonKanaele == 6 {
		tonRate = 384_000
	}
	if j.TonKopieren && j.TonCodec != "" {
		ton = TonKennung(j.TonCodec)
		tonRate = 640_000
	}
	if codecs != "" {
		codecs += "," + ton
	} else {
		codecs = ton
	}
	bw += tonRate

	var attr []string
	attr = append(attr, fmt.Sprintf("BANDWIDTH=%d", bw))
	if breite > 0 && hoehe > 0 {
		attr = append(attr, fmt.Sprintf("RESOLUTION=%dx%d", breite, hoehe))
	}
	attr = append(attr, fmt.Sprintf("CODECS=\"%s\"", codecs))
	if d.FPS > 0 && d.FPS <= 240 {
		attr = append(attr, fmt.Sprintf("FRAME-RATE=%.3f", d.FPS))
	}
	switch {
	case j.Kopie() && d.HLG():
		attr = append(attr, "VIDEO-RANGE=HLG")
	case j.Kopie() && d.HDR():
		attr = append(attr, "VIDEO-RANGE=PQ")
	default:
		attr = append(attr, "VIDEO-RANGE=SDR")
	}
	if spuren != "" {
		attr = append(attr, `SUBTITLES="subs"`)
	}
	version := 3
	if j.FMP4() {
		version = 7
	}
	return fmt.Sprintf("#EXTM3U\n#EXT-X-VERSION:%d\n#EXT-X-INDEPENDENT-SEGMENTS\n%s#EXT-X-STREAM-INF:%s\n%s\n",
		version, spuren, strings.Join(attr, ","), mediaURL)
}

// ---------------------------------------------------------------- Segmente

// ErrGone meldet, dass die Umwandlung abgebrochen wurde.
var ErrGone = errors.New("umwandlung beendet")

// Segment liefert den Pfad zu einem fertigen Segment. Ist es noch nicht da,
// wird gewartet — und wenn es ausserhalb des laufenden Fensters liegt,
// springt die Umwandlung dorthin.
func (t *Transcoder) Segment(ctx context.Context, j Job, index int) (string, error) {
	grenzen := j.SegmentGrenzen()
	if index < 0 || index >= len(segmentDauern(grenzen, j.dauer())) {
		return "", fmt.Errorf("segment %d gibt es nicht", index)
	}
	deadline := time.Now().Add(segmentWarten)
	for versuch := 0; ; versuch++ {
		cur, err := t.sicherstellen(j, index)
		if err != nil {
			return "", err
		}
		pfad, weiter, err := t.warten(ctx, cur, index, deadline)
		if err != nil || !weiter {
			return pfad, err
		}
		if versuch > 4 {
			return "", fmt.Errorf("segment %d kam nicht zustande", index)
		}
	}
}

// sicherstellen sorgt dafür, dass ein Lauf das Segment liefern wird, und
// gibt ihn zurück. Entschieden und gestartet wird unter derselben Sperre:
// fragen zwei Anfragen derselben Wiedergabe gleichzeitig, startet nur eine
// neu, und die andere sieht schon den neuen Lauf.
func (t *Transcoder) sicherstellen(j Job, index int) (*job, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cur, laeuft := t.jobs[j.SessionID]
	if laeuft && !cur.brauchtNeustart(index) {
		return cur, nil
	}
	return t.startGesperrt(j, index)
}

// brauchtNeustart: liefert dieser Lauf das Segment nicht (bald)?
func (cur *job) brauchtNeustart(index int) bool {
	cur.mu.Lock()
	defer cur.mu.Unlock()
	cur.listeLesen()
	if index < cur.fromSeg || index < cur.geloeschtBis || index >= cur.bisSeg {
		return true
	}
	if index <= cur.fertigBis {
		return false // liegt schon da
	}
	if cur.finished {
		return true // vorbei, und das Segment kam nicht
	}
	vorne := cur.fertigBis
	if vorne < cur.fromSeg {
		vorne = cur.fromSeg
	}
	return index > vorne+neustartAbstand
}

// warten wartet auf das Segment. weiter=true heisst: neu entscheiden (der
// Lauf wurde ersetzt oder ist auf die nächste Stufe zurückgefallen).
func (t *Transcoder) warten(ctx context.Context, cur *job, index int, deadline time.Time) (string, bool, error) {
	cur.mu.Lock()
	if index > cur.angefragt || index < cur.angefragt-zurueckBehalten {
		cur.angefragt = index
	}
	cur.mu.Unlock()
	cur.bremsen()

	pfad := segPfad(cur.dir, index, cur.endung())
	for {
		cur.mu.Lock()
		cur.listeLesen()
		fertig := index <= cur.fertigBis && index >= cur.fromSeg
		fehler := cur.lastError
		vorbei := cur.finished
		cur.mu.Unlock()

		if fertig {
			if _, err := os.Stat(pfad); err == nil {
				t.bewaehrt(cur)
				return pfad, false, nil
			}
			return "", true, nil // inzwischen weggeräumt — neu ansetzen
		}
		if fehler != "" {
			if t.zurueckfallen(cur) {
				return "", true, nil
			}
			return "", false, fmt.Errorf("umwandlung fehlgeschlagen: %s", fehler)
		}
		t.mu.Lock()
		aktuell := t.jobs[cur.SessionID] == cur
		t.mu.Unlock()
		if !aktuell {
			// Ein anderer Lauf hat übernommen. Liegt das Segment in seinem
			// Fenster, wird dort gewartet; sonst ist diese Anfrage überholt.
			t.mu.Lock()
			neu, ok := t.jobs[cur.SessionID]
			t.mu.Unlock()
			if ok && !neu.brauchtNeustart(index) {
				return "", true, nil
			}
			return "", false, ErrUeberholt
		}
		if vorbei {
			return "", true, nil
		}
		select {
		case <-ctx.Done():
			return "", false, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return "", false, fmt.Errorf("segment %d kam nicht rechtzeitig", index)
		}
	}
}

// Init liefert den Kopf der fMP4-Segmente (EXT-X-MAP). Er ist für jeden
// Lauf derselben Wiedergabe gleich und wird darum einmal behalten.
func (t *Transcoder) Init(ctx context.Context, j Job) ([]byte, error) {
	if !j.FMP4() {
		return nil, fmt.Errorf("kein fmp4")
	}
	t.mu.Lock()
	b, ok := t.inits[j.SessionID]
	cur, laeuft := t.jobs[j.SessionID]
	t.mu.Unlock()
	if ok {
		return b, nil
	}
	if !laeuft {
		var err error
		if cur, err = t.sicherstellen(j, 0); err != nil {
			return nil, err
		}
	}
	// Der Kopf steht fest, sobald das erste Segment geschrieben wird.
	deadline := time.Now().Add(segmentWarten)
	for time.Now().Before(deadline) {
		cur.mu.Lock()
		cur.listeLesen()
		bereit := cur.fertigBis >= cur.fromSeg || cur.finished
		fehler := cur.lastError
		cur.mu.Unlock()
		if bereit {
			b, err := os.ReadFile(filepath.Join(cur.dir, "init.mp4"))
			if err == nil && len(b) > 0 {
				t.mu.Lock()
				t.inits[j.SessionID] = b
				t.mu.Unlock()
				return b, nil
			}
		}
		if fehler != "" {
			if t.zurueckfallen(cur) {
				var err error
				if cur, err = t.sicherstellen(j, cur.fromSeg); err != nil {
					return nil, err
				}
				continue
			}
			return nil, fmt.Errorf("umwandlung fehlgeschlagen: %s", fehler)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("der kopf der segmente kam nicht rechtzeitig")
}

// zurueckfallen: Ist ein Lauf mit Hardware gescheitert, nimmt die
// Wiedergabe die nächste Stufe (gemischt, dann Software). Gibt false
// zurück, wenn es keine nächste gibt — dann war es nicht die Hardware.
func (t *Transcoder) zurueckfallen(cur *job) bool {
	if cur.verf == "" || cur.stufe >= StufeSoftware || cur.Kopie() {
		return false
	}
	t.mu.Lock()
	if t.stufen[cur.SessionID] <= cur.stufe {
		t.stufen[cur.SessionID] = cur.stufe + 1
		if t.jobs[cur.SessionID] == cur {
			delete(t.jobs, cur.SessionID)
			go cur.aufraeumen()
		}
		t.mu.Unlock()
		// Ins Protokoll nur, wenn ein Verfahren ganz ruht — eine einzelne
		// gescheiterte Umwandlung hinge mit ihrer Uhrzeit an einer Wiedergabe.
		if msg := t.ff.HW.Fehlschlag(cur.verf); msg != "" {
			t.melden("warn", msg)
		}
		return true
	}
	t.mu.Unlock()
	return true
}

// bewaehrt: Ein Lauf mit Hardware hat ein Segment geliefert.
func (t *Transcoder) bewaehrt(cur *job) {
	cur.mu.Lock()
	erstes := !cur.geliefert
	cur.geliefert = true
	cur.mu.Unlock()
	if erstes && cur.verf != "" {
		t.ff.HW.Erfolg(cur.verf)
	}
}

func (t *Transcoder) melden(stufe, text string) {
	if t.Protokoll != nil {
		t.Protokoll(stufe, text)
	}
}

// ---------------------------------------------------------------- FFmpeg

// startGesperrt startet einen neuen Lauf ab fromSeg; t.mu ist gehalten.
func (t *Transcoder) startGesperrt(j Job, fromSeg int) (*job, error) {
	if alt, ok := t.jobs[j.SessionID]; ok {
		delete(t.jobs, j.SessionID)
		go alt.aufraeumen()
	}

	// Jeder Lauf bekommt einen frischen Ordner mit zufälligem Namen. Der
	// Name hängt mit nichts zusammen — nicht mit der Wiedergabe, nicht
	// mit der Kennung in der Verwaltung, nicht mit der Uhrzeit —, damit
	// niemand vom Ordner auf dem NAS zur Verwaltung und zurück schliessen
	// kann. Und weil kein Teil davon aus der Anfrage stammt, kann eine
	// Anfrage auch nicht aus dem Arbeitsordner herausführen.
	if frei := freierPlatz(t.workDir); frei > 0 && frei < MinFrei {
		return nil, ErrKeinPlatz
	}
	dir := filepath.Join(t.workDir, zufall())
	if err := os.Mkdir(dir, 0o700); err != nil {
		return nil, err
	}

	grenzen := j.SegmentGrenzen()
	anzahl := len(segmentDauern(grenzen, j.dauer()))
	bis := fromSeg + laufLaenge
	if bis > anzahl {
		bis = anzahl
	}
	stufe := t.stufen[j.SessionID]
	args, bild := t.args(j, grenzen, fromSeg, bis, dir, stufe)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, t.ff.FFmpegPath, args...)
	stderr := &ringBuffer{limit: 4000}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		cancel()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("ffmpeg startet nicht: %w", err)
	}

	jb := &job{Job: j, grenzen: grenzen, dir: dir, cmd: cmd, cancel: cancel,
		fromSeg: fromSeg, bisSeg: bis, stufe: bild.Stufe, verf: bild.Verfahren,
		fertigBis: fromSeg - 1, angefragt: fromSeg, geloeschtBis: fromSeg}
	if j.Kopie() {
		jb.verf, jb.stufe = "", StufeSoftware
	}
	// Die Bremse sieht jede Sekunde nach, auch wenn das Gerät gerade
	// nichts anfragt (Pause).
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				jb.bremsen()
			}
		}
	}()
	go func() {
		err := cmd.Wait()
		jb.mu.Lock()
		if err != nil && ctx.Err() == nil {
			jb.lastError = Bereinigt(lastLine(stderr.String()))
		}
		jb.finished = true
		jb.mu.Unlock()
	}()

	t.jobs[j.SessionID] = jb
	return jb, nil
}

// args baut den FFmpeg-Aufruf für die Segmente [von, bis). Die
// Reihenfolge ist wichtig: -ss vor -i springt ohne Decodieren an die
// Stelle, danach wäre es langsam.
func (t *Transcoder) args(j Job, grenzen []float64, von, bis int, dir string, stufe int) ([]string, BildBefehl) {
	a := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}

	var bild BildBefehl
	if !j.Kopie() {
		var hoehe int
		if j.Details != nil && j.Plan.Height > 0 && j.Plan.Height < j.Details.Hoehe {
			hoehe = j.Plan.Height
		} else if j.Details == nil {
			hoehe = j.Plan.Height
		}
		auftrag := BildAuftrag{Quelle: j.Details, ZielHoehe: hoehe, Bitrate: j.Plan.Bitrate,
			Tonemap: j.Tonemap, Einbrennen: -1, Stufe: stufe,
			// Schlüsselbilder genau im Raster. t zählt hier ab dem Beginn
			// dieses Laufs (auch mit -copyts), und jeder Lauf beginnt auf
			// einer Grenze — so liegt nach jedem Neustart jede Grenze
			// wieder genau dort, wo die Liste sie verspricht.
			Schluessel: fmt.Sprintf("expr:gte(t,n_forced*%d)", SegmentSeconds),
		}
		if j.MitEinbrennen {
			auftrag.Einbrennen = j.Einbrennen
		}
		bild = t.ff.BildBefehl(auftrag)
		a = append(a, bild.VorEingang...)
	}

	a = append(a, "-copyts")
	start := grenzen[von]
	if start > 0 {
		ss := start
		if j.Kopie() {
			// Beim Kopieren liegt die Grenze genau auf einem Schlüsselbild.
			// Ohne den Hauch Versatz nähme FFmpeg das Schlüsselbild davor.
			ss += seekEpsilon
		}
		a = append(a, "-ss", strconv.FormatFloat(ss, 'f', 3, 64))
	}
	if bis < len(grenzen) {
		// Der Lauf endet genau an einer Grenze; danach übernimmt ein neuer.
		dauer := grenzen[bis] - start
		if j.Kopie() {
			dauer -= seekEpsilon
		}
		a = append(a, "-t", strconv.FormatFloat(dauer, 'f', 3, 64))
	}
	a = append(a, "-i", j.SourcePath)

	if j.Kopie() {
		a = append(a, "-map", "0:v:0")
	} else {
		a = append(a, "-filter_complex", bild.Filter, "-map", "[v]")
	}
	if j.AudioIndex >= 0 {
		a = append(a, "-map", fmt.Sprintf("0:%d", j.AudioIndex))
	} else {
		a = append(a, "-map", "0:a:0?")
	}

	if j.Kopie() {
		a = append(a, "-c:v", "copy")
		if j.FMP4() && strings.EqualFold(j.QuellCodec, "hevc") {
			// Apple will HEVC als „hvc1", sonst bleibt das Bild schwarz.
			a = append(a, "-tag:v", "hvc1")
		}
	} else {
		a = append(a, bild.Kodierer...)
	}

	if j.TonKopieren {
		a = append(a, "-c:a", "copy")
	} else {
		// Sonst AAC: das kann jedes Gerät. Stereo, ausser ein Gerät mit
		// Mehrkanalton bekommt fMP4 (dort lässt sich AC3 nicht kopieren,
		// siehe api/wiedergabe.go) — dann AAC 5.1.
		if j.TonKanaele == 6 {
			a = append(a, "-c:a", "aac", "-b:a", "384k", "-ac", "6")
		} else {
			a = append(a, "-c:a", "aac", "-b:a", "160k", "-ac", "2")
		}
	}
	// Alle Zeitstempel um zehn Sekunden nach hinten: Ein Lauf ab null
	// hätte wegen der B-Frames sonst negative Decodierzeiten, und FFmpeg
	// schöbe dann genau diesen Lauf ein Stück — nach einem Sprung passten
	// die Segmente nicht mehr aneinander. Mit festem Versatz liegt jeder
	// Lauf gleich; dem Player ist der Versatz gleich.
	a = append(a, "-max_muxing_queue_size", "4096", "-output_ts_offset", "10")

	a = append(a, "-f", "segment")
	if j.FMP4() {
		a = append(a, "-segment_format", "mp4",
			// frag_discont + Schnittliste: die Zeitstempel in den
			// Fragmenten bleiben die der Datei (wie bei TS durch -copyts).
			"-segment_format_options", "movflags=+frag_keyframe+empty_moov+default_base_moof+frag_discont:use_editlist=1",
			"-segment_header_filename", filepath.Join(dir, "init.mp4"))
	} else {
		a = append(a, "-segment_format", "mpegts")
	}
	if bis-von > 1 {
		zeiten := make([]string, 0, bis-von-1)
		for i := von + 1; i < bis; i++ {
			zeiten = append(zeiten, strconv.FormatFloat(grenzen[i]-start, 'f', 3, 64))
		}
		a = append(a, "-segment_times", strings.Join(zeiten, ","))
		if j.Kopie() {
			// Die Zählung beginnt beim ersten Paket, nicht genau an der
			// Grenze; ein Hauch Spielraum fängt Rundung und Tonvorlauf ab.
			a = append(a, "-segment_time_delta", "0.05")
		}
	} else {
		a = append(a, "-segment_time", "100000")
	}
	a = append(a,
		"-segment_start_number", strconv.Itoa(von),
		"-segment_list", filepath.Join(dir, "liste.csv"),
		"-segment_list_type", "csv",
		filepath.Join(dir, "seg%05d"+j.endung()),
	)
	return a, bild
}

func (t *Transcoder) Stop(sessionID string) {
	t.mu.Lock()
	jb, ok := t.jobs[sessionID]
	delete(t.jobs, sessionID)
	delete(t.stufen, sessionID)
	delete(t.inits, sessionID)
	delete(t.plaene, sessionID)
	t.mu.Unlock()
	if !ok {
		return
	}
	jb.aufraeumen()
}

func (j *job) aufraeumen() {
	j.cancel()
	// Ein angehaltenes FFmpeg reagiert erst, wenn es weiterläuft.
	j.mu.Lock()
	if j.eingefroren && j.cmd != nil && j.cmd.Process != nil {
		weiter(j.cmd.Process)
		j.eingefroren = false
	}
	j.mu.Unlock()
	_ = os.RemoveAll(j.dir)
}

// StopAll räumt beim Herunterfahren auf.
func (t *Transcoder) StopAll() {
	t.mu.Lock()
	ids := make([]string, 0, len(t.jobs))
	for id := range t.jobs {
		ids = append(ids, id)
	}
	t.mu.Unlock()
	for _, id := range ids {
		t.Stop(id)
	}
	_ = os.RemoveAll(t.workDir)
}

// Active sagt, wie viele Umwandlungen gerade laufen — das ist die Zahl, die
// der Besitzer in der Auslastung sieht.
func (t *Transcoder) Active() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.jobs)
}

// ---------------------------------------------------------------- job

func segPfad(dir string, i int, endung string) string {
	return filepath.Join(dir, fmt.Sprintf("seg%05d%s", i, endung))
}

// listeLesen liest neue Zeilen der Segmentliste. Der Segmentierer schreibt
// eine Zeile erst, wenn das Segment geschlossen ist — darum ist das die
// zuverlässige Auskunft „fertig", ohne auf das nächste Segment zu warten.
// j.mu muss gehalten sein.
func (j *job) listeLesen() {
	f, err := os.Open(filepath.Join(j.dir, "liste.csv"))
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(j.gelesen, 0); err != nil {
		return
	}
	r := bufio.NewReader(f)
	for {
		zeile, err := r.ReadString('\n')
		j.gelesen += int64(len(zeile))
		if err != nil {
			// Unvollständige Zeile: beim nächsten Mal zu Ende lesen.
			j.rest += zeile
			return
		}
		zeile = j.rest + zeile
		j.rest = ""
		name, _, _ := strings.Cut(strings.TrimSpace(zeile), ",")
		if n, err := SegmentIndex(name); err == nil && n > j.fertigBis {
			j.fertigBis = n
		}
	}
}

func (j *job) endung() string { return j.Job.endung() }

// bremsen hält FFmpeg an, wenn es readAhead Segmente voraus ist, lässt es
// weiterlaufen, wenn das Gerät nachgerückt ist, und räumt Segmente weg,
// die weit hinter der Abspielstelle liegen.
func (j *job) bremsen() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.finished || j.cmd == nil || j.cmd.Process == nil {
		return
	}
	j.listeLesen()
	weitVoraus := j.fertigBis >= j.angefragt+readAhead
	switch {
	case weitVoraus && !j.eingefroren:
		if anhalten(j.cmd.Process) {
			j.eingefroren = true
		}
	case !weitVoraus && j.eingefroren:
		if weiter(j.cmd.Process) {
			j.eingefroren = false
		}
	}
	for i := j.geloeschtBis; i < j.angefragt-zurueckBehalten; i++ {
		_ = os.Remove(segPfad(j.dir, i, j.endung()))
		j.geloeschtBis = i + 1
	}
}

// ringBuffer behält nur das Ende der FFmpeg-Ausgabe. Bei einem Fehler steht
// das Wesentliche ohnehin zuletzt, und ein ganzer Lauf wäre megabyteweise.
type ringBuffer struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.limit {
		r.buf = r.buf[len(r.buf)-r.limit:]
	}
	return len(p), nil
}

func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}

// SegmentIndex liest die Nummer aus „seg00042.ts" (oder .m4s).
func SegmentIndex(name string) (int, error) {
	base := filepath.Base(name)
	base = strings.TrimSuffix(strings.TrimSuffix(base, ".ts"), ".m4s")
	if !strings.HasPrefix(base, "seg") {
		return 0, fmt.Errorf("kein segment: %q", name)
	}
	n, err := strconv.Atoi(strings.TrimPrefix(base, "seg"))
	if err != nil || n < 0 || n > 1_000_000 {
		return 0, fmt.Errorf("kein segment: %q", name)
	}
	return n, nil
}

// Aufraeumen beendet Umwandlungen, deren Wiedergabe es nicht mehr gibt —
// beendet in der Verwaltung, unter „Ich → Geräte", durch einen neuen Film
// auf demselben Gerät. Sonst rechnete FFmpeg für niemanden weiter.
func (t *Transcoder) Aufraeumen(laeuft func(sessionID string) bool) {
	t.mu.Lock()
	var weg []string
	for id := range t.jobs {
		if !laeuft(id) {
			weg = append(weg, id)
		}
	}
	for id := range t.plaene {
		if _, ok := t.jobs[id]; !ok && !laeuft(id) {
			delete(t.stufen, id)
			delete(t.inits, id)
			delete(t.plaene, id)
		}
	}
	t.mu.Unlock()
	for _, id := range weg {
		t.Stop(id)
	}
}
