// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package sprache übersetzt die Texte, die der Server selbst an Menschen
// schickt — vor allem Fehlermeldungen, die eine App so anzeigt, wie sie
// kommen.
//
// Der Schlüssel ist der deutsche Text (Schweizer Schreibweise), so wie er
// heute im Code steht. Das hat zwei Gründe: Der Code bleibt lesbar (man
// sieht an der Stelle, was gemeint ist), und ein Text ohne Übersetzung
// fällt nicht als Kennung wie „err_42" auf den Bildschirm, sondern als
// verständlicher deutscher Satz.
//
// Kataloge erben: de-DE nimmt alles aus de-CH und überschreibt nur, was
// in Deutschland anders geschrieben wird (ß), de-AT erbt von de-DE, en-GB
// von en-US. So steht jede Abweichung genau einmal da, und eine neue
// Sprache (fr-CH, it-CH) ist eine neue Datei und eine Zeile in Sprachen.
package sprache

import (
	"embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Vorgabe gilt, wo nichts anderes eingestellt ist — auch für alle
// Installationen, die es schon vor der Sprachwahl gab: Lumen sprach bis
// dahin Schweizer Deutsch.
const Vorgabe = "de-CH"

// Sprache ist ein Eintrag der Auswahl. Name steht in der Sprache selbst,
// damit man seine eigene auch dann findet, wenn die Oberfläche gerade in
// einer fremden spricht.
type Sprache struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// erbt: von wem fehlende Texte kommen. Leer heisst: der Schlüssel
	// selbst (de-CH) ist der Text.
	erbt string
	// Metadaten: was bei der Einrichtung für TMDb vorbelegt wird.
	metaSprache, metaRegion string
}

// Sprachen: die Auswahl, in dieser Reihenfolge angezeigt.
//
// Zu den Metadaten: TMDb führt Übersetzungen fast nur für de-DE und en-US.
// Fragt man nach de-CH oder en-GB, fehlen bei den meisten Titeln
// Beschreibung und Titel — TMDb füllt dann nicht mit de-DE auf. Darum
// bleibt die Metadaten-Sprache die grosse Variante; die Region (und damit
// die Altersfreigabe und die Kinostarts) kommt aus der Wahl.
var Sprachen = []Sprache{
	{ID: "de-CH", Name: "Deutsch (Schweiz)", metaSprache: "de-DE", metaRegion: "CH"},
	{ID: "de-DE", Name: "Deutsch (Deutschland)", erbt: "de-CH", metaSprache: "de-DE", metaRegion: "DE"},
	{ID: "de-AT", Name: "Deutsch (Österreich)", erbt: "de-DE", metaSprache: "de-DE", metaRegion: "AT"},
	{ID: "en-US", Name: "English (United States)", metaSprache: "en-US", metaRegion: "US"},
	{ID: "en-GB", Name: "English (United Kingdom)", erbt: "en-US", metaSprache: "en-US", metaRegion: "GB"},
}

func finde(id string) (Sprache, bool) {
	for _, s := range Sprachen {
		if s.ID == id {
			return s, true
		}
	}
	return Sprache{}, false
}

// Gueltig sagt, ob id genau eine der angebotenen Sprachen ist.
func Gueltig(id string) bool {
	_, ok := finde(id)
	return ok
}

// Metadaten gibt die TMDb-Sprache und -Region, die zu einer Wahl passen.
func Metadaten(id string) (sprache, region string) {
	s, ok := finde(id)
	if !ok {
		s, _ = finde(Vorgabe)
	}
	return s.metaSprache, s.metaRegion
}

// Normalisieren macht aus einer Angabe wie „de", „de_ch", „EN-au" eine der
// angebotenen Sprachen — oder "", wenn nichts passt. Eine Region, die es
// nicht gibt, fällt auf die Hauptvariante der Sprache (en-AU → en-GB wäre
// geraten; en-US ist das, was Apple und Google auch tun).
func Normalisieren(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "_", "-"))
	if s == "" {
		return ""
	}
	teile := strings.SplitN(s, "-", 3)
	haupt := strings.ToLower(teile[0])
	if len(teile) > 1 {
		id := haupt + "-" + strings.ToUpper(teile[1])
		if Gueltig(id) {
			return id
		}
		// Liechtenstein und Luxemburg schreiben wie die Schweiz bzw.
		// wie Deutschland; Irland, Australien, Neuseeland britisch.
		switch id {
		case "de-LI":
			return "de-CH"
		case "de-LU", "de-BE":
			return "de-DE"
		case "en-IE", "en-AU", "en-NZ", "en-ZA", "en-IN":
			return "en-GB"
		}
	}
	switch haupt {
	case "de":
		return "de-DE"
	case "en":
		return "en-US"
	}
	return ""
}

// AusAcceptLanguage wählt aus dem Kopf des Browsers die erste Sprache,
// die Lumen spricht. Gewichte (q=…) stehen in der Praxis schon in
// absteigender Reihenfolge; Lumen nimmt die Reihenfolge, wie sie kommt.
func AusAcceptLanguage(kopf string) string {
	for _, teil := range strings.Split(kopf, ",") {
		tag, _, _ := strings.Cut(teil, ";")
		if id := Normalisieren(tag); id != "" {
			return id
		}
	}
	return ""
}

// ---------------------------------------------------------------- Kataloge

//go:embed kataloge/*.json
var dateien embed.FS

// eintrag ist ein Text oder, für Mehrzahl, je Form ein Text.
type eintrag struct {
	text   string
	formen map[string]string // "one", "other" … wie Intl.PluralRules
}

func (e *eintrag) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '{' {
		return json.Unmarshal(b, &e.formen)
	}
	return json.Unmarshal(b, &e.text)
}

var (
	ladenEinmal sync.Once
	kataloge    map[string]map[string]eintrag
	ladeFehler  error
)

func laden() {
	kataloge = map[string]map[string]eintrag{}
	for _, s := range Sprachen {
		b, err := dateien.ReadFile("kataloge/" + s.ID + ".json")
		if err != nil {
			ladeFehler = fmt.Errorf("katalog %s fehlt: %w", s.ID, err)
			continue
		}
		k := map[string]eintrag{}
		if err := json.Unmarshal(b, &k); err != nil {
			ladeFehler = fmt.Errorf("katalog %s: %w", s.ID, err)
			continue
		}
		kataloge[s.ID] = k
	}
}

// Fehler meldet, ob ein Katalog nicht lesbar war (für die Tests; zur
// Laufzeit fällt dann einfach alles auf den deutschen Text zurück).
func Fehler() error {
	ladenEinmal.Do(laden)
	return ladeFehler
}

// Schluessel liefert alle Schlüssel eines Katalogs (für die Tests).
func Schluessel(id string) []string {
	ladenEinmal.Do(laden)
	var out []string
	for k := range kataloge[id] {
		out = append(out, k)
	}
	return out
}

// suchen geht die Kette der Kataloge entlang: de-AT → de-DE → de-CH.
func suchen(id, schluessel string) (eintrag, bool) {
	ladenEinmal.Do(laden)
	for tiefe := 0; id != "" && tiefe < 5; tiefe++ {
		if e, ok := kataloge[id][schluessel]; ok {
			return e, true
		}
		s, ok := finde(id)
		if !ok {
			break
		}
		id = s.erbt
	}
	return eintrag{}, false
}

// Mehrzahl gibt die Form („one" oder „other") wie Intl.PluralRules. Für
// Deutsch und Englisch ist nur 1 Einzahl; Französisch (später) zählt
// auch 0 dazu — darum steht die Regel hier je Sprache und nicht als
// „n == 1" verstreut im Code.
func Mehrzahl(id string, n int) string {
	switch strings.SplitN(id, "-", 2)[0] {
	case "fr":
		if n == 0 || n == 1 {
			return "one"
		}
	default:
		if n == 1 {
			return "one"
		}
	}
	return "other"
}

// T übersetzt schluessel in die Sprache id und setzt Werte ein: {name}
// wird aus werte genommen (abwechselnd Name und Wert). Steht ein Wert
// „n" dabei und hat der Eintrag Formen, entscheidet n über Ein- oder
// Mehrzahl. Ohne Eintrag kommt der Schlüssel selbst zurück — also der
// deutsche Text.
func T(id, schluessel string, werte ...any) string {
	text := schluessel
	if e, ok := suchen(id, schluessel); ok {
		if e.formen != nil {
			n := 0
			for i := 0; i+1 < len(werte); i += 2 {
				if werte[i] == "n" {
					n, _ = strconv.Atoi(fmt.Sprint(werte[i+1]))
				}
			}
			text = e.formen[Mehrzahl(id, n)]
			if text == "" {
				text = e.formen["other"]
			}
		} else {
			text = e.text
		}
	} else if e, ok := suchen(Vorgabe, schluessel); ok && e.formen != nil {
		// Auch im Schweizer Katalog kann eine Mehrzahl stehen — der
		// Schlüssel allein wäre dann die Mehrzahlform.
		return T(Vorgabe, schluessel, werte...)
	}
	for i := 0; i+1 < len(werte); i += 2 {
		text = strings.ReplaceAll(text, "{"+fmt.Sprint(werte[i])+"}", fmt.Sprint(werte[i+1]))
	}
	return text
}

// Bekannt sagt, ob es für schluessel in id (oder ihren Vorfahren) einen
// Eintrag gibt. de-CH kennt jeden Schlüssel, weil der Schlüssel der Text ist.
func Bekannt(id, schluessel string) bool {
	if id == Vorgabe {
		return true
	}
	_, ok := suchen(id, schluessel)
	return ok
}
