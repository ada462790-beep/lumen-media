// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"lumen/internal/meta"
	"lumen/internal/scan"
	"lumen/internal/store"
)

// Alles, was mit dem Einrichten einer Bibliothek zu tun hat.
//
// Der Anlass für diese Datei war ein Bericht aus dem Betrieb: eine
// Bibliothek liess sich anlegen, blieb aber für immer leer, und löschen
// konnte man sie nicht. Drei Fehler auf einmal — der Pfad wurde nicht
// geprüft, es gab keinen Weg zum Löschen, und tippen musste man ihn
// blind.

// pfadPruefen sagt, ob aus diesem Pfad eine Bibliothek werden kann.
//
// Vorher nahm der Server jeden Pfad an, auch einen leeren. Heraus kam
// eine Bibliothek, die beim Einlesen nichts fand und auch nie etwas
// finden würde — ohne dass irgendwo stand, warum. Lieber gleich sagen,
// was nicht stimmt.
func pfadPruefen(pfad string) error {
	pfad = strings.TrimSpace(pfad)
	if pfad == "" {
		return fmt.Errorf("ohne ordner geht es nicht — trag ein, wo deine filme liegen")
	}
	if !filepath.IsAbs(pfad) {
		return fmt.Errorf("%q ist kein vollständiger pfad. er muss von ganz oben anfangen, "+
			"etwa %s", pfad, beispielPfad())
	}

	st, err := os.Stat(pfad)
	if os.IsNotExist(err) {
		return errors.New(oeffnenFehler(pfad, err))
	}
	if err != nil {
		return errors.New(oeffnenFehler(pfad, err))
	}
	if !st.IsDir() {
		return fmt.Errorf("%q ist eine datei, kein ordner", pfad)
	}

	// Lesen können muss der Server ihn auch — sonst merkt man es erst beim
	// Einlesen, und dort fällt es kaum auf.
	if !lesbar(pfad) {
		return errors.New(keinZugriff(pfad))
	}
	return nil
}

func beispielPfad() string {
	if runtime.GOOS == "windows" {
		return `D:\Filme`
	}
	return "/medien/filme"
}

// ---------------------------------------------------------------- Löschen

// adminDeleteLibrary nimmt eine Bibliothek wieder heraus.
//
// Die Dateien bleiben, wo sie sind — gelöscht wird nur, was Lumen über
// sie weiss. Das ist der Grund, warum hier nichts nachfragt: es geht
// nichts verloren, was sich nicht durch ein erneutes Einlesen
// zurückholen liesse.
func (s *Server) adminDeleteLibrary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	libs, err := s.st.Libraries(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	name := ""
	for _, l := range libs {
		if l.ID == id {
			name = l.Name
		}
	}
	if name == "" {
		fail(w, http.StatusNotFound, "diese bibliothek gibt es nicht")
		return
	}
	if s.scanner.Running(id) {
		fail(w, http.StatusConflict,
			"für diese bibliothek läuft gerade ein durchlauf — warte, bis er fertig ist")
		return
	}

	if err := s.st.DeleteLibrary(r.Context(), id); err != nil {
		failErr(w, err)
		return
	}
	s.st.Log(r.Context(), "info", "library",
		fmt.Sprintf("Bibliothek %q entfernt — die Dateien selbst bleiben liegen", name))
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- Blättern

// ordnerEintrag ist ein Unterordner, in den man hineingehen kann.
type ordnerEintrag struct {
	Name  string `json:"name"`
	Pfad  string `json:"pfad"`
	Leer  bool   `json:"leer"`
	Filme int    `json:"filme"`
	// Gesperrt: der Ordner ist da, aber Lumen darf nicht hinein. Vorher
	// stand dann einfach „keine Videos" — und niemand kam darauf, dass es
	// an den Rechten lag.
	Gesperrt bool `json:"gesperrt,omitempty"`
	// Freigabe: dem Container ausdrücklich freigegeben (ein eingehängter
	// Ordner). Genau die will man im Wähler zuerst sehen.
	Freigabe bool `json:"freigabe,omitempty"`
}

// lesbar: darf Lumen in den Ordner hineinsehen?
func lesbar(pfad string) bool {
	f, err := os.Open(pfad)
	if err != nil {
		return false
	}
	defer f.Close()
	_, err = f.Readdirnames(1)
	return err == nil || err == io.EOF
}

// imContainer: läuft Lumen in Docker oder Podman?
func imContainer() bool {
	for _, p := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// freigaben nennt die Ordner, die in den Container eingehängt sind — das,
// was in der docker-compose.yml unter „volumes" steht. Ein Container sieht
// vom NAS nur diese; darum stehen sie im Wähler ganz oben.
func freigaben(datenOrdner string) []string {
	roh, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil
	}
	system := []string{"/proc", "/sys", "/dev", "/run", "/etc", "/tmp", "/usr", "/lib", "/bin", "/sbin", "/var"}
	daten := filepath.Clean(datenOrdner)
	var out []string
	gesehen := map[string]bool{}
	for _, zeile := range strings.Split(string(roh), "\n") {
		f := strings.Fields(zeile)
		if len(f) < 5 {
			continue
		}
		ziel := strings.ReplaceAll(f[4], "\\040", " ")
		if ziel == "/" || gesehen[ziel] || filepath.Clean(ziel) == daten {
			continue
		}
		systemisch := false
		for _, sys := range system {
			if ziel == sys || strings.HasPrefix(ziel, sys+"/") {
				systemisch = true
				break
			}
		}
		if systemisch {
			continue
		}
		if st, err := os.Stat(ziel); err != nil || !st.IsDir() {
			continue
		}
		gesehen[ziel] = true
		out = append(out, ziel)
	}
	sort.Strings(out)
	return out
}

// keinZugriff erklärt, warum Lumen einen Ordner nicht lesen darf — mit dem
// Benutzer, unter dem es läuft, damit man weiss, wem man Rechte gibt.
func keinZugriff(pfad string) string {
	t := fmt.Sprintf("Lumen darf %q nicht lesen. Es läuft als Benutzer %d (Gruppe %d), "+
		"und dieser hat dort keine Leserechte.", pfad, os.Getuid(), os.Getgid())
	if imContainer() {
		t += " Trag in der docker-compose.yml bei PUID und PGID den NAS-Benutzer ein, " +
			"dem die Filme gehören — per SSH zeigt „id“ seine Zahlen, auf einem Synology meist 1026 und 100."
	}
	return t
}

// adminBrowse zeigt die Unterordner eines Ordners.
//
// Damit lässt sich der Pfad anklicken statt eintippen. Das war der
// eigentliche Grund, warum niemand eine Bibliothek anlegen konnte: man
// musste den Pfad blind kennen und fehlerfrei tippen, und ein Vertipper
// ergab eine Bibliothek, die stumm leer blieb.
//
// Nur der Besitzer kommt hierher, und er darf ohnehin jeden Pfad als
// Bibliothek eintragen — hier zu blättern gibt ihm also nichts, was er
// nicht schon könnte.
// bibliothekAnsicht ist eine Bibliothek, wie der Besitzer sie sieht:
// mit dem Stand eines laufenden Durchlaufs und der Auskunft, ob der
// Ordner überhaupt noch da ist.
//
// Eine eigene Benennung, keine anonyme Struktur im Handler: die Apps
// lesen dieselben Felder, und der Abgleich zwischen App und Server
// braucht einen Namen, an dem er sich festhalten kann.
type bibliothekAnsicht struct {
	store.Library
	Scanning bool `json:"scanning"`
	// Wie weit der Durchlauf ist. „Läuft" allein genügt nicht: bei einer
	// grossen Sammlung dauert das Minuten, und wer nur einen Kreisel
	// sieht, weiss nicht, ob sich etwas aufgehängt hat.
	Stand scan.Fortschritt `json:"stand"`
	// Liegt der Ordner noch da? Eine Platte, die nicht eingehängt ist,
	// sieht sonst aus wie eine leere Bibliothek.
	Erreichbar bool   `json:"erreichbar"`
	Problem    string `json:"problem,omitempty"`
	// Werden gerade Metadaten geholt — für diese Bibliothek oder alle?
	Metadaten *meta.Stand `json:"metadaten,omitempty"`
	// Angenommen, wartet aber auf eine andere Bibliothek.
	Wartet bool `json:"wartet,omitempty"`
}

func (s *Server) adminBrowse(w http.ResponseWriter, r *http.Request) {
	pfad := strings.TrimSpace(r.URL.Query().Get("pfad"))

	if pfad == "" {
		ok(w, map[string]any{
			"pfad":        "",
			"oben":        "",
			"ordner":      startpunkte(s.dataDir),
			"hinweis":     tr(w, "Von hier aus weiterklicken, oder den Pfad oben eintippen."),
			"imContainer": imContainer(),
		})
		return
	}
	pfad = filepath.Clean(pfad)

	eintraege, err := os.ReadDir(pfad)
	if err != nil {
		fail(w, http.StatusBadRequest,
			oeffnenFehler(pfad, err))
		return
	}

	ordner := []ordnerEintrag{}
	for _, e := range eintraege {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		voll := filepath.Join(pfad, e.Name())
		eintrag := ordnerEintrag{Name: e.Name(), Pfad: voll}
		if lesbar(voll) {
			eintrag.Filme = videosDarin(voll)
		} else {
			eintrag.Gesperrt = true
		}
		ordner = append(ordner, eintrag)
	}
	sort.Slice(ordner, func(i, j int) bool {
		return strings.ToLower(ordner[i].Name) < strings.ToLower(ordner[j].Name)
	})
	for i := range ordner {
		ordner[i].Leer = ordner[i].Filme == 0 && !ordner[i].Gesperrt
	}

	oben := filepath.Dir(pfad)
	if oben == pfad {
		oben = ""
	}
	// „hier" aus denselben Zahlen bilden, die eine Zeile tiefer stehen.
	// Würde hier eigens gezählt, käme wegen der Tiefengrenze eine
	// kleinere Zahl heraus als die Summe der Ordner daneben — und zwei
	// Zahlen, die sich widersprechen, sind schlimmer als keine.
	hier := videosDirekt(pfad)
	for _, o := range ordner {
		hier += o.Filme
	}
	ok(w, map[string]any{
		"pfad":   pfad,
		"oben":   oben,
		"ordner": ordner,
		"hier":   hier,
	})
}

// videosDarin zählt grob, wie viele Videodateien hier oder eine Ebene
// tiefer liegen. Das ist kein Einlesen, sondern eine Orientierungshilfe:
// man sieht beim Blättern, wo etwas drin ist.
// videosDarin zählt, wie viele Videodateien unter einem Ordner liegen —
// bis drei Ebenen tief. Drei, weil beides vorkommt und beides stimmen
// soll: „Filme/Die Bucht (2019)/film.mkv" und
// „Serien/Nordlicht/Staffel 01/S01E01.mkv". Nur eine Ebene zu zählen
// liess jeden Serienordner im Wähler als „leer" erscheinen — und genau
// das sollte der Wähler ja verhindern.
//
// Gezählt wird mit Deckel: nach 200 Funden oder 400 besuchten Ordnern
// ist Schluss. Die Zahl soll eine Auskunft sein, keine Inventur, und
// der Wähler soll auch über einer Platte mit 20000 Dateien flott
// bleiben.
// videosDirekt zählt nur, was unmittelbar in diesem Ordner liegt.
func videosDirekt(dir string) int {
	eintraege, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range eintraege {
		if !e.IsDir() && istVideo(e.Name()) {
			n++
		}
	}
	return n
}

func videosDarin(dir string) int {
	const maxFunde, maxOrdner, maxTiefe = 200, 400, 3

	gefunden, besucht := 0, 0

	var gehen func(string, int)
	gehen = func(d string, tiefe int) {
		if gefunden >= maxFunde || besucht >= maxOrdner {
			return
		}
		besucht++
		eintraege, err := os.ReadDir(d)
		if err != nil {
			return
		}
		var tiefer []string
		for _, e := range eintraege {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if e.IsDir() {
				if tiefe < maxTiefe {
					tiefer = append(tiefer, filepath.Join(d, e.Name()))
				}
				continue
			}
			if istVideo(e.Name()) {
				gefunden++
				if gefunden >= maxFunde {
					return
				}
			}
		}
		for _, t := range tiefer {
			gehen(t, tiefe+1)
		}
	}
	gehen(dir, 1)
	return gefunden
}

func istVideo(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mkv", ".mp4", ".m4v", ".avi", ".mov", ".wmv", ".ts", ".m2ts", ".webm", ".mpg", ".mpeg":
		return true
	}
	return false
}

// startpunkte sind die Orte, an denen Filme üblicherweise liegen. Wer
// woanders sucht, tippt den Pfad oben ein.
func startpunkte(datenOrdner string) []ordnerEintrag {
	var kandidaten []string
	if runtime.GOOS == "windows" {
		for _, b := range "CDEFGHIJKLMNOPQRSTUVWXYZ" {
			kandidaten = append(kandidaten, string(b)+`:\`)
		}
	} else {
		kandidaten = []string{"/medien", "/media", "/mnt", "/volume1", "/Volumes",
			"/srv", "/data", "/home", "/Users", "/"}
		if heim, err := os.UserHomeDir(); err == nil {
			kandidaten = append([]string{heim}, kandidaten...)
		}
	}

	out := []ordnerEintrag{}
	gesehen := map[string]bool{}
	// Lumens eigener Datenordner ist kein Ort für Filme.
	gesehen[filepath.Clean(datenOrdner)] = true
	for _, k := range freigaben(datenOrdner) {
		gesehen[k] = true
		e := ordnerEintrag{Name: k, Pfad: k, Freigabe: true}
		if lesbar(k) {
			e.Filme = videosDarin(k)
		} else {
			e.Gesperrt = true
		}
		out = append(out, e)
	}
	for _, k := range kandidaten {
		if gesehen[k] {
			continue
		}
		st, err := os.Stat(k)
		if err != nil || !st.IsDir() {
			continue
		}
		gesehen[k] = true
		e := ordnerEintrag{Name: k, Pfad: k}
		if lesbar(k) {
			e.Filme = videosDarin(k)
		} else {
			e.Gesperrt = true
		}
		out = append(out, e)
	}
	for i := range out {
		out[i].Leer = out[i].Filme == 0 && !out[i].Gesperrt
	}
	return out
}

// ---------------------------------------------------------------- Wünsche

// adminWishes sind die Wünsche des Haushalts für den Besitzer — mit der
// Zahl offener vorneweg, damit die Verwaltung sie anzeigen kann, ohne
// die ganze Liste zu holen.
//
// Die Wünsche gab es schon, nur nicht als eigenen Punkt in der
// Verwaltung: wer etwas wünschte, konnte lange warten, weil es niemand
// sah.
func (s *Server) adminWishes(w http.ResponseWriter, r *http.Request) {
	acc := accountOf(r)
	alle, err := s.st.Wishes(r.Context(), acc.ID, true)
	if err != nil {
		failErr(w, err)
		return
	}
	offen := 0
	for _, w := range alle {
		if w.State == "open" || w.State == "" {
			offen++
		}
	}
	ok(w, map[string]any{"wishes": alle, "offen": offen})
}

func oeffnenFehler(pfad string, err error) string {
	switch {
	case os.IsPermission(err):
		return keinZugriff(pfad)
	case os.IsNotExist(err):
		t := fmt.Sprintf("Den Ordner %q gibt es nicht.", pfad)
		if imContainer() {
			t += " Lumen läuft in einem Container und sieht nur die Ordner, die ihm in der " +
				"docker-compose.yml unter „volumes“ freigegeben sind."
		}
		return t
	}
	return fmt.Sprintf("%q lässt sich nicht öffnen: %v", pfad, err)
}
