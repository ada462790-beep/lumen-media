// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Die tägliche Sicherung der Datenbank (Regeln: store/sicherung.go) und
// was die Verwaltung damit tun kann: sehen, jetzt sichern, wiederherstellen.

import (
	"context"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"lumen/internal/store"
)

func (s *Server) sicherungsOrdner() string { return filepath.Join(s.dataDir, "sicherungen") }

// sichern macht eine Sicherung und merkt sich Tag und Ergebnis — nur den
// Tag, keine Uhrzeit.
func (s *Server) sichern(ctx context.Context) error {
	err := s.st.Sichern(ctx, s.sicherungsOrdner())
	if err != nil {
		_ = s.st.SetSetting(ctx, "sicherung.fehler", err.Error())
		s.st.Log(ctx, "error", "sicherung", "Sicherung gescheitert: "+err.Error())
		return err
	}
	_ = s.st.SetSetting(ctx, "sicherung.zuletzt", store.TagHeute())
	_ = s.st.SetSetting(ctx, "sicherung.fehler", "")
	return nil
}

// SicherungsWaechter sichert einmal am Tag, nach 4:30 Uhr — nach dem
// nächtlichen Scan, wenn kaum jemand schaut.
func (s *Server) SicherungsWaechter(ctx context.Context) {
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		jetzt := time.Now()
		if jetzt.Hour()*60+jetzt.Minute() < 4*60+30 {
			continue
		}
		if s.st.Setting(ctx, "sicherung.zuletzt", "") == store.TagHeute() {
			continue
		}
		// Ein Fehler wird gemeldet; der nächste Versuch kommt morgen,
		// damit eine kaputte Datenbank nicht alle zehn Minuten prüft.
		if err := s.sichern(ctx); err != nil {
			_ = s.st.SetSetting(ctx, "sicherung.zuletzt", store.TagHeute())
		}
	}
}

func (s *Server) adminSicherungen(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]any{
		"sicherungen":  store.Sicherungen(s.sicherungsOrdner()),
		"zuletzt":      s.st.Setting(r.Context(), "sicherung.zuletzt", ""),
		"fehler":       s.st.Setting(r.Context(), "sicherung.fehler", ""),
		"generationen": store.SicherungsGenerationen,
		"hinweis": tr(w, "Lumen behält genau zwei Sicherungen, die neueste und die davor. Mehr wären ein "+
			"Tagebuch darüber, wer wann wo im Film stand."),
	})
}

func (s *Server) adminSichernJetzt(w http.ResponseWriter, r *http.Request) {
	if err := s.sichern(r.Context()); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.adminSicherungen(w, r)
}

// adminWiederherstellen legt die gewählte Sicherung bereit und startet neu.
func (s *Server) adminWiederherstellen(w http.ResponseWriter, r *http.Request) {
	nr, err := strconv.Atoi(r.PathValue("nr"))
	if err != nil {
		fail(w, http.StatusNotFound, "diese sicherung gibt es nicht")
		return
	}
	if err := store.Wiederherstellen(s.sicherungsOrdner(), nr, filepath.Join(s.dataDir, "lumen.db")); err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	s.st.Log(r.Context(), "warn", "sicherung", "Wiederherstellen gewählt — Lumen startet neu")
	ok(w, map[string]string{"status": tr(w, "Lumen startet neu und nimmt die Sicherung.")})
	go func() {
		time.Sleep(time.Second)
		if err := s.neustarten(); err != nil {
			s.log.Error("neustart nach wiederherstellen", "err", err)
		}
	}()
}
