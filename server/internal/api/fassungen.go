// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package api

// Fassungen und Teile (Issue #8): welche Datei eines Titels gilt, eine
// fälschlich zugeordnete Datei abtrennen, einen Film in Teilen zu einer
// Datei zusammenführen.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"lumen/internal/scan"
	"lumen/internal/store"
	"lumen/internal/stream"
)

// adminFassungWaehlen: PUT /admin/items/{id}/fassung {"fileId": "..."}
func (s *Server) adminFassungWaehlen(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FileID string `json:"fileId"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.st.FassungWaehlen(r.Context(), r.PathValue("id"), in.FileID); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminDateiAbtrennen: POST /admin/files/{id}/abtrennen — die Datei wird ein
// eigener Film, mit dem Titel, den ihr Name hergibt.
func (s *Server) adminDateiAbtrennen(w http.ResponseWriter, r *http.Request) {
	f, err := s.st.File(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	g := scan.Parse(f.Path, s.ordnerUeber(r.Context(), f))
	neu, err := s.st.DateiAbtrennen(r.Context(), f.ID, g.Title, g.Year)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, neu)
}

// ordnerUeber nennt die Ordner zwischen Bibliothek und Datei.
func (s *Server) ordnerUeber(ctx context.Context, f *store.MediaFile) []string {
	it, err := s.st.Item(ctx, f.ItemID)
	if err != nil {
		return nil
	}
	libs, _ := s.st.Libraries(ctx)
	for _, l := range libs {
		if l.ID == it.LibraryID {
			if rel, err := filepath.Rel(l.Path, f.Path); err == nil {
				teile := strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/")
				if len(teile) == 1 && teile[0] == "." {
					return nil
				}
				return teile
			}
		}
	}
	return nil
}

var zusammenfuehrenLaeuft sync.Map // item-Kennung → true

// adminTeileZusammenfuehren: POST /admin/items/{id}/teile-zusammenfuehren.
//
// „Apocalypse Now 1979a.avi", „…b.avi", „…c.avi" werden zu einer MKV-Datei
// im selben Ordner, ohne neu zu rechnen (FFmpeg hängt die Teile nur
// aneinander). Die Teile selbst löscht Lumen nicht — sie wandern in einen
// Unterordner „.lumen-teile", den der Scan nicht ansieht. Wer sicher ist,
// löscht ihn von Hand.
func (s *Server) adminTeileZusammenfuehren(w http.ResponseWriter, r *http.Request) {
	it, err := s.st.Item(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	dateien, _ := s.st.FilesFor(r.Context(), it.ID)
	var teile []store.MediaFile
	for _, f := range dateien {
		if !f.Missing {
			teile = append(teile, f)
		}
	}
	sort.Slice(teile, func(i, j int) bool { return teile[i].PartNo < teile[j].PartNo })
	if len(teile) < 2 || teile[len(teile)-1].PartNo < 2 {
		fail(w, http.StatusBadRequest, "dieser titel hat keine teile zum zusammenführen")
		return
	}
	for i, f := range teile {
		if f.PartNo != i+1 {
			fail(w, http.StatusBadRequest, "die teile sind nicht durchgehend nummeriert (1, 2, 3 …) — erst die fassungen aufräumen")
			return
		}
	}
	if s.ff == nil || !s.ff.Available() {
		fail(w, http.StatusServiceUnavailable, "ohne FFmpeg geht das nicht")
		return
	}
	ordner := filepath.Dir(teile[0].Path)
	name := it.Title
	if it.Year > 0 {
		name = fmt.Sprintf("%s (%d)", it.Title, it.Year)
	}
	name = strings.Map(func(c rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, c) {
			return '-'
		}
		return c
	}, name)
	ziel := filepath.Join(ordner, name+".mkv")
	if _, err := os.Stat(ziel); err == nil {
		failW(w, http.StatusConflict, "dort liegt schon eine datei „{name}“", "name", filepath.Base(ziel))
		return
	}
	// Schreiben dürfen? Lieber jetzt merken als nach einer halben Stunde.
	probe, err := os.CreateTemp(ordner, ".lumen-probe-*")
	if err != nil {
		fail(w, http.StatusForbidden, "lumen darf in diesen ordner nicht schreiben (die medien sind schreibgeschützt eingebunden)")
		return
	}
	probe.Close()
	_ = os.Remove(probe.Name())

	if _, laeuft := zusammenfuehrenLaeuft.LoadOrStore(it.ID, true); laeuft {
		fail(w, http.StatusConflict, "läuft schon")
		return
	}
	go func() {
		defer zusammenfuehrenLaeuft.Delete(it.ID)
		ctx := context.Background()
		if err := s.teileZusammenfuehren(ctx, it, teile, ziel); err != nil {
			s.st.Log(ctx, "error", "aufraeumen", "Zusammenführen gescheitert: "+err.Error())
			return
		}
		s.st.Log(ctx, "info", "aufraeumen", fmt.Sprintf("%d Teile zu einer Datei zusammengeführt", len(teile)))
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": tr(w, "Läuft – das kann ein paar Minuten dauern.")})
}

func (s *Server) teileZusammenfuehren(ctx context.Context, it *store.Item, teile []store.MediaFile, ziel string) error {
	liste, err := os.CreateTemp("", "lumen-teile-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(liste.Name())
	for _, f := range teile {
		// Format der concat-Liste: file '<pfad>', Apostrophe verdoppelt.
		fmt.Fprintf(liste, "file '%s'\n", strings.ReplaceAll(f.Path, "'", `'\''`))
	}
	liste.Close()

	tmp := ziel + ".teil"
	ctx, cancel := context.WithTimeout(ctx, 3*time.Hour)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.ff.FFmpegPath, "-hide_banner", "-loglevel", "error", "-nostdin",
		"-f", "concat", "-safe", "0", "-i", liste.Name(), "-map", "0", "-c", "copy", "-f", "matroska", "-y", tmp)
	if out, err := stream.Niedrig(cmd, true); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("ffmpeg: %s", strings.TrimSpace(string(out)))
	}
	if err := os.Rename(tmp, ziel); err != nil {
		return err
	}
	// Die Teile beiseitelegen, nicht löschen.
	beiseite := filepath.Join(filepath.Dir(ziel), ".lumen-teile")
	if err := os.MkdirAll(beiseite, 0o755); err != nil {
		return err
	}
	for _, f := range teile {
		if err := os.Rename(f.Path, filepath.Join(beiseite, filepath.Base(f.Path))); err != nil {
			return err
		}
	}
	// Die neue Datei an den Titel hängen, die Teile austragen; ausgemessen
	// wird sie beim nächsten Durchlauf, der gleich angestossen wird.
	info, err := os.Stat(ziel)
	if err != nil {
		return err
	}
	neu := &store.MediaFile{ItemID: it.ID, Path: ziel, SizeBytes: info.Size(), MTime: info.ModTime().Unix(),
		PartNo: 1, Container: "mkv"}
	if err := s.st.UpsertFile(ctx, neu); err != nil {
		return err
	}
	for _, f := range teile {
		_ = s.st.DateiAustragen(ctx, f.ID)
	}
	libs, _ := s.st.Libraries(ctx)
	for _, l := range libs {
		if l.ID == it.LibraryID {
			go s.Durchlauf(ctx, l, true)
		}
	}
	return nil
}
