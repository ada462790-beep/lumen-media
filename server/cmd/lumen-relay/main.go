// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// lumen-relay macht einen Lumen-Server von aussen erreichbar, ohne dass im
// Router ein Port offen stehen muss.
//
// Das Programm läuft nicht zu Hause, sondern auf irgendeiner Maschine mit
// öffentlicher Adresse — ein kleiner Mietserver genügt, der billigste tut
// es. Es braucht weder Datenbank noch FFmpeg noch Platz: eine Binärdatei
// von wenigen Megabyte, die Bytes durchreicht.
//
//	lumen-relay --geheimnis "$(head -c 32 /dev/urandom | base64)"
//
// Beim Start gibt es den Fingerabdruck seines Zertifikats aus. Der kommt
// zusammen mit dem Geheimnis in die Einstellungen des Heimservers, und
// damit steht die Sache.
//
// # Was dieses Programm nicht kann
//
// Hineinsehen. Es bekommt verschlüsselte Bytes und gibt verschlüsselte
// Bytes weiter; der Schlüssel liegt zu Hause. Wer den Vermittler betreibt,
// sieht, dass jemand da war und wie viele Bytes es waren — nicht wer, und
// erst recht nicht was. Das ist dieselbe Regel, nach der auch der Besitzer
// des Heimservers nicht sieht, wer was schaut.
package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	// Die Zeitzonen kommen mit in die Binärdatei. Der Vermittler läuft in
	// einem leeren Abbild — kein Betriebssystem, keine Dateien ausser
	// seinem Zertifikat. Hier stand erst ein COPY aus dem Bau-Abbild,
	// nur führt Alpine das Zeitzonenverzeichnis gar nicht mit, und das
	// Bauen scheiterte an einer Datei, die es nie gab. So trägt das
	// Programm sie selbst bei sich; kostet knapp ein halbes Megabyte und
	// kann nicht fehlen.
	_ "time/tzdata"

	"lumen/internal/cert"
	"lumen/internal/relay"
)

var version = "0.1.0"

func main() {
	if err := lauf(); err != nil {
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
}

func lauf() error {
	// Die Grenzen dürfen auch aus der Umgebung kommen, wie alles andere.
	// Ein unlesbarer Wert ist ein Fehler, keine stille Vorgabe: wer eine
	// Grenze setzt, will sie auch haben.
	vorgabeBesucher, err := envInt("LUMEN_RELAY_MAX_VISITORS", relay.VorgabeMaxBesucher)
	if err != nil {
		return err
	}
	vorgabeJeAdresse, err := envInt("LUMEN_RELAY_MAX_PER_IP", relay.VorgabeMaxJeAdresse)
	if err != nil {
		return err
	}

	var (
		besucherAddr = flag.String("addr", envOr("LUMEN_RELAY_ADDR", ":8443"),
			"Adresse für Besucher — dorthin zeigt die Adresse, die du weitergibst")
		tunnelAddr = flag.String("tunnel", envOr("LUMEN_RELAY_TUNNEL", ":8444"),
			"Adresse, an der sich der Heimserver meldet")
		geheimnis = flag.String("geheimnis", os.Getenv("LUMEN_RELAY_SECRET"),
			"gemeinsames Geheimnis — dasselbe wie im Heimserver")
		datenDir = flag.String("data", envOr("LUMEN_RELAY_DATA", "."),
			"Ordner für das Zertifikat")
		certPfad = flag.String("cert", os.Getenv("LUMEN_RELAY_CERT"),
			"Zertifikat für den Tunnelport (leer = eigenes anlegen)")
		keyPfad = flag.String("key", os.Getenv("LUMEN_RELAY_KEY"),
			"der zugehörige Schlüssel")
		statusAddr = flag.String("status", os.Getenv("LUMEN_RELAY_STATUS"),
			"Adresse für eine kleine Statusseite (leer = aus)")
		name = flag.String("name", envOr("LUMEN_RELAY_NAME", ""),
			"Rechnername für das eigene Zertifikat")
		maxBesucher = flag.Int("max-besucher", vorgabeBesucher,
			"höchstens so viele Besucherverbindungen gleichzeitig")
		maxJeAdresse = flag.Int("max-je-adresse", vorgabeJeAdresse,
			"davon höchstens so viele von derselben Adresse (IPv6: je /64)")
		zeigVersion = flag.Bool("version", false, "Fassung ausgeben")
	)
	flag.Parse()

	if *zeigVersion {
		fmt.Println("lumen-relay", version)
		return nil
	}
	if *geheimnis == "" {
		return errors.New("ohne --geheimnis geht es nicht.\n\n" +
			"  Eines erzeugen:  head -c 32 /dev/urandom | base64\n" +
			"  Dasselbe gehört in die Einstellungen des Heimservers.")
	}
	if len(*geheimnis) < 16 {
		return errors.New("das Geheimnis ist zu kurz — mindestens 16 Zeichen.\n" +
			"Es ist das Einzige, was zwischen deinem Heimnetz und dem Rest\n" +
			"der Welt steht.")
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	namen := []string{"localhost", "127.0.0.1"}
	if *name != "" {
		namen = append([]string{*name}, namen...)
	}
	zert, woher, err := cert.Laden(*certPfad, *keyPfad, *datenDir, namen)
	if err != nil {
		return fmt.Errorf("zertifikat: %w", err)
	}

	if *maxBesucher < 1 || *maxJeAdresse < 1 {
		return errors.New("--max-besucher und --max-je-adresse müssen mindestens 1 sein")
	}

	v, err := relay.NeuerVermittler(*geheimnis, log)
	if err != nil {
		return err
	}
	v.MaxBesucher = *maxBesucher
	v.MaxJeAdresse = *maxJeAdresse

	tunnel, err := tls.Listen("tcp", *tunnelAddr, relay.TLSAufbau(zert))
	if err != nil {
		return fmt.Errorf("tunnelport %s: %w", *tunnelAddr, err)
	}
	besucher, err := net.Listen("tcp", *besucherAddr)
	if err != nil {
		_ = tunnel.Close()
		return fmt.Errorf("besucherport %s: %w", *besucherAddr, err)
	}

	fp := cert.Fingerabdruck(zert)
	fmt.Println()
	fmt.Println("  lumen-relay", version, "läuft.")
	fmt.Println()
	fmt.Println("  Besucher          ", *besucherAddr)
	fmt.Println("  Heimserver meldet sich an", *tunnelAddr)
	if woher.NeuAngelegt {
		fmt.Println()
		fmt.Println("  Eigenes Zertifikat angelegt in", filepath.Clean(*datenDir))
		fmt.Println("  Fingerabdruck:")
		fmt.Println("   ", cert.Lesbar(fp))
		fmt.Println()
		fmt.Println("  Dieser Fingerabdruck gehört in die Einstellungen des")
		fmt.Println("  Heimservers. Er sorgt dafür, dass sich kein anderer")
		fmt.Println("  zwischen die beiden schieben kann.")
	} else {
		fmt.Println("  Fingerabdruck:", cert.Lesbar(fp))
	}
	fmt.Println()

	stop := make(chan struct{})
	if *statusAddr != "" {
		go statusseite(*statusAddr, v, log)
	}
	go v.Lauschen(tunnel, besucher, stop)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Info("lumen-relay hört auf")
	close(stop)
	time.Sleep(200 * time.Millisecond)
	return nil
}

// statusseite zeigt, ob der Heimserver verbunden ist. Mehr steht dort
// nicht — es gibt auch nicht mehr zu wissen.
func statusseite(addr string, v *relay.Vermittler, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		z := v.Stand()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		stand := "nicht verbunden"
		if z.Verbunden {
			stand = "verbunden seit " + z.Seit.Format("02.01.2006 15:04")
		}
		fmt.Fprintf(w, "lumen-relay %s\n\nHeimserver: %s\nBesuche: %d (davon %d offen)\n"+
			"Abgewiesen, weil alles belegt war: %d\n"+
			"Durchgereicht: %s hin, %s zurück\n\n"+
			"Was jemand geschaut hat, steht hier nicht — der Vermittler\n"+
			"weiss es nicht.\n",
			version, stand, z.Besuche, z.Offen, z.Abgewiesen,
			menge(z.BytesHin), menge(z.BytesZurueck))
	})
	// Die Statusseite kann ebenso offen im Netz stehen wie der Besucherport.
	// Darum alle Fristen, nicht nur die für die Kopfzeilen: sonst hielte
	// jemand mit stehen gelassenen Keep-alive-Verbindungen beliebig viele
	// Goroutinen fest.
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("Statusseite", "grund", err)
	}
}

func menge(b int64) string {
	const k = 1024
	if b < k {
		return strconv.FormatInt(b, 10) + " B"
	}
	d, e := int64(k), 0
	for n := b / k; n >= k; n /= k {
		d *= k
		e++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(d), "KMGTP"[e])
}

func envInt(key string, def int) (int, error) {
	s := os.Getenv(key)
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s=%q ist keine Zahl ab 1", key, s)
	}
	return n, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
