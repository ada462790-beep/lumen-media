// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Lumen — eigener Streaming-Server fürs Heimnetz.
//
// Eine Binärdatei. Starten, im Browser öffnen, Besitzer anlegen, Ordner
// angeben. Alles andere macht der Server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	// Die Zeitzonen stecken im Programm selbst: Feste Uhrzeiten wie „täglich
	// um 4 Uhr" (store.faellig) rechnen in time.Local, und das kommt aus TZ.
	// Fehlt auf dem Rechner die Zeitzonen-Datenbank (Windows, ein schlankes
	// Abbild, manche NAS), fiele Lumen sonst still auf UTC zurück.
	_ "time/tzdata"

	"lumen/internal/api"
	"lumen/internal/meta"
	"lumen/internal/scan"
	"lumen/internal/store"
	"lumen/internal/stream"
)

// version setzt der Bau über -ldflags "-X main.version=…". Eine Konstante
// liesse sich so nicht setzen — darum stand in jeder Fassung „0.1.0",
// und die Verwaltung hätte nie gewusst, ob sie veraltet ist.
var version = "dev"

// Optionen ist, was beim Start eingestellt werden kann. Als Struktur, weil
// eine Funktion mit zehn Zeichenketten hintereinander niemand mehr richtig
// aufruft.
type Optionen struct {
	Addr    string
	DataDir string
	WebDir  string
	FFmpeg  string
	FFprobe string

	// TLSAddr ist der verschlüsselte Zweite Eingang. Im Heimnetz genügt
	// der gewöhnliche; von aussen soll nichts im Klartext laufen.
	TLSAddr string
	TLSCert string
	TLSKey  string
	TLSName string
	TLSAn   bool

	// Fernzugriff über einen Vermittler.
	Vermittler  string
	RelayGeheim string
	RelayFinger string
}

func main() {
	var (
		// 8097 — eins neben dem Anschluss von Emby und Jellyfin. Wer Lumen
		// ausprobiert, hat meist einen davon schon laufen; anfangs lag
		// Lumen auf demselben, und auf dem ersten NAS ging deshalb nichts.
		addr    = flag.String("addr", envOr("LUMEN_ADDR", ":8097"), "Adresse, auf der Lumen hört")
		dataDir = flag.String("data", envOr("LUMEN_DATA", defaultDataDir()), "Ordner für Datenbank und Zwischenspeicher")
		webDir  = flag.String("web", envOr("LUMEN_WEB", ""), "Ordner mit der Web-App (leer = die eingebaute)")
		ffmpeg  = flag.String("ffmpeg", envOr("LUMEN_FFMPEG", "ffmpeg"), "Pfad zu ffmpeg")
		ffprobe = flag.String("ffprobe", envOr("LUMEN_FFPROBE", "ffprobe"), "Pfad zu ffprobe")
		verbose = flag.Bool("v", false, "ausführlich protokollieren")

		tlsAn   = flag.Bool("tls", envOr("LUMEN_TLS", "") != "", "zusätzlich verschlüsselt hören")
		tlsAddr = flag.String("tls-addr", envOr("LUMEN_TLS_ADDR", ":8920"), "Adresse für den verschlüsselten Eingang")
		tlsCert = flag.String("tls-cert", os.Getenv("LUMEN_TLS_CERT"), "Zertifikat (leer = eigenes anlegen)")
		tlsKey  = flag.String("tls-key", os.Getenv("LUMEN_TLS_KEY"), "der zugehörige Schlüssel")
		tlsName = flag.String("tls-name", os.Getenv("LUMEN_TLS_NAME"), "Rechnername fürs eigene Zertifikat")

		vermittler = flag.String("relay", os.Getenv("LUMEN_RELAY"),
			"Vermittler für den Fernzugriff, etwa meinserver.example:8444")
		relayGeheim = flag.String("relay-secret", os.Getenv("LUMEN_RELAY_SECRET"),
			"gemeinsames Geheimnis mit dem Vermittler")
		relayFinger = flag.String("relay-fingerprint", os.Getenv("LUMEN_RELAY_FINGERPRINT"),
			"Fingerabdruck des Vermittler-Zertifikats (gibt der Vermittler beim Start aus)")
	)
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	o := Optionen{
		Addr: *addr, DataDir: *dataDir, WebDir: *webDir,
		FFmpeg: *ffmpeg, FFprobe: *ffprobe,
		TLSAn: *tlsAn, TLSAddr: *tlsAddr, TLSCert: *tlsCert,
		TLSKey: *tlsKey, TLSName: *tlsName,
		Vermittler: *vermittler, RelayGeheim: *relayGeheim, RelayFinger: *relayFinger,
	}
	// Fernzugriff ohne Verschlüsselung gibt es nicht. Durch den Vermittler
	// liefe sonst alles im Klartext — und damit könnte er mitlesen, was
	// jemand schaut. Genau das soll die Bauweise ausschliessen.
	if o.Vermittler != "" {
		o.TLSAn = true
	}

	if err := run(o, log); err != nil {
		log.Error("Lumen konnte nicht starten", "fehler", err)
		os.Exit(1)
	}
}

func run(o Optionen, log *slog.Logger) error {
	addr, dataDir, webDir := o.Addr, o.DataDir, o.WebDir
	ffmpegPath, ffprobePath := o.FFmpeg, o.FFprobe
	zeitzoneMelden(log)
	st, err := store.Open(filepath.Join(dataDir, "lumen.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	ff := stream.NewFFmpeg(ffmpegPath, ffprobePath)
	if !ff.Available() {
		log.Warn("FFmpeg nicht gefunden — Lumen kann dann nur direkt abspielen",
			"gesucht", ffmpegPath)
		st.Log(context.Background(), "warn", "start",
			"FFmpeg nicht gefunden — Umwandeln steht nicht zur Verfügung")
	}

	live := stream.NewLive()
	// Die Zwischendateien beim Umwandeln dürfen woanders liegen als die
	// Datenbank (LUMEN_UMWANDLUNG_DIR) — etwa auf einer schnellen SSD oder
	// im Arbeitsspeicher (tmpfs). Läuft dieser Ordner voll, bleibt die
	// Datenbank heil.
	umwandlungDir := envOr("LUMEN_UMWANDLUNG_DIR", dataDir)
	tr, err := stream.NewTranscoder(ff, umwandlungDir)
	if err != nil {
		return fmt.Errorf("zwischenspeicher fürs umwandeln: %w", err)
	}
	defer tr.StopAll()

	// Die Metadaten-Quellen stehen in der Datenbank: der Besitzer richtet
	// sie einmal in der Verwaltung ein, nicht beim Start.
	ctx := context.Background()
	resolver, err := meta.NewResolver(st, dataDir, meta.Config{
		TMDbKey:  st.Setting(ctx, "meta.tmdb_key", ""),
		OMDbKey:  st.Setting(ctx, "meta.omdb_key", ""),
		Language: st.Setting(ctx, "meta.language", "de-DE"),
		Region:   st.Setting(ctx, "meta.region", "CH"),
		UseIMDb:  st.Setting(ctx, "meta.use_imdb", "1") == "1",

		// Abweichende Adressen nur über die Umgebung — damit sich niemand
		// über die Verwaltung eine fremde Quelle unterschieben lässt.
		TMDbURL:    os.Getenv("LUMEN_TMDB_URL"),
		TMDbImgURL: os.Getenv("LUMEN_TMDB_IMG_URL"),
		OMDbURL:    os.Getenv("LUMEN_OMDB_URL"),
	})
	if err != nil {
		return fmt.Errorf("bilderordner: %w", err)
	}
	if !resolver.Ready() {
		log.Info("Noch keine Metadaten-Quelle eingerichtet — Titel kommen vorerst mit " +
			"ihrem Dateinamen. Den TMDb-Schlüssel trägst du in der Verwaltung unter " +
			"„Poster und Beschreibungen“ ein.")
	}

	scanner := scan.New(st, ff)
	// Lokale Bilder („poster.jpg" neben dem Film) landen bei den anderen.
	scanner.BilderOrdner = resolver.ArtDir()
	srv := api.New(st, scanner, ff, tr, live, resolver, dataDir, log, version)
	srv.SetUmwandlungsOrdner(umwandlungDir)
	// grund endet beim Herunterfahren: Hausmeister, Durchläufe, Metadaten
	// und Downloads hören dann auf, bevor die Datenbank geschlossen wird.
	grund, allesBeenden := context.WithCancel(context.Background())
	defer allesBeenden()
	srv.SetzeGrund(grund)
	// Umwandlungen vom letzten Lauf wegräumen (kurzlebig.go).
	srv.KurzlebigAufraeumen()
	// Hardware-Beschleunigung erkennen, Vorschaubilder und Untertitel im
	// Hintergrund vorbereiten (umwandlung_einstellungen.go).
	srv.UmwandlungEinrichten(grund)
	// Filmreihen einmal beim Start bilden. Wer von einer älteren Fassung
	// kommt, hat sonst keine, bis er das nächste Mal eine Bibliothek prüft.
	// Unter der Arbeitssperre — nicht neben einem Durchlauf her.
	go srv.Nacheinander(grund, srv.ReihenBilden)
	// Und ab dann hält der Hausmeister die Bibliotheken aktuell.
	go srv.Hausmeister(grund)
	// Fehlende Untertitel holen, wenn die Verwaltung es eingeschaltet hat.
	go srv.UntertitelWaechter(grund)
	// Und sieht nach, ob es Lumen selbst in neuer Fassung gibt.
	go srv.UpdateWaechter(grund)
	// Und sichert einmal am Tag die Datenbank (zwei Generationen).
	go srv.SicherungsWaechter(grund)

	mux := http.NewServeMux()
	mux.Handle("/api/", srv.Handler())
	mux.Handle("/", webApp(webDir))
	if webDir != "" {
		log.Info("Web-App aus einem Ordner", "ordner", webDir)
	}

	http.DefaultServeMux = nil // niemand soll versehentlich daran hängen

	httpSrv := &http.Server{
		Addr:    addr,
		Handler: logging(log, mux),
		// Grosszügig, weil Wiedergaben stundenlang laufen. Für das Lesen
		// der Anfrage reicht eine knappe Frist.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Aufräumen im Hintergrund: altes Protokoll wegwerfen, verwaiste
	// Wiedergaben beenden.
	stop := make(chan struct{})
	go housekeeping(st, stop)
	go verwaisteAufraeumen(live, tr, st, stop)

	errCh := make(chan error, 1)
	go func() {
		log.Info("Lumen läuft", "version", version, "adresse", addr,
			"daten", dataDir, "erreichbar", guessURL(addr))
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// Der verschlüsselte Eingang — und, wenn eingerichtet, der Fernzugriff.
	if o.TLSName != "" {
		srv.SetzeErlaubteHosts(o.TLSName)
	}
	tlsSrv, eigenerFingerabdruck, tlsConf, err := verschluesseltHoeren(o, logging(log, mux), log, errCh)
	if err != nil {
		return err
	}
	if tlsSrv != nil {
		defer func() {
			zu, ab := context.WithTimeout(context.Background(), 5*time.Second)
			defer ab()
			_ = tlsSrv.Shutdown(zu)
		}()
	}

	fernCtx, fernAus := context.WithCancel(context.Background())
	defer fernAus()
	if o.Vermittler != "" {
		h, err := fernzugriffStarten(fernCtx, o, srv, eigenerFingerabdruck, tlsConf, logging(log, mux), log)
		if err != nil {
			return err
		}
		_ = h
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case <-sig:
		log.Info("Lumen hört auf — laufende Wiedergaben bekommen noch Zeit")
	}

	close(stop)
	// Arbeit im Hintergrund abbrechen und abwarten, während laufende
	// Wiedergaben noch ihre Zeit bekommen. Ein abgebrochener Durchlauf
	// vermerkt nichts als fehlend (scan.Scan), ein abgebrochener Download
	// hinterlässt kein halbes Bild.
	allesBeenden()
	abgewartet := make(chan bool, 1)
	go func() { abgewartet <- srv.ArbeitAbwarten(15 * time.Second) }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = httpSrv.Shutdown(ctx)
	if !<-abgewartet {
		log.Warn("Arbeit im Hintergrund hörte nicht rechtzeitig auf — die Datenbank wird trotzdem geschlossen")
	}
	return err
}

// zeitzoneMelden sagt beim Start, in welcher Zeitzone feste Uhrzeiten
// gelten. Steht TZ auf etwas, das Go nicht kennt, nimmt Go still UTC — und
// „täglich um 4 Uhr" liefe in Zürich um 5 oder 6 Uhr.
func zeitzoneMelden(log *slog.Logger) {
	zone := time.Local.String()
	if tz := os.Getenv("TZ"); tz != "" && tz != "UTC" && zone == "UTC" {
		log.Warn("Zeitzone aus TZ unbekannt — feste Uhrzeiten gelten in UTC", "TZ", tz)
		return
	}
	log.Info("Zeitzone für feste Uhrzeiten", "zone", zone, "jetzt", time.Now().Format("15:04 MST -07:00"))
}

func logging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		// Nur der Bereich der Anfrage, keine Kennungen: Aus „/items/<id>",
		// „/art/<id>" oder „/stream/<datei>" liesse sich sonst ablesen,
		// wer wann was angesehen hat. Die Abfrage (?token=…) bleibt ganz
		// draussen.
		log.Debug("anfrage", "methode", r.Method, "pfad", pfadOhneKennung(r.URL.Path),
			"status", rec.status, "dauer", time.Since(start).Round(time.Millisecond))
	})
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// verwaisteFrist: so lange darf eine umgewandelte Wiedergabe schweigen,
// bevor sie als beendet gilt. Ein Gerät holt alle vier Sekunden ein
// Segment; zwei Minuten sind also reichlich Spielraum für einen Lift, einen
// Tunnel oder einen kurzen Netzwechsel — und kurz genug, dass niemand lange
// auf seinem eigenen Platz sitzen bleibt, der längst leer ist.
const verwaisteFrist = 2 * time.Minute

// verwaisteFristDirekt: Eine Direktwiedergabe (oder ein Browser-Strom)
// lebt, solange eine ihrer Anfragen offen ist. Ist keine mehr offen, ist
// sie nach einer halben Minute vorbei — wer weiterspielt, schickt die
// nächste Bereichsanfrage und bekommt nahtlos eine neue.
const verwaisteFristDirekt = 30 * time.Second

// verwaisteAufraeumen beendet Wiedergaben, von denen nichts mehr kommt.
//
// Ohne das bliebe jede Wiedergabe stehen, die nicht sauber abgemeldet
// wurde — ein Handy ohne Netz, ein zugeklapptes Fenster, ein leerer Akku.
// Die Folge wäre nicht nur eine falsche Anzeige beim Besitzer: die Grenze
// „gleichzeitig schauen" bliebe für diese Person dauerhaft belegt, und sie
// käme an ihre eigenen Filme nicht mehr heran.
func verwaisteAufraeumen(live *stream.Live, tr *stream.Transcoder,
	st *store.Store, stop <-chan struct{}) {

	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if tr != nil {
				tr.Aufraeumen(live.Laeuft)
			}
			for _, id := range live.VerwaisteMit(verwaisteFrist, verwaisteFristDirekt) {
				live.EndMit(id, stream.GrundAbgelaufen)
				if tr != nil {
					tr.Stop(id)
				}
				// Ins Protokoll kommt, dass etwas aufgeräumt wurde — nicht,
				// was lief und auch nicht, wer es war.
				st.Log(context.Background(), "info", "stream",
					"Eine Wiedergabe hat sich nicht mehr gemeldet und wurde beendet")
			}
		}
	}
}

func housekeeping(st *store.Store, stop <-chan struct{}) {
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			_ = st.TrimLog(context.Background(), 14*24*time.Hour)
			// Ein Gerät, das ein halbes Jahr nicht da war, ist meist
			// verloren, verkauft oder ersetzt. Seine Anmeldung gilt dann
			// nicht mehr.
			_ = st.AlteGeraeteAbmelden(context.Background(), 180*24*time.Hour)
		}
	}
}

// ---------------------------------------------------------------- Hilfen

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "lumen")
	}
	return "./lumen-data"
}

// guessURL sagt beim Start, wo man Lumen im Heimnetz erreicht — sonst sucht
// man die Adresse des NAS zusammen.
func guessURL(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		port = "8097"
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return "http://localhost:" + port
	}
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				return "http://" + ipnet.IP.String() + ":" + port
			}
		}
	}
	return "http://localhost:" + port
}

// pfadOhneKennung kürzt einen Pfad auf seine ersten drei Teile:
// „/api/v1/items/it_123/children" → „/api/v1/items/…".
func pfadOhneKennung(p string) string {
	teile := strings.SplitN(strings.TrimPrefix(p, "/"), "/", 4)
	if len(teile) <= 3 {
		return p
	}
	return "/" + strings.Join(teile[:3], "/") + "/…"
}
