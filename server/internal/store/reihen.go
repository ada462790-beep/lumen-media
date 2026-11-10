// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Filmreihen.
//
// Lumen bildet sie selbst — niemand soll „Herr der Ringe" von Hand
// zusammenklicken müssen. Zwei Wege:
//
//   - Mit TMDb-Schlüssel sagt TMDb, welche Filme zusammengehören. Das ist
//     verlässlich und geht vor.
//   - Ohne Schlüssel liest Lumen es aus den Titeln: „Alien 3", „Rocky II",
//     „Der Herr der Ringe: Die zwei Türme", „Harry Potter und …".
//
// Der Besitzer kann eine Reihe umbenennen oder auflösen. Beides bleibt,
// auch wenn Lumen die Reihen später neu bildet.
//
// Reihen gehören niemandem: sie beschreiben die Mediathek, nicht wer was
// schaut. Wer welche Filme einer Reihe sehen darf, entscheidet wie überall
// die Freigabe — das prüft die API.

type Reihe struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Overview string   `json:"overview,omitempty"`
	Source   string   `json:"source"` // tmdb | auto
	Hidden   bool     `json:"hidden,omitempty"`
	ItemIDs  []string `json:"-"`
	// Dateinamen im Bilderordner — die Bilder der Reihe selbst.
	PosterDatei   string `json:"-"`
	BackdropDatei string `json:"-"`
	TMDbID        string `json:"-"`
}

// TMDbReiheZuordnen hängt einen Film an die Reihe, die TMDb nennt. Steht
// der Film schon in einer selbst erkannten Reihe, kommt er dort heraus:
// TMDb weiss es besser.
func (s *Store) TMDbReiheZuordnen(ctx context.Context, tmdbID, name, itemID string, jahr int) error {
	tmdbID, name = strings.TrimSpace(tmdbID), strings.TrimSpace(name)
	if tmdbID == "" || name == "" {
		return nil
	}
	return s.tx(ctx, func(t *sql.Tx) error {
		var id string
		var eigener int
		err := t.QueryRowContext(ctx,
			`SELECT id, eigener_name FROM collection WHERE source = 'tmdb' AND tmdb_id = ?`, tmdbID).
			Scan(&id, &eigener)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			id = newID("col")
			if _, err := t.ExecContext(ctx,
				`INSERT INTO collection (id, name, tmdb_id, source) VALUES (?, ?, ?, 'tmdb')`,
				id, name, tmdbID); err != nil {
				return err
			}
		case err != nil:
			return err
		case eigener == 0:
			if _, err := t.ExecContext(ctx,
				`UPDATE collection SET name = ? WHERE id = ?`, name, id); err != nil {
				return err
			}
		}
		// Aus selbst erkannten Reihen heraus — und aus einer anderen
		// TMDb-Reihe, falls der Film neu zugeordnet wurde.
		if _, err := t.ExecContext(ctx,
			`DELETE FROM collection_item WHERE item_id = ? AND collection_id != ? AND collection_id IN
			   (SELECT id FROM collection WHERE source IN ('auto', 'tmdb'))`, itemID, id); err != nil {
			return err
		}
		_, err = t.ExecContext(ctx,
			`INSERT INTO collection_item (collection_id, item_id, ord) VALUES (?, ?, ?)
			 ON CONFLICT (collection_id, item_id) DO UPDATE SET ord = excluded.ord`,
			id, itemID, jahr)
		return err
	})
}

// ReihenNeuBilden erkennt Reihen aus den Titeln aller Filme, die noch in
// keiner TMDb-Reihe stehen. Läuft nach jedem Durchlauf und nach dem
// Holen der Metadaten; das Ergebnis ist jedes Mal dasselbe, solange sich
// die Filme nicht ändern.
func (s *Store) ReihenNeuBilden(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT i.id, i.title, COALESCE(i.year, 0) FROM item i
		  WHERE i.kind = 'movie'
		    AND NOT EXISTS (SELECT 1 FROM collection_item ci
		                      JOIN collection c ON c.id = ci.collection_id
		                     WHERE ci.item_id = i.id AND c.source = 'tmdb')
		  ORDER BY COALESCE(i.year, 9999), i.sort_title, i.id`)
	if err != nil {
		return 0, err
	}
	var filme []ReihenFilm
	for rows.Next() {
		var f ReihenFilm
		if err := rows.Scan(&f.ID, &f.Titel, &f.Jahr); err != nil {
			rows.Close()
			return 0, err
		}
		filme = append(filme, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	gruppen := ReihenErkennen(filme)

	err = s.tx(ctx, func(t *sql.Tx) error {
		behalten := map[string]bool{}
		for _, g := range gruppen {
			var id string
			err := t.QueryRowContext(ctx,
				`SELECT id FROM collection WHERE source = 'auto' AND schluessel = ?`, g.Schluessel).
				Scan(&id)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				id = "col_" + kurzHash(g.Schluessel)
				if _, err := t.ExecContext(ctx,
					`INSERT INTO collection (id, name, source, schluessel) VALUES (?, ?, 'auto', ?)
					 ON CONFLICT (id) DO UPDATE SET schluessel = excluded.schluessel`,
					id, g.Name, g.Schluessel); err != nil {
					return err
				}
			case err != nil:
				return err
			default:
				// Den Namen nur nachführen, wenn der Besitzer ihn nicht
				// selbst gesetzt hat.
				if _, err := t.ExecContext(ctx,
					`UPDATE collection SET name = ? WHERE id = ? AND eigener_name = 0`,
					g.Name, id); err != nil {
					return err
				}
			}
			behalten[id] = true
			if _, err := t.ExecContext(ctx,
				`DELETE FROM collection_item WHERE collection_id = ?`, id); err != nil {
				return err
			}
			for _, f := range g.Filme {
				if _, err := t.ExecContext(ctx,
					`INSERT OR IGNORE INTO collection_item (collection_id, item_id, ord) VALUES (?, ?, ?)`,
					id, f.ID, f.Jahr); err != nil {
					return err
				}
			}
		}

		// Selbst erkannte Reihen, die es nicht mehr gibt, verschwinden.
		// Aufgelöste bleiben stehen, solange es sie gibt — sonst käme eine
		// Reihe, die der Besitzer weggeräumt hat, beim nächsten Durchlauf
		// einfach wieder.
		alte, err := t.QueryContext(ctx,
			`SELECT id FROM collection WHERE source = 'auto' AND hidden = 0 AND eigener_name = 0`)
		if err != nil {
			return err
		}
		var weg []string
		for alte.Next() {
			var id string
			if err := alte.Scan(&id); err != nil {
				alte.Close()
				return err
			}
			if !behalten[id] {
				weg = append(weg, id)
			}
		}
		alte.Close()
		for _, id := range weg {
			if _, err := t.ExecContext(ctx, `DELETE FROM collection WHERE id = ?`, id); err != nil {
				return err
			}
		}
		// Reihen ohne einen einzigen Film — etwa nach dem Löschen einer
		// Bibliothek — sind keine Reihen mehr.
		//
		// Was der Besitzer umbenannt oder aufgelöst hat, bleibt stehen, auch
		// wenn die Reihe für den Moment nicht zusammenkommt. Sonst käme sie
		// beim nächsten Mal mit altem Namen und wieder sichtbar zurück. Ihre
		// Filme verliert sie dann aber.
		if _, err := t.ExecContext(ctx,
			`DELETE FROM collection_item WHERE collection_id IN
			   (SELECT id FROM collection WHERE source = 'auto') AND collection_id NOT IN (`+
				placeholders(max(len(behalten), 1))+`)`, behaltenListe(behalten)...); err != nil {
			return err
		}
		_, err = t.ExecContext(ctx,
			`DELETE FROM collection WHERE hidden = 0 AND eigener_name = 0 AND NOT EXISTS
			   (SELECT 1 FROM collection_item ci WHERE ci.collection_id = collection.id)`)
		return err
	})
	return len(gruppen), err
}

const reiheCols = `c.id, c.name, COALESCE(c.overview, ''), c.source, c.hidden,
	COALESCE(c.poster, ''), COALESCE(c.backdrop, ''), COALESCE(c.tmdb_id, '')`

func scanReihe(sc interface{ Scan(...any) error }) (*Reihe, error) {
	var r Reihe
	var hidden int
	if err := sc.Scan(&r.ID, &r.Name, &r.Overview, &r.Source, &hidden,
		&r.PosterDatei, &r.BackdropDatei, &r.TMDbID); err != nil {
		return nil, err
	}
	r.Hidden = hidden == 1
	return &r, nil
}

// Reihen gibt alle Reihen mit ihren Filmen in Reihenfolge — aufgelöste
// nur, wenn danach gefragt wird.
func (s *Store) Reihen(ctx context.Context, auchAufgeloeste bool) ([]Reihe, error) {
	q := `SELECT ` + reiheCols + ` FROM collection c`
	if !auchAufgeloeste {
		q += ` WHERE c.hidden = 0`
	}
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	var out []Reihe
	for rows.Next() {
		r, err := scanReihe(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	nachNamenSortieren(out, func(r *Reihe) string { return r.Name }, func(r *Reihe) string { return r.ID }, nil)
	for i := range out {
		if out[i].ItemIDs, err = s.reiheFilme(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) reiheFilme(ctx context.Context, id string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT ci.item_id FROM collection_item ci JOIN item i ON i.id = ci.item_id
		  WHERE ci.collection_id = ? ORDER BY `+reihenOrdnung, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Reihe holt eine einzelne Reihe samt Filmen.
func (s *Store) Reihe(ctx context.Context, id string) (*Reihe, error) {
	r, err := scanReihe(s.db.QueryRowContext(ctx,
		`SELECT `+reiheCols+` FROM collection c WHERE c.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if r.ItemIDs, err = s.reiheFilme(ctx, r.ID); err != nil {
		return nil, err
	}
	return r, nil
}

// ReiheVon nennt die Reihe, in der ein Film steht — oder nil.
func (s *Store) ReiheVon(ctx context.Context, itemID string) (*Reihe, error) {
	var id string
	err := s.db.QueryRowContext(ctx,
		`SELECT c.id FROM collection_item ci JOIN collection c ON c.id = ci.collection_id
		  WHERE ci.item_id = ? AND c.hidden = 0
		  ORDER BY CASE c.source WHEN 'tmdb' THEN 0 ELSE 1 END LIMIT 1`, itemID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.Reihe(ctx, id)
}

// ReiheAendern benennt eine Reihe um oder löst sie auf (und wieder ein).
func (s *Store) ReiheAendern(ctx context.Context, id string, name *string, aufgeloest *bool) error {
	if _, err := s.Reihe(ctx, id); err != nil {
		return err
	}
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" {
			return errors.New("der name fehlt")
		}
		if len([]rune(n)) > 80 {
			return errors.New("der name ist zu lang")
		}
		if _, err := s.db.ExecContext(ctx,
			`UPDATE collection SET name = ?, eigener_name = 1 WHERE id = ?`, n, id); err != nil {
			return err
		}
	}
	if aufgeloest != nil {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE collection SET hidden = ? WHERE id = ?`, b2i(*aufgeloest), id); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------- Titel lesen

// ReihenFilm ist, was die Erkennung von einem Film braucht.
type ReihenFilm struct {
	ID    string
	Titel string
	Jahr  int
}

// ReihenGruppe ist eine erkannte Reihe.
type ReihenGruppe struct {
	Schluessel string // klein geschrieben, ohne Satzzeichen
	Name       string // wie er dasteht
	Filme      []ReihenFilm
}

// Ein Teil am Ende des Titels, der ihn als Fortsetzung kennzeichnet:
// „ 2", „ II", „: Teil 3", „ – Part 4", „ Episode IV". Davor muss ein
// Abstand oder Satzzeichen stehen — sonst würde aus „Max" ein „Ma".
var reFortsetzung = regexp.MustCompile(
	`(?i)[\s:,.\-–—]+(?:(?:teil|part|kapitel|chapter|episode|vol\.?|volume|folge)\s*)?(\d{1,2}|[ivx]{1,4})\s*$`)

// Trenner zwischen Reihe und Untertitel.
var trenner = []string{": ", " - ", " – ", " — "}

var fuellwoerter = map[string]bool{
	"der": true, "die": true, "das": true, "den": true, "dem": true, "des": true,
	"ein": true, "eine": true, "einer": true, "eines": true, "und": true, "oder": true,
	"von": true, "vom": true, "zum": true, "zur": true, "im": true, "in": true,
	"am": true, "an": true, "auf": true, "mit": true, "aus": true, "fuer": true, "für": true,
	"the": true, "a": true, "and": true, "of": true, "to": true, "on": true, "at": true,
	"la": true, "le": true, "les": true, "el": true, "los": true, "de": true, "du": true,
	"il": true, "lo": true, "et": true,
	// Wörter, mit denen viele Titel anfangen, ohne dass sie eine Reihe
	// ergäben: „The Man Who Knew …", „The Man Who Fell …".
	"who": true, "what": true, "when": true, "where": true, "with": true, "from": true,
	"for": true, "was": true, "is": true, "are": true, "my": true, "your": true,
	"his": true, "her": true, "its": true, "our": true, "their": true, "this": true,
	"that": true, "it": true, "i": true, "you": true, "we": true, "they": true,
	"man": true, "men": true, "ich": true, "wir": true, "wer": true,
}

// woerter zerlegt einen Titel in klein geschriebene Wörter ohne
// Satzzeichen.
func woerter(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
	})
}

func normSchluessel(s string) string { return strings.Join(woerter(s), " ") }

// bedeutsam: Trägt der Schlüssel genug, um eine Reihe zu tragen? „Der"
// allein nicht, „Star" allein auch nicht — „Alien" oder „Herr Ringe" schon.
func bedeutsam(schluessel string) bool {
	if len([]rune(schluessel)) < 3 {
		return false
	}
	gewicht, lang := 0, false
	for _, w := range strings.Fields(schluessel) {
		if fuellwoerter[w] {
			continue
		}
		gewicht++
		if len([]rune(w)) >= 5 {
			lang = true
		}
	}
	return gewicht >= 2 || lang
}

type kandidat struct {
	name    string // Anzeige
	schl    string
	mindest int    // so viele Filme braucht es mindestens
	nummer  string // abgeschnittene Fortsetzungs-Nummer, klein: „3", „ii"
	vonNr   bool   // entstand der Name nur durch das Abschneiden einer Nummer?
}

// kandidaten nennt, welche Reihen-Namen in einem Titel stecken könnten.
func kandidaten(titel string) []kandidat {
	titel = strings.TrimSpace(titel)
	var out []kandidat
	add := func(name string, mindest int) {
		name = strings.TrimSpace(name)
		nummer, vonNr := "", false
		if m := reFortsetzung.FindStringSubmatch(name); m != nil {
			nummer, vonNr = strings.ToLower(m[1]), true
			name = strings.TrimSpace(reFortsetzung.ReplaceAllString(name, ""))
		}
		name = strings.TrimRight(name, " :-–—,.")
		if k := normSchluessel(name); k != "" {
			out = append(out, kandidat{name: name, schl: k, mindest: mindest, nummer: nummer, vonNr: vonNr})
		}
	}
	// a) der ganze Titel ohne Fortsetzungs-Nummer: „Alien 3" → „Alien"
	add(titel, 2)
	// b) vor dem letzten Trenner: „Mission: Impossible – Fallout"
	//    → „Mission: Impossible"
	// c) vor dem ersten Trenner: „Der Herr der Ringe: Die Gefährten"
	//    → „Der Herr der Ringe"
	letzter, erster := -1, -1
	for _, t := range trenner {
		if i := strings.LastIndex(titel, t); i > letzter {
			letzter = i
		}
		if i := strings.Index(titel, t); i >= 0 && (erster < 0 || i < erster) {
			erster = i
		}
	}
	if letzter > 0 {
		add(titel[:letzter], 2)
	}
	if erster > 0 && erster != letzter {
		add(titel[:erster], 2)
	}
	// d) die ersten zwei tragenden Wörter, bei längeren Titeln ohne
	//    Trenner: „Harry Potter und der Stein der Weisen" → „Harry Potter".
	//    Das ist die unsicherste Lesart; darum braucht sie drei Filme.
	roh := strings.Fields(titel)
	if len(roh) >= 3 {
		tragend := 0
		for i, w := range roh {
			// Zeichen wie „&" oder „+" tragen nichts.
			if n := normSchluessel(w); n != "" && !fuellwoerter[n] {
				tragend++
			}
			if tragend == 2 {
				add(strings.Join(roh[:i+1], " "), 3)
				break
			}
		}
	}
	return out
}

// ReihenErkennen bildet aus Filmtiteln Reihen. Rein rechnerisch, ohne
// Datenbank — damit lässt es sich prüfen, ohne eine anzulegen.
func ReihenErkennen(filme []ReihenFilm) []ReihenGruppe {
	type zaehler struct {
		filme   map[string]bool
		name    string
		mindest int
		anders  bool // trägt mindestens ein Film mehr als nur diesen Namen?
		// Nur Nummern sind kein Beweis: „Apollo 13" und „Apollo 18" sind
		// keine Reihe, „District 9" und „District 13" auch nicht. Eine
		// Reihe braucht einen Film, der den Namen ohne Nummer trägt (das
		// Original oder einen mit Untertitel), oder einen Teil 2.
		ohneNr bool
		zwei   bool
	}
	alle := map[string]*zaehler{}
	je := make([][]kandidat, len(filme))
	for i, f := range filme {
		// Je Name: kam er bei diesem Film auch ohne Nummer zustande?
		nurNr := map[string]bool{}
		for _, k := range kandidaten(f.Titel) {
			if v, ok := nurNr[k.schl]; ok {
				nurNr[k.schl] = v && k.vonNr
			} else {
				nurNr[k.schl] = k.vonNr
			}
		}
		gesehen := map[string]bool{}
		for _, k := range kandidaten(f.Titel) {
			if gesehen[k.schl] || !bedeutsam(k.schl) {
				continue
			}
			gesehen[k.schl] = true
			je[i] = append(je[i], k)
			z := alle[k.schl]
			if z == nil {
				z = &zaehler{filme: map[string]bool{}, name: k.name, mindest: k.mindest}
				alle[k.schl] = z
			}
			if k.mindest < z.mindest {
				z.mindest = k.mindest
			}
			z.filme[f.ID] = true
			if normSchluessel(f.Titel) != k.schl {
				z.anders = true
			}
			if !nurNr[k.schl] {
				z.ohneNr = true
			}
			if k.nummer == "2" || k.nummer == "ii" {
				z.zwei = true
			}
		}
	}

	// Jeder Film kommt in die Reihe, die am meisten Filme trägt; bei
	// Gleichstand in die mit dem längeren Namen. „Mission: Impossible"
	// schlägt „Mission".
	gruppen := map[string]*ReihenGruppe{}
	for i, f := range filme {
		var best *kandidat
		for k := range je[i] {
			kk := je[i][k]
			z := alle[kk.schl]
			// Lauter gleiche Titel sind Neuverfilmungen, keine Reihe:
			// „Dune" von 1984 und „Dune" von 2021.
			if len(z.filme) < z.mindest || !z.anders || !(z.ohneNr || z.zwei) {
				continue
			}
			if best == nil {
				best = &kk
				continue
			}
			zb := alle[best.schl]
			if len(z.filme) > len(zb.filme) ||
				(len(z.filme) == len(zb.filme) && len(kk.schl) > len(best.schl)) {
				best = &kk
			}
		}
		if best == nil {
			continue
		}
		g := gruppen[best.schl]
		if g == nil {
			g = &ReihenGruppe{Schluessel: best.schl, Name: alle[best.schl].name}
			gruppen[best.schl] = g
		}
		g.Filme = append(g.Filme, f)
	}

	out := []ReihenGruppe{}
	for _, g := range gruppen {
		if len(g.Filme) < 2 {
			continue
		}
		sort.SliceStable(g.Filme, func(a, b int) bool {
			if g.Filme[a].Jahr != g.Filme[b].Jahr {
				return g.Filme[a].Jahr < g.Filme[b].Jahr
			}
			return g.Filme[a].Titel < g.Filme[b].Titel
		})
		out = append(out, *g)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Schluessel < out[b].Schluessel })
	return out
}

func kurzHash(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}

func behaltenListe(m map[string]bool) []any {
	out := []any{}
	for id := range m {
		out = append(out, id)
	}
	if len(out) == 0 {
		out = append(out, "")
	}
	return out
}

// TMDbReihenOhneBilder nennt die TMDb-Reihen, für die noch keine Bilder
// geholt wurden — Kennung bei Lumen und bei TMDb.
func (s *Store) TMDbReihenOhneBilder(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tmdb_id FROM collection WHERE source = 'tmdb' AND bilder_geholt = 0 AND tmdb_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, tmdb string
		if err := rows.Scan(&id, &tmdb); err != nil {
			return nil, err
		}
		out[id] = tmdb
	}
	return out, rows.Err()
}

// ReiheBilderSetzen vermerkt Beschreibung und Bilder einer Reihe. Leere
// Werte lassen stehen, was da ist; geholt gilt sie danach so oder so —
// sonst würde bei jedem Durchlauf erneut gefragt.
func (s *Store) ReiheBilderSetzen(ctx context.Context, id, overview, poster, backdrop string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE collection SET
		   overview = CASE WHEN ? != '' THEN ? ELSE overview END,
		   poster   = CASE WHEN ? != '' THEN ? ELSE poster END,
		   backdrop = CASE WHEN ? != '' THEN ? ELSE backdrop END,
		   bilder_geholt = 1
		 WHERE id = ?`,
		overview, overview, poster, poster, backdrop, backdrop, id)
	return err
}
