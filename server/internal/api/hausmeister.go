// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lumen/internal/scan"
	"lumen/internal/store"
)

// Der Hausmeister hält die Bibliotheken von selbst aktuell.
//
// Kommen neue Filme oder Folgen in die Ordner, sollen sie von allein
// auftauchen — samt Beschreibung, Poster und Hintergrund. Bisher musste
// jemand auf „Prüfen" drücken, und was er nicht prüfte, blieb unsichtbar.
//
// Jede Bibliothek hat ein Intervall (Vorgabe: täglich um 4 Uhr). Der
// Hausmeister sieht jede Minute nach, was fällig ist, und arbeitet es
// eines nach dem anderen ab — nie zwei Durchläufe gleichzeitig, und nie
// einen zweiten für dieselbe Bibliothek, solange der erste läuft. Das ist
// Rücksicht auf ein NAS mit schwachem Prozessor: ein Durchlauf, dazu
// FFprobe je Datei, ist genug. Läuft dabei eine Wiedergabe, die
// umgewandelt wird, bremst der Durchlauf von selbst (siehe scan.Bremse).

// arbeitMu stellt sicher, dass Durchläufe und Metadaten nacheinander
// laufen — egal, ob der Hausmeister oder ein Knopf sie anstösst.
var arbeitMu sync.Mutex

// wartend: Bibliotheken, deren „Prüfen" angenommen ist, die aber warten,
// bis eine andere fertig ist. Ohne das sah ein Klick auf „Prüfen" aus, als
// passiere nichts — der Durchlauf hing still an arbeitMu.
//
// Je Bibliothek wartet höchstens ein Auftrag (*auftrag). Angemeldet wird
// mit LoadOrStore — Nachsehen und Eintragen in einem Schritt; früher lagen
// Load und Store auseinander, und zwei schnelle Klicks reihten zwei
// Durchläufe ein. Wer drankommt, trägt nur seinen eigenen Auftrag aus
// (CompareAndDelete): Früher löschte der erste den Merker der Bibliothek
// und damit den eines anderen, der noch wartete.
var wartend sync.Map

// inArbeit: Bibliotheken, deren Durchlauf (samt Metadaten und Aufräumen
// danach) gerade läuft. Der Scanner allein weiss nur vom Einlesen.
var inArbeit sync.Map

type auftrag struct{ _ byte } // nur seine Adresse zählt

// anmelden reiht einen Durchlauf für die Bibliothek ein — oder sagt
// false, wenn schon einer wartet.
func anmelden(libraryID string) (*auftrag, bool) {
	a := &auftrag{}
	if _, schon := wartend.LoadOrStore(libraryID, a); schon {
		return nil, false
	}
	return a, true
}

// laeuftOderWartet: Für diese Bibliothek ist schon etwas unterwegs.
func (s *Server) laeuftOderWartet(libraryID string) bool {
	_, w := wartend.Load(libraryID)
	_, l := inArbeit.Load(libraryID)
	return w || l || s.scanner.Running(libraryID)
}

// nachholenMu: Was nur nachgetragen wird (Zeiträume, IMDb, Rotten
// Tomatoes), läuft ausserhalb von arbeitMu und höchstens einmal zugleich.
// Hunderte Abfragen bei TMDb und OMDb sollen keinen Durchlauf aufhalten.
var nachholenMu sync.Mutex

// Durchlauf prüft eine Bibliothek und holt danach die Metadaten. Das ist
// der eine Weg, den „Prüfen", „Anlegen" und der Hausmeister gemeinsam
// nehmen.
//
// Wartet für die Bibliothek schon ein Durchlauf, kommt kein zweiter dazu:
// Der wartende sieht ohnehin alles, was bis zu seinem Beginn dazukommt.
func (s *Server) Durchlauf(ctx context.Context, lib store.Library, vonSelbst bool) {
	if a, ok := anmelden(lib.ID); ok {
		s.durchlaufMitAuftrag(ctx, lib, vonSelbst, a)
	}
}

func (s *Server) durchlaufMitAuftrag(ctx context.Context, lib store.Library, vonSelbst bool, a *auftrag) {
	arbeitMu.Lock()
	inArbeit.Store(lib.ID, true)
	wartend.CompareAndDelete(lib.ID, a)
	// Fährt Lumen gerade herunter, fängt nichts Neues mehr an.
	if ctx.Err() == nil {
		s.durchlaufGesperrt(ctx, lib, vonSelbst)
	}
	inArbeit.Delete(lib.ID)
	arbeitMu.Unlock()
	if ctx.Err() == nil {
		go s.nachholen(ctx)
	}
}

// Nacheinander führt fn unter der Arbeitssperre aus — wie einen
// Durchlauf. Metadaten holen und Filmreihen bilden liefen sonst neben
// einem Durchlauf her: doppelte Anfragen bei TMDb, zwei Downloads ins
// selbe Bild, und der Stand in der Verwaltung zeigte mal den einen, mal
// den anderen.
func (s *Server) Nacheinander(ctx context.Context, fn func(context.Context)) {
	arbeitMu.Lock()
	defer arbeitMu.Unlock()
	if ctx.Err() == nil {
		fn(ctx)
	}
}

// ArbeitAbwarten wartet beim Herunterfahren, bis laufende Durchläufe,
// Metadaten und Nachträge aufgehört haben — höchstens so lange wie
// angegeben. Vorher muss der Kontext dieser Arbeit beendet sein (main),
// sonst wartet es nur auf das natürliche Ende. Danach schliesst main die
// Datenbank; ohne das Warten lief ein Durchlauf noch in eine
// geschlossene Datenbank hinein.
func (s *Server) ArbeitAbwarten(hoechstens time.Duration) bool {
	fertig := make(chan struct{})
	go func() {
		arbeitMu.Lock()
		arbeitMu.Unlock() // nur warten, bis sie frei ist
		nachholenMu.Lock()
		nachholenMu.Unlock()
		close(fertig)
	}()
	select {
	case <-fertig:
		return true
	case <-time.After(hoechstens):
		return false
	}
}

func (s *Server) nachholen(ctx context.Context) {
	if s.meta == nil || !nachholenMu.TryLock() {
		return
	}
	defer nachholenMu.Unlock()
	s.meta.SerienZeitraeumeNachholen(ctx, 200)
	s.meta.SerienWertungenNachholen(ctx, 300)
	s.meta.TomatenNachholen(ctx, 300)
}

func (s *Server) durchlaufGesperrt(ctx context.Context, lib store.Library, vonSelbst bool) {
	if vonSelbst {
		ctx = scan.VonSelbst(ctx)
	}
	if _, err := s.scanner.Scan(ctx, lib); err != nil {
		s.log.Error("scan", "library", lib.Name, "err", err)
		return
	}
	if s.meta != nil && s.meta.Ready() {
		res, err := s.meta.ResolvePending(ctx, lib.ID, 500)
		if err != nil {
			s.log.Error("metadaten", "library", lib.Name, "err", err)
		} else if res != nil && res.Checked >= 500 {
			// Mehr als ein Häppchen wartet (eine grosse Serien-Mediathek hat
			// tausende Folgen): den Rest gleich danach im Hintergrund holen,
			// nicht erst in den nächsten Nächten zu je 500.
			s.metadatenImHintergrund(lib.ID)
		}
	}
	// Leere Doppel von Serien aus älteren Fassungen gehen in
	// der Serie mit den Dateien auf — samt dem Stand der Leute.
	if n, err := s.st.SerienZusammenfuehren(ctx); err == nil && n > 0 {
		s.st.Log(ctx, "info", "scan", fmt.Sprintf("%d doppelte Serien zusammengeführt", n))
	}
	if n, err := s.st.FilmeOhneDatei(ctx); err == nil && n > 0 {
		s.st.Log(ctx, "info", "scan", fmt.Sprintf("%d Filme ohne Datei entfernt", n))
	}
	s.ReihenBilden(ctx)
	// Viele neue Zeilen: Statistik für den Abfrageplaner nachführen
	// (store.Optimieren) — sonst wählt er für manche Abfragen den falschen Index.
	if err := s.st.Optimieren(ctx); err != nil {
		s.log.Warn("statistik", "err", err)
	}
	s.verwaisteBilderWeg(ctx)
	// Neue Dateien: Vorschaubilder und Untertitel vorbereiten (hintergrund.go).
	s.hintergrundAnstossen()
	// Ist ein gewünschter Titel jetzt da, schliesst sich der Wunsch selbst.
	if n, err := s.st.WuenscheErfuellen(ctx); err == nil && n > 0 {
		s.st.Log(ctx, "info", "wuensche", fmt.Sprintf("%d Wunsch/Wünsche von selbst erfüllt", n))
	}
}

// Hausmeister läuft, bis der Zusammenhang endet.
func (s *Server) Hausmeister(ctx context.Context) {
	// Nicht sofort beim Start: erst soll der Server erreichbar sein.
	select {
	case <-time.After(90 * time.Second):
	case <-ctx.Done():
		return
	}
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	// Ging ein Durchlauf schief, ohne dass „zuletzt geprüft" weiterrückte
	// (Fehler beim Lesen, Datenbank gesperrt), ist die Bibliothek eine
	// Minute später wieder fällig. Dann nicht jede Minute neu über die
	// ganze Sammlung, sondern frühestens nach einer Viertelstunde.
	type versuch struct {
		um     time.Time
		vorher *time.Time
	}
	versucht := map[string]versuch{}
	gleicheZeit := func(a, b *time.Time) bool {
		return (a == nil && b == nil) || (a != nil && b != nil && a.Equal(*b))
	}
	for {
		faellig, err := s.st.Faellig(ctx)
		if err == nil {
			for _, lib := range faellig {
				if ctx.Err() != nil {
					return
				}
				if s.laeuftOderWartet(lib.ID) || pfadPruefen(lib.Path) != nil {
					// Fehlt die Platte, wird nicht gewartet, sondern beim
					// nächsten Mal neu geschaut — und die Zeile in der
					// Verwaltung sagt derweil, was los ist.
					continue
				}
				if v, ok := versucht[lib.ID]; ok && gleicheZeit(v.vorher, lib.ScannedAt) &&
					time.Since(v.um) < 15*time.Minute {
					continue
				}
				versucht[lib.ID] = versuch{um: time.Now(), vorher: lib.ScannedAt}
				s.Durchlauf(ctx, lib, true)
			}
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return
		}
	}
}

// verwaisteBilderWeg löscht Standbilder und Vorschau-Clips von Titeln, die
// es nicht mehr gibt. Sonst wächst der Ordner mit jedem gelöschten Film.
func (s *Server) verwaisteBilderWeg(ctx context.Context) int {
	n := 0
	for _, ordner := range []string{s.standbildOrdner(), s.vorschauOrdner()} {
		eintraege, err := os.ReadDir(ordner)
		if err != nil {
			continue
		}
		for _, e := range eintraege {
			if !e.Type().IsRegular() {
				continue
			}
			id, _, _ := strings.Cut(e.Name(), ".")
			if _, err := s.st.Item(ctx, id); errors.Is(err, store.ErrNotFound) {
				if os.Remove(filepath.Join(ordner, e.Name())) == nil {
					n++
				}
			}
		}
	}
	return n
}
