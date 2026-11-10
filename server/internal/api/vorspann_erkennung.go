// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Vorspann und Abspann automatisch erkennen — als Teil der Arbeit im
// Hintergrund (hintergrund.go), mit denselben Regeln:
//
//   - eine Datei zugleich, FFmpeg mit niedrigster Rechen- und
//     Plattenpriorität und einem Faden (vorspann.AusDatei);
//   - Wiedergabe hat Vorrang: Läuft eine Umwandlung, wird vor jedem
//     Lesen gewartet;
//   - feste Reihenfolge (Serien mit den neuesten Dateien zuerst), nie
//     „die Folge, die gerade jemand schaut" — sonst verriete die
//     Reihenfolge der Ergebnisse, was jemand schaut;
//   - jede Datei wird einmal untersucht; das Ergebnis steht in der
//     Datenbank (store/vorspann_erkennung.go) und gilt, bis sich die
//     Datei ändert. Beim Herunterfahren bricht der Durchgang ab und macht
//     beim nächsten Mal dort weiter.
//
// Je Folge werden zwei Stücke gelesen: die ersten zehn Minuten und die
// letzten fünf (bei kurzen Folgen weniger). Verglichen wird mit bis zu
// vier Nachbarfolgen derselben Staffel; hat die Staffel zu wenige, mit den
// nächsten Folgen der Serie. Die Abdrücke der Nachbarn bleiben für die
// nächste Folge im Speicher — wenige, je zwei kleine Listen von Zahlen.

import (
	"context"
	"errors"
	"time"

	"lumen/internal/store"
	"lumen/internal/stream"
	"lumen/internal/vorspann"
)

const einstErkennung = "umwandlung.vorspann_erkennen"

func (s *Server) erkennungAn(ctx context.Context) bool {
	return s.st.Setting(ctx, einstErkennung, "1") == "1"
}

// Wie viele Nachbarn je Folge, wie viele Abdrücke im Speicher.
const (
	erkennungPartner   = 4
	erkennungVorrat    = 16
	erkennungJeAbfrage = 20
)

// erkennungDurchgang untersucht, was noch offen ist — höchstens etwa so
// lange wie angegeben (0 = bis alles fertig ist). Die Arbeit im
// Hintergrund ruft ihn zwischen den Vorschaubildern immer wieder auf:
// So wartet die Erkennung (Sekunden je Folge) nach einem grossen Scan
// nicht tagelang auf die Vorschaubilder (Minuten je Datei), und
// umgekehrt.
func (s *Server) erkennungDurchgang(ctx context.Context, hoechstens time.Duration) {
	if !s.erkennungAn(ctx) || s.ff == nil || !s.ff.Available() {
		return
	}
	var bis time.Time
	if hoechstens > 0 {
		bis = time.Now().Add(hoechstens)
	}
	gesehen := map[string]bool{}
	untersucht := 0
	defer func() {
		if untersucht > 0 && ctx.Err() == nil {
			s.log.Info("Vorspann-Erkennung", "folgen", untersucht)
		}
	}()
	for {
		serien, err := s.st.ErkennungOffeneSerien(ctx, vorspann.Fassung, erkennungJeAbfrage)
		if err != nil || len(serien) == 0 {
			return
		}
		neu := false
		for _, id := range serien {
			if gesehen[id] {
				continue
			}
			gesehen[id], neu = true, true
			n, weiter := s.erkennungSerie(ctx, id, bis)
			untersucht += n
			if !weiter {
				return
			}
		}
		// Kam nur, was in diesem Durchgang schon dran war (etwa weil eine
		// Folge gerade nicht zu lesen ist), ist Schluss bis zum nächsten.
		if !neu {
			return
		}
	}
}

// erkennungWarten: Wiedergabe hat Vorrang. false, wenn Lumen herunterfährt.
func (s *Server) erkennungWarten(ctx context.Context) bool {
	for s.live != nil && s.live.Load().Transcodes > 0 {
		select {
		case <-time.After(30 * time.Second):
		case <-ctx.Done():
			return false
		}
	}
	return ctx.Err() == nil
}

// abdruecke einer Datei: Anfang und Ende.
type abdruecke struct {
	anfang, ende *vorspann.Abdruck
	err          error
}

// erkennungSerie untersucht die offenen Folgen einer Serie. Gibt zurück,
// wie viele es waren, und ob weitergemacht werden soll (false: Lumen
// fährt herunter oder die Zeit bis „bis" ist um).
func (s *Server) erkennungSerie(ctx context.Context, serieID string, bis time.Time) (int, bool) {
	dateien, err := s.st.ErkennungSerie(ctx, serieID, vorspann.Fassung)
	if err != nil || len(dateien) == 0 {
		return 0, ctx.Err() == nil
	}
	folgenZahl, err := s.st.FolgenDerSerie(ctx, serieID)
	if err != nil {
		return 0, ctx.Err() == nil
	}
	// Je Folge eine Datei als Vergleich (die bevorzugte Fassung steht
	// vorn); untersucht wird jede offene Datei.
	var vertreter []store.ErkennungsDatei
	stelle := map[string]int{} // Folge → Stelle in vertreter
	for _, d := range dateien {
		if _, da := stelle[d.ItemID]; !da {
			stelle[d.ItemID] = len(vertreter)
			vertreter = append(vertreter, d)
		}
	}
	vorrat := map[string]*abdruecke{}
	var reihe []string // älteste zuerst, für das Verdrängen
	holen := func(d store.ErkennungsDatei) (*abdruecke, bool) {
		if a, da := vorrat[d.FileID]; da {
			return a, true
		}
		if !s.erkennungWarten(ctx) {
			return nil, false
		}
		a := &abdruecke{}
		dauer := float64(d.DurationMS) / 1000
		ab, laenge := vorspann.AnfangFenster(dauer)
		a.anfang, a.err = vorspann.AusDatei(ctx, s.ff.FFmpegPath, d.Path, ab, laenge)
		if a.err == nil {
			if !s.erkennungWarten(ctx) {
				return nil, false
			}
			ab, laenge = vorspann.EndeFenster(dauer)
			a.ende, a.err = vorspann.AusDatei(ctx, s.ff.FFmpegPath, d.Path, ab, laenge)
		}
		if ctx.Err() != nil {
			return nil, false
		}
		if len(reihe) >= erkennungVorrat {
			delete(vorrat, reihe[0])
			reihe = reihe[1:]
		}
		vorrat[d.FileID], reihe = a, append(reihe, d.FileID)
		return a, true
	}

	u := vorspann.NeuerUntersucher()
	n := 0
	for _, ziel := range dateien {
		if !ziel.Offen {
			continue
		}
		if !bis.IsZero() && time.Now().After(bis) {
			return n, false
		}
		erg := store.Erkennung{FileID: ziel.FileID, Groesse: ziel.Groesse, MTime: ziel.MTime,
			Fassung: vorspann.Fassung, Folgen: folgenZahl}
		partner := erkennungNachbarn(vertreter, stelle[ziel.ItemID], ziel.StaffelID)
		if len(partner) == 0 {
			erg.Stand = store.ErkanntEinsam
			if err := s.st.ErkennungSetzen(ctx, erg); err != nil {
				return n, ctx.Err() == nil
			}
			continue
		}
		a, weiter := holen(ziel)
		if !weiter {
			return n, false
		}
		if a.err != nil {
			erg.Stand = store.ErkanntFehler
			if errors.Is(a.err, vorspann.ErrOhneTon) {
				erg.Stand = store.ErkanntOhneTon
			} else {
				// Ins Protokoll: dass es nicht ging, nicht bei welcher Folge.
				s.st.Log(ctx, "warn", "vorbereiten",
					"Vorspann-Erkennung: eine Datei liess sich nicht lesen — "+stream.Bereinigt(a.err.Error()))
			}
			if err := s.st.ErkennungSetzen(ctx, erg); err != nil {
				return n, ctx.Err() == nil
			}
			n++
			continue
		}
		zielFolge := &vorspann.Folge{Schluessel: ziel.FileID, Dauer: float64(ziel.DurationMS) / 1000,
			Anfang: a.anfang, Ende: a.ende}
		var folgen []*vorspann.Folge
		for _, p := range partner {
			pa, weiter := holen(p)
			if !weiter {
				return n, false
			}
			if pa.err != nil {
				continue
			}
			folgen = append(folgen, &vorspann.Folge{Schluessel: p.FileID, Dauer: float64(p.DurationMS) / 1000,
				Anfang: pa.anfang, Ende: pa.ende})
		}
		if len(folgen) == 0 {
			erg.Stand = store.ErkanntEinsam
		} else {
			b := u.Untersuchen(zielFolge, folgen)
			erg.Stand, erg.Partner = store.ErkanntFertig, b.Partner
			if v := b.Vorspann; v != nil {
				erg.VorspannVonMS, erg.VorspannBisMS = int64(v.Von*1000), int64(v.Bis*1000)
				erg.VorspannSich = max(v.Sicherheit, 0.001)
			}
			if e := b.Abspann; e != nil {
				erg.AbspannAbMS, erg.AbspannSich = int64(e.Von*1000), max(e.Sicherheit, 0.001)
			}
		}
		if err := s.st.ErkennungSetzen(ctx, erg); err != nil {
			return n, ctx.Err() == nil
		}
		n++
	}
	return n, true
}

// erkennungNachbarn wählt bis zu vier Folgen um die Folge an Stelle i
// herum: aus derselben Staffel, wenn die mindestens zwei andere hat,
// sonst aus der ganzen Serie. Abwechselnd davor und danach.
func erkennungNachbarn(vertreter []store.ErkennungsDatei, i int, staffel string) []store.ErkennungsDatei {
	gleiche := 0
	for j, v := range vertreter {
		if j != i && v.StaffelID == staffel {
			gleiche++
		}
	}
	nurStaffel := gleiche >= 2
	var out []store.ErkennungsDatei
	for abstand := 1; len(out) < erkennungPartner && abstand < len(vertreter); abstand++ {
		for _, j := range []int{i - abstand, i + abstand} {
			if j < 0 || j >= len(vertreter) || len(out) >= erkennungPartner {
				continue
			}
			if nurStaffel && vertreter[j].StaffelID != staffel {
				continue
			}
			out = append(out, vertreter[j])
		}
	}
	return out
}

// erkanntFuer: das automatische Ergebnis für eine Datei, nur ab der
// Mindestsicherheit.
func (s *Server) erkanntFuer(ctx context.Context, f *store.MediaFile) (vor *store.Vorspann, abspannAbMS int64) {
	e, err := s.st.Erkannt(ctx, f.ID, f.SizeBytes, f.MTime, vorspann.Fassung)
	if err != nil || e == nil || e.Stand != store.ErkanntFertig {
		return nil, 0
	}
	if e.VorspannSich >= vorspann.Mindestsicherheit && e.VorspannBisMS > e.VorspannVonMS {
		vor = &store.Vorspann{VonMS: e.VorspannVonMS, BisMS: e.VorspannBisMS, Quelle: "automatisch"}
	}
	if e.AbspannSich >= vorspann.Mindestsicherheit && e.AbspannAbMS > 0 &&
		(f.DurationMS <= 0 || e.AbspannAbMS < f.DurationMS) {
		abspannAbMS = e.AbspannAbMS
	}
	return vor, abspannAbMS
}

// erkennungZustand: für die Verwaltung (adminUmwandlung).
func (s *Server) erkennungZustand(ctx context.Context) map[string]any {
	z, _ := s.st.ErkennungZaehlen(ctx, vorspann.Fassung, vorspann.Mindestsicherheit)
	return map[string]any{"an": s.erkennungAn(ctx), "untersucht": z.Untersucht,
		"mitVorspann": z.MitVorspann, "mitAbspann": z.MitAbspann, "offen": z.Offen}
}

// Wie die Erkennung sich mit den Vorschaubildern abwechselt.
const (
	erkennungStueck = 20 * time.Minute
	erkennungAlle   = 25 // Dateien Vorschaubilder/Untertitel zwischen zwei Stücken
)
