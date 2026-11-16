// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build messdb

package store

// Eine Messdatenbank in der Grösse einer gewachsenen Mediathek: 800
// Filme, 150 Serien mit rund 15 000 Folgen, Dateien mit Spuren und
// Kapiteln, NFO-Daten, 240 Filmreihen, Fortschritt für zwei Profile und
// ein volles Protokoll. Nur für Messungen, nicht für die gewöhnlichen
// Tests:
//
//	LUMEN_MESSDB=/tmp/mess/lumen.db go test -mod=vendor -tags messdb -run TestMessDB ./internal/store/
//
// Daneben landet lumen.db.token mit einem Anmeldeschlüssel des Besitzers
// und lumen.db.kind mit einem des Kinderprofils.

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"
)

func messText(r *rand.Rand, n int) string {
	woerter := []string{"der", "die", "das", "und", "ein", "Reise", "nach", "Norden", "wo", "alte",
		"Freunde", "sich", "wiederfinden", "während", "Stadt", "Geheimnis", "verbirgt", "Familie",
		"Sommer", "gegen", "Zeit", "Leben", "zwischen", "Licht", "Schatten", "entdeckt", "plötzlich"}
	var b strings.Builder
	for b.Len() < n {
		b.WriteString(woerter[r.IntN(len(woerter))])
		b.WriteByte(' ')
	}
	return b.String()[:n]
}

func TestMessDB(t *testing.T) {
	pfad := os.Getenv("LUMEN_MESSDB")
	if pfad == "" {
		t.Skip("LUMEN_MESSDB fehlt")
	}
	st, err := Open(pfad)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	r := rand.New(rand.NewPCG(1, 2))
	jetzt := time.Now().Unix()

	filme, _ := st.CreateLibrary(ctx, "Filme", "movie", "/gibt/es/nicht/filme", "de")
	serien, _ := st.CreateLibrary(ctx, "Serien", "series", "/gibt/es/nicht/serien", "de")
	// Kein Durchlauf von selbst: die Dateien gibt es nicht.
	_, _ = st.DB().Exec(`UPDATE library SET scan_interval_min = 0, scan_at = ''`)

	genres := []string{"Drama", "Komödie", "Action", "Thriller", "Krimi", "Abenteuer", "Animation",
		"Familie", "Science-Fiction", "Fantasy", "Dokumentarfilm", "Horror", "Romantik", "Mystery"}
	alter := []int{0, 6, 12, 16, 18}
	var filmIDs, serienIDs, folgenIDs []string
	folgenVon := map[string][]string{}

	datei := func(s *Store, itemID, wo string, dauer int64, untertitel int) error {
		f := &MediaFile{ItemID: itemID, Path: wo, SizeBytes: 1 << 30, MTime: jetzt - int64(r.IntN(3e7)),
			Container: "matroska", DurationMS: dauer, Bitrate: 8000000, Width: 1920, Height: 1080,
			VideoCodec: "hevc"}
		f.Spuren = []Track{{Index: 1, Kind: "audio", Codec: "eac3", Language: "ger", Channels: 6},
			{Index: 2, Kind: "audio", Codec: "eac3", Language: "eng", Channels: 6}}
		for i := 0; i < untertitel; i++ {
			f.Spuren = append(f.Spuren, Track{Index: 3 + i, Kind: "subtitle", Codec: "subrip",
				Language: []string{"ger", "eng", "fre", "ita", "spa"}[i%5], Forced: i == 0})
		}
		f.Kapitel = []Chapter{}
		for i := 0; i < 6; i++ {
			f.Kapitel = append(f.Kapitel, Chapter{Index: i, StartMS: int64(i) * dauer / 6,
				EndMS: int64(i+1) * dauer / 6, Title: fmt.Sprintf("Kapitel %d", i+1), Kind: "normal"})
		}
		return s.UpsertFile(ctx, f)
	}
	zusatz := func(s *Store, id string, nfo, personen int) error {
		return s.tx(ctx, func(tx *sql.Tx) error {
			for i := 0; i < personen; i++ {
				rolle := "actor"
				if i == 0 {
					rolle = "director"
				}
				if _, err := tx.Exec(`INSERT INTO person (item_id, name, role, character, ord) VALUES (?,?,?,?,?)`,
					id, fmt.Sprintf("Person %d", r.IntN(4000)), rolle, messText(r, 14), i); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(`INSERT INTO lokal_nfo (item_id, pfad, mtime, daten) VALUES (?,?,?,?)`,
				id, "/gibt/es/nicht/"+id+".nfo", jetzt, `{"plot":"`+messText(r, nfo)+`"}`); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO item_zusatz (item_id, land) VALUES (?, 'US')`, id); err != nil {
				return err
			}
			for _, art := range []string{"poster", "backdrop"} {
				if _, err := tx.Exec(`INSERT INTO artwork (item_id, kind, path, source_url, blurhash) VALUES (?,?,?,?,?)`,
					id, art, id+"-"+art+".jpg", "https://image.tmdb.org/t/p/original/"+id+".jpg", "LEHV6nWB2yk8pyo0adR*.7kCMdnj"); err != nil {
					return err
				}
			}
			neu := jetzt - int64(r.IntN(4e7))
			_, err := tx.Exec(`UPDATE item SET match_state = 'matched', tmdb_id = ?, added_at = ?, zuletzt_neu = ? WHERE id = ?`,
				fmt.Sprint(r.IntN(900000)), neu, neu, id)
			return err
		})
	}

	// Filme
	if err := st.Stapel(ctx, func(s *Store) error {
		for i := 0; i < 800; i++ {
			a := alter[r.IntN(len(alter))]
			it := &Item{LibraryID: filme.ID, Kind: "movie", Title: fmt.Sprintf("Film %s %d", messText(r, 10), i),
				Year: 1970 + r.IntN(56), Overview: messText(r, 300+r.IntN(400)), AgeYears: &a,
				AgeRating: fmt.Sprint(a), RuntimeMS: int64(80+r.IntN(80)) * 60000,
				IMDbRating: float64(30+r.IntN(60)) / 10, IMDbVotes: r.IntN(500000),
				Genres: []string{genres[r.IntN(len(genres))], genres[r.IntN(len(genres))]}}
			if err := s.UpsertItem(ctx, it); err != nil {
				return err
			}
			filmIDs = append(filmIDs, it.ID)
			if err := datei(s, it.ID, fmt.Sprintf("/gibt/es/nicht/filme/%d.mkv", i), it.RuntimeMS, 5); err != nil {
				return err
			}
			if err := zusatz(s, it.ID, 2500, 15); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Serien mit Staffeln und Folgen
	for i := 0; i < 150; i++ {
		if err := st.Stapel(ctx, func(s *Store) error {
			a := alter[r.IntN(len(alter))]
			se := &Item{LibraryID: serien.ID, Kind: "series", Title: fmt.Sprintf("Serie %s %d", messText(r, 10), i),
				Year: 1990 + r.IntN(36), Overview: messText(r, 300+r.IntN(400)), AgeYears: &a,
				AgeRating: fmt.Sprint(a), IMDbRating: float64(50+r.IntN(45)) / 10,
				Genres: []string{genres[r.IntN(len(genres))], genres[r.IntN(len(genres))]}}
			if err := s.UpsertItem(ctx, se); err != nil {
				return err
			}
			serienIDs = append(serienIDs, se.ID)
			if err := zusatz(s, se.ID, 2500, 15); err != nil {
				return err
			}
			staffeln := 1 + r.IntN(11)
			for sn := 1; sn <= staffeln; sn++ {
				sta := &Item{LibraryID: serien.ID, Kind: "season", ParentID: se.ID, Title: fmt.Sprintf("Staffel %d", sn), SeasonNo: sn}
				if err := s.UpsertItem(ctx, sta); err != nil {
					return err
				}
				folgen := 8 + r.IntN(18)
				for en := 1; en <= folgen; en++ {
					ep := &Item{LibraryID: serien.ID, Kind: "episode", ParentID: sta.ID,
						Title: fmt.Sprintf("Folge %s", messText(r, 16)), SeasonNo: sn, EpisodeNo: en,
						Overview: messText(r, 150+r.IntN(250)), RuntimeMS: int64(20+r.IntN(40)) * 60000, AgeYears: &a}
					if err := s.UpsertItem(ctx, ep); err != nil {
						return err
					}
					folgenIDs = append(folgenIDs, ep.ID)
					folgenVon[se.ID] = append(folgenVon[se.ID], ep.ID)
					if err := datei(s, ep.ID, fmt.Sprintf("/gibt/es/nicht/serien/%d/%d/%d.mkv", i, sn, en), ep.RuntimeMS, 3); err != nil {
						return err
					}
					if err := zusatz(s, ep.ID, 1200, 4); err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Serien: zuletzt_neu ist die jüngste Folge (wie UpsertItem es führt).
	if _, err := st.DB().Exec(`UPDATE item SET zuletzt_neu = neu.n
	     FROM (SELECT st.parent_id AS serie, max(e.added_at) AS n FROM item st JOIN item e ON e.parent_id = st.id
	            WHERE st.kind = 'season' GROUP BY st.parent_id) AS neu
	    WHERE item.id = neu.serie AND neu.n > item.zuletzt_neu`); err != nil {
		t.Fatal(err)
	}

	// Filmreihen wie aus Emby: 210 mit zwei bis vier Filmen, 30 mit einem.
	n := 0
	for i := 0; i < 240 && n < len(filmIDs); i++ {
		k := 2 + r.IntN(3)
		if i >= 210 {
			k = 1
		}
		if n+k > len(filmIDs) {
			k = len(filmIDs) - n
		}
		if _, err := st.EmbyReiheAnlegen(ctx, fmt.Sprintf("Reihe %d %s", i, messText(r, 8)), fmt.Sprint(10000+i), filmIDs[n:n+k]); err != nil {
			t.Fatal(err)
		}
		n += k
	}

	// Zwei Profile: der Besitzer und ein Kind.
	besitzer, err := st.CreateAccount(ctx, NewAccount{Name: "Besitzer", IsOwner: true, Avatar: "av-a", LockKind: "pin", Secret: "4711",
		Grants: []string{filme.ID, serien.ID}, Limits: VerwaltungsLimits()})
	if err != nil {
		t.Fatal(err)
	}
	kind, err := st.CreateAccount(ctx, NewAccount{Name: "Kind", IsKid: true, Avatar: "av-b",
		Grants: []string{filme.ID, serien.ID}, Limits: Limits{MaxAge: 12, MaxStreams: 2, AllowTranscode: true, RemoteQuality: "1080p"}})
	if err != nil {
		t.Fatal(err)
	}
	fortschritt := func(acc string, ids []string, fertig bool, reihe *int) {
		_ = st.tx(ctx, func(tx *sql.Tx) error {
			for _, id := range ids {
				*reihe++
				f, pos := 0, int64(600000+r.IntN(600000))
				if fertig {
					f, pos = 1, 2400000
				}
				if _, err := tx.Exec(`INSERT OR REPLACE INTO progress (account_id, item_id, position_ms, duration_ms, finished, reihenfolge)
				                      VALUES (?,?,?,?,?,?)`, acc, id, pos, 2400000, f, *reihe); err != nil {
					return err
				}
			}
			_, err := tx.Exec(`INSERT OR REPLACE INTO progress_reihe (account_id, reihe, letzter) VALUES (?,?,?)`, acc, *reihe, ids[len(ids)-1])
			return err
		})
	}
	rb, rk := 0, 0
	for i, se := range serienIDs {
		f := folgenVon[se]
		switch {
		case i < 40:
			fortschritt(besitzer.ID, f, true, &rb)
		case i < 60:
			fortschritt(besitzer.ID, f[:len(f)/2], true, &rb)
			fortschritt(besitzer.ID, f[len(f)/2:len(f)/2+1], false, &rb)
		case i < 75:
			fortschritt(kind.ID, f, true, &rk)
		}
	}
	fortschritt(besitzer.ID, filmIDs[:250], true, &rb)
	fortschritt(besitzer.ID, filmIDs[250:262], false, &rb)
	fortschritt(kind.ID, filmIDs[300:420], true, &rk)
	for i := 0; i < 60; i++ {
		_ = st.AddToWatchlist(ctx, besitzer.ID, filmIDs[400+i])
	}
	for i := 0; i < 25; i++ {
		_ = st.AddFavorit(ctx, besitzer.ID, filmIDs[i*7])
	}

	// Protokoll: 100 000 Zeilen.
	_ = st.tx(ctx, func(tx *sql.Tx) error {
		for i := 0; i < 100000; i++ {
			if _, err := tx.Exec(`INSERT INTO log_line (at, level, area, message) VALUES (?,?,?,?)`,
				jetzt-int64(100000-i)*30, []string{"info", "info", "warn", "error"}[i%4], "scan",
				"Durchlauf fertig: "+messText(r, 60+r.IntN(60))); err != nil {
				return err
			}
		}
		return nil
	})

	tb, err := st.IssueToken(ctx, besitzer.ID, "Messung", "browser")
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := st.IssueToken(ctx, kind.ID, "Messung", "browser")
	_ = os.WriteFile(pfad+".token", []byte(tb), 0o600)
	_ = os.WriteFile(pfad+".kind", []byte(tk), 0o600)
	if err := optimieren(ctx, st.db); err != nil {
		t.Fatal(err)
	}
	t.Logf("Filme %d, Serien %d, Folgen %d", len(filmIDs), len(serienIDs), len(folgenIDs))
}
