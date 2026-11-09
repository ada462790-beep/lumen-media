// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package scan_test

// Messung an einer grossen Sammlung — nur, wenn LUMEN_GROSS auf einen
// Ordner mit den Unterordnern filme/ und serien/ zeigt. Im gewöhnlichen
// Testlauf wird sie übersprungen: Sie dauert Minuten und braucht eine
// Sammlung von zehntausenden (leeren) Dateien.
//
//	LUMEN_GROSS=/pfad/zur/sammlung LUMEN_GROSS_FFPROBE=/pfad/zu/ffprobe \
//	  go test -mod=vendor -run TestGrosseSammlung -v -timeout 2h ./internal/scan/
//
// Gemessen wird, was auf einem NAS weh tut: Dauer des ersten Durchlaufs,
// eines Durchlaufs ohne Änderung, des Aufräumens danach — und wie viele
// Bytes dabei geschrieben werden (wchar aus /proc/self/io).

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"lumen/internal/scan"
	"lumen/internal/store"
	"lumen/internal/stream"
)

func geschrieben() (wchar, syscw int64) {
	f, err := os.Open("/proc/self/io")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), ": ")
		n, _ := strconv.ParseInt(v, 10, 64)
		switch k {
		case "wchar":
			wchar = n
		case "syscw":
			syscw = n
		}
	}
	return
}

func TestGrosseSammlung(t *testing.T) {
	wurzel := os.Getenv("LUMEN_GROSS")
	if wurzel == "" {
		t.Skip("LUMEN_GROSS nicht gesetzt")
	}
	dbPfad := os.Getenv("LUMEN_GROSS_DB")
	if dbPfad == "" {
		dbPfad = filepath.Join(t.TempDir(), "lumen.db")
	}
	st, err := store.Open(dbPfad)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	var prober scan.Prober
	if p := os.Getenv("LUMEN_GROSS_FFPROBE"); p != "" {
		prober = stream.NewFFmpeg("", p)
	}
	sc := scan.New(st, prober)

	messen := func(name string, f func()) {
		w0, c0 := geschrieben()
		t0 := time.Now()
		f()
		w1, c1 := geschrieben()
		t.Logf("MESSUNG %-36s %9.3f s  geschrieben %8.1f MB in %7d Aufrufen",
			name, time.Since(t0).Seconds(), float64(w1-w0)/1e6, c1-c0)
	}
	libs, _ := st.Libraries(ctx)
	var filme, serien *store.Library
	for i := range libs {
		switch libs[i].Kind {
		case "movie":
			filme = &libs[i]
		case "series":
			serien = &libs[i]
		}
	}
	if filme == nil {
		filme, _ = st.CreateLibrary(ctx, "Filme", "movie", filepath.Join(wurzel, "filme"), "de")
	}
	if serien == nil {
		serien, _ = st.CreateLibrary(ctx, "Serien", "series", filepath.Join(wurzel, "serien"), "de")
	}
	nur := os.Getenv("LUMEN_GROSS_NUR") // „rescan": nur die Durchläufe ohne Änderung
	for _, lib := range []*store.Library{filme, serien} {
		if nur != "rescan" {
			messen("erster Durchlauf "+lib.Kind, func() {
				if _, err := sc.Scan(ctx, *lib); err != nil {
					t.Fatal(err)
				}
			})
		}
		messen("Durchlauf ohne Änderung "+lib.Kind, func() {
			if _, err := sc.Scan(ctx, *lib); err != nil {
				t.Fatal(err)
			}
		})
	}
	messen("Optimieren (Statistik)", func() { _ = st.Optimieren(ctx) })
	messen("SerienZusammenfuehren", func() {
		if _, err := st.SerienZusammenfuehren(ctx); err != nil {
			t.Fatal(err)
		}
	})
	messen("FilmeOhneDatei", func() { _, _ = st.FilmeOhneDatei(ctx) })
	for _, sortierung := range []string{"title", "added", "year", "rating"} {
		for _, art := range []string{"movie", "series"} {
			messen("Query "+art+" sort="+sortierung, func() {
				for off := 0; off < 600; off += 60 {
					if _, err := st.Query(ctx, store.Query{Libraries: []string{filme.ID, serien.ID},
						Kinds: []string{art}, Sort: sortierung, Limit: 60, Offset: off}); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
	messen("Duplicates", func() { _, _ = st.Duplicates(ctx) })
	messen("MissingFiles", func() { _, _ = st.MissingFiles(ctx) })
}
