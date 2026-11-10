// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package vorspann

import (
	"math"
	"math/cmplx"
	"math/rand"
	"testing"
	"time"
)

// ---------------------------------------------------------------- künstlicher Ton

// musik: eine Folge von Tönen mit Obertönen und Hüllkurve, dazu ein Bass —
// je Saat immer dieselbe „Melodie".
func musik(saat int64, sek float64) []float64 {
	rng := rand.New(rand.NewSource(saat))
	n := int(sek * Abtastrate)
	out := make([]float64, n)
	for t := 0; t < n; {
		laenge := int((0.15 + rng.Float64()*0.45) * Abtastrate)
		f := 180 * math.Pow(2, float64(rng.Intn(36))/12)
		bass := f / 4
		for k := 0; k < laenge && t+k < n; k++ {
			x := float64(k) / Abtastrate
			huelle := math.Min(1, x*40) * math.Exp(-x*2.5)
			s := math.Sin(2*math.Pi*f*x) + 0.5*math.Sin(2*math.Pi*2*f*x) + 0.25*math.Sin(2*math.Pi*3*f*x)
			out[t+k] += 0.18*huelle*s + 0.1*math.Sin(2*math.Pi*bass*x)
		}
		t += laenge
	}
	return out
}

// inhalt: etwas wie Sprache und Geräusch — gefiltertes Rauschen in
// Silben, dazwischen leise eigene Musik. Jede Saat klingt anders.
func inhalt(saat int64, sek float64) []float64 {
	rng := rand.New(rand.NewSource(saat))
	n := int(sek * Abtastrate)
	out := make([]float64, n)
	var tief, tiefer float64
	silbe := 4 + rng.Float64()*2
	for t := 0; t < n; t++ {
		x := float64(t) / Abtastrate
		r := rng.NormFloat64()
		tief += 0.25 * (r - tief)
		tiefer += 0.05 * (tief - tiefer)
		huelle := math.Max(0, math.Sin(2*math.Pi*silbe*x+math.Sin(x*0.7)*3))
		out[t] = 0.35*(tief-tiefer)*huelle + 0.01*r
	}
	m := musik(saat+1000, sek)
	for t := range out {
		out[t] += 0.3 * m[t]
	}
	return out
}

// verhunzt: was eine andere Umwandlung derselben Musik antut — leiser
// oder lauter, Rauschen (etwa 15 dB unter der Musik — mehr, als eine
// Umwandlung je anrichtet; so viel wie Geräusche über dem Vorspann).
func verhunzt(x []float64, saat int64, faktor float64) []float64 {
	rng := rand.New(rand.NewSource(saat))
	out := make([]float64, len(x))
	for i, v := range x {
		out[i] = v*faktor + 0.02*rng.NormFloat64()
	}
	return out
}

func alsPCM(teile ...[]float64) []int16 {
	var out []int16
	for _, t := range teile {
		for _, v := range t {
			out = append(out, int16(math.Max(-1, math.Min(1, v))*32767))
		}
	}
	return out
}

func stille(sek float64) []float64 { return make([]float64, int(sek*Abtastrate)) }

// ---------------------------------------------------------------- eine Staffel

type wahrheit struct {
	vorVon, vorBis float64 // -1: kein Vorspann
	abAb           float64 // -1: kein Abspann
}

const testDauer = 22 * 60.0

// folgeBauen: Cold Open (kalt Sekunden, 0 = keins), Vorspann (vorspann
// Sekunden der Titelmusik, 0 = keiner), dann Handlung; am Ende Abspann
// (abspann Sekunden) und danach optional eine kurze Vorschau.
func folgeBauen(nr int, saat int64, kalt, vorspann, abspann, vorschau float64) (*Folge, wahrheit) {
	w := wahrheit{vorVon: -1, vorBis: -1, abAb: -1}
	// Ein Versatz, der nicht auf das Abdruck-Raster fällt.
	krumm := float64(nr*137%500) / Abtastrate
	var teile [][]float64
	pos := 0.0
	if kalt > 0 {
		teile = append(teile, inhalt(saat*10+1, kalt+krumm))
		pos += kalt + krumm
	}
	if vorspann > 0 {
		teile = append(teile, verhunzt(musik(4242, vorspann), saat, 0.7+float64(nr%4)*0.1))
		w.vorVon, w.vorBis = pos, pos+vorspann
		pos += vorspann
	}
	_, lang := AnfangFenster(testDauer)
	if rest := lang - pos; rest > 0 {
		teile = append(teile, inhalt(saat*10+2, rest))
	}
	anfang := AusPCM(alsPCM(teile...), 0)

	ab, laenge := EndeFenster(testDauer)
	teile = nil
	handlung := laenge - abspann - vorschau
	teile = append(teile, inhalt(saat*10+3, handlung))
	if abspann > 0 {
		teile = append(teile, verhunzt(musik(777, abspann), saat+1, 0.9))
		w.abAb = testDauer - abspann - vorschau
	}
	if vorschau > 0 {
		teile = append(teile, inhalt(saat*10+4, vorschau))
	}
	ende := AusPCM(alsPCM(teile...), ab)
	return &Folge{Schluessel: string(rune('A' + nr)), Dauer: testDauer, Anfang: anfang, Ende: ende}, w
}

// nachbarn: bis zu vier Folgen um i herum, wie es die API macht.
func nachbarn(alle []*Folge, i int) []*Folge {
	var out []*Folge
	for abstand := 1; len(out) < 4 && abstand < len(alle); abstand++ {
		if i-abstand >= 0 {
			out = append(out, alle[i-abstand])
		}
		if i+abstand < len(alle) && len(out) < 4 {
			out = append(out, alle[i+abstand])
		}
	}
	return out
}

func TestStaffelErkennen(t *testing.T) {
	if testing.Short() {
		t.Skip("rechnet einige Sekunden")
	}
	// Acht Folgen: verschiedene Cold Opens, Folge 5 ohne Vorspann, Folge 2
	// mit Vorschau nach dem Abspann, Folge 6 ohne Abspann.
	plan := []struct{ kalt, vorspann, abspann, vorschau float64 }{
		{0, 45, 40, 0}, {95, 45, 40, 0}, {30, 45, 40, 25}, {0, 45, 40, 0},
		{180, 45, 40, 0}, {60, 0, 40, 0}, {12, 45, 0, 0}, {240, 45, 40, 0},
	}
	var folgen []*Folge
	var wahr []wahrheit
	start := time.Now()
	for i, p := range plan {
		f, w := folgeBauen(i, int64(100+i), p.kalt, p.vorspann, p.abspann, p.vorschau)
		folgen, wahr = append(folgen, f), append(wahr, w)
	}
	t.Logf("Abdrücke: %v für %d Folgen", time.Since(start).Round(time.Millisecond), len(folgen))
	start = time.Now()
	u := NeuerUntersucher()
	richtig, falsch, verpasst := 0, 0, 0
	for i, f := range folgen {
		b := u.Untersuchen(f, nachbarn(folgen, i))
		w := wahr[i]
		pruefen := func(was string, e *Ergebnis, von, bis float64) {
			erkannt := e != nil && e.Sicherheit >= Mindestsicherheit
			switch {
			case von < 0 && erkannt:
				falsch++
				t.Errorf("Folge %d: %s erkannt (%.1f–%.1f, %.2f), gibt es aber nicht", i, was, e.Von, e.Bis, e.Sicherheit)
			case von < 0:
			case !erkannt:
				verpasst++
				t.Errorf("Folge %d: %s nicht erkannt (%+v)", i, was, e)
			case math.Abs(e.Von-von) > 1 || (bis > 0 && math.Abs(e.Bis-bis) > 1):
				falsch++
				t.Errorf("Folge %d: %s %.1f–%.1f, richtig wäre %.1f–%.1f", i, was, e.Von, e.Bis, von, bis)
			default:
				richtig++
				t.Logf("Folge %d: %s %.2f–%.2f (richtig %.2f–%.2f), Sicherheit %.2f, %d Stützen",
					i, was, e.Von, e.Bis, von, bis, e.Sicherheit, e.Stuetzen)
			}
		}
		pruefen("Vorspann", b.Vorspann, w.vorVon, w.vorBis)
		pruefen("Abspann", b.Abspann, w.abAb, -1)
	}
	t.Logf("Vergleich und Abstimmung: %v; richtig %d, falsch %d, verpasst %d",
		time.Since(start).Round(time.Millisecond), richtig, falsch, verpasst)
}

// Eine Rückblende: Folge 2 beginnt mit 50 s aus Folge 1. Das darf kein
// Vorspann werden — weder in Folge 2 noch in Folge 1.
func TestRueckblendeIstKeinVorspann(t *testing.T) {
	szene := inhalt(9999, 50)
	bauen := func(nr int, mitSzeneVorn, mitSzeneMitte bool) *Folge {
		var teile [][]float64
		if mitSzeneVorn {
			teile = append(teile, verhunzt(szene, int64(nr), 0.9))
		}
		teile = append(teile, inhalt(int64(nr)*7, 60))
		if mitSzeneMitte {
			teile = append(teile, szene)
		}
		teile = append(teile, verhunzt(musik(4242, 40), int64(nr), 1), inhalt(int64(nr)*7+1, 200))
		return &Folge{Schluessel: string(rune('a' + nr)), Dauer: testDauer, Anfang: AusPCM(alsPCM(teile...), 0)}
	}
	f1 := bauen(1, false, true)
	f2 := bauen(2, true, false)
	f3 := bauen(3, false, false)
	u := NeuerUntersucher()
	for _, fall := range []struct {
		f       *Folge
		partner []*Folge
	}{{f1, []*Folge{f2, f3}}, {f2, []*Folge{f1, f3}}} {
		b := u.Untersuchen(fall.f, fall.partner)
		if b.Vorspann == nil || b.Vorspann.Sicherheit < Mindestsicherheit {
			t.Fatalf("%s: Vorspann nicht erkannt: %+v", fall.f.Schluessel, b.Vorspann)
		}
		if b.Vorspann.Dauer() < 38 || b.Vorspann.Dauer() > 42 {
			t.Errorf("%s: %+v ist nicht der Vorspann (40 s)", fall.f.Schluessel, b.Vorspann)
		}
	}
}

func (e Ergebnis) Dauer() float64 { return e.Bis - e.Von }

// Stille ist in jeder Folge gleich — sie darf nie ein Treffer sein.
func TestStilleIstKeinTreffer(t *testing.T) {
	a := AusPCM(alsPCM(inhalt(1, 20), stille(60), inhalt(2, 20)), 0)
	b := AusPCM(alsPCM(inhalt(3, 30), stille(60), inhalt(4, 10)), 0)
	if tr := Vergleichen(a, b, VorspannMin); len(tr) > 0 {
		t.Fatalf("Stille als gemeinsamer Abschnitt: %+v", tr)
	}
}

// Ganz verschiedener Inhalt: kein Treffer.
func TestVerschiedenesTrifftNicht(t *testing.T) {
	a := AusPCM(alsPCM(inhalt(11, 300)), 0)
	b := AusPCM(alsPCM(inhalt(12, 300)), 0)
	if tr := Vergleichen(a, b, VorspannMin); len(tr) > 0 {
		t.Fatalf("Treffer in verschiedenem Inhalt: %+v", tr)
	}
}

// Zu kurz gemeinsam (ein Senderlogo von 6 s) zählt nicht.
func TestKurzesGemeinsamesZaehltNicht(t *testing.T) {
	logo := musik(55, 6)
	a := AusPCM(alsPCM(inhalt(21, 30), logo, inhalt(22, 60)), 0)
	b := AusPCM(alsPCM(inhalt(23, 50), logo, inhalt(24, 40)), 0)
	if tr := Vergleichen(a, b, VorspannMin); len(tr) > 0 {
		t.Fatalf("kurzer Abschnitt als Treffer: %+v", tr)
	}
}

// Zwei Folgen allein: Ein sauberer Treffer reicht, ein schmutziger nicht.
func TestSchwelleBeiNurEinemPartner(t *testing.T) {
	if s := sicherheit(1, 1, 0.20); s < Mindestsicherheit {
		t.Errorf("sauberer Treffer mit einem Partner: %.2f", s)
	}
	if s := sicherheit(1, 1, 0.36); s >= Mindestsicherheit {
		t.Errorf("schmutziger Treffer mit einem Partner gilt: %.2f", s)
	}
	if s := sicherheit(1, 4, 0.10); s >= Mindestsicherheit {
		t.Errorf("einer von vier Partnern gilt: %.2f", s)
	}
	if s := sicherheit(2, 4, 0.30); s < Mindestsicherheit {
		t.Errorf("zwei von vier Partnern mit ordentlichem Treffer gelten nicht: %.2f", s)
	}
}

func TestAbstimmungNimmtMehrheit(t *testing.T) {
	// Drei Partner sehen den Vorspann bei 60–100 s, einer (die Folge,
	// aus der die Rückblende stammt) zusätzlich 0–55 s.
	je := [][]Spanne{
		{{Von: 0, Bis: 55, Fehler: 0.1}, {Von: 60.2, Bis: 100.1, Fehler: 0.15}},
		{{Von: 59.8, Bis: 100, Fehler: 0.2}},
		{{Von: 60, Bis: 99.6, Fehler: 0.18}},
	}
	e, ok := Abstimmen(je, nil)
	if !ok || e.Stuetzen != 3 || math.Abs(e.Von-60) > 0.3 || math.Abs(e.Bis-100) > 0.5 {
		t.Fatalf("Abstimmung: %+v", e)
	}
}

func TestFFT(t *testing.T) {
	n := 64
	f := neueFFT(n)
	re, im := make([]float64, n), make([]float64, n)
	x := make([]complex128, n)
	rng := rand.New(rand.NewSource(1))
	for i := range re {
		re[i] = rng.Float64() - 0.5
		x[i] = complex(re[i], 0)
	}
	f.rechnen(re, im)
	for k := 0; k < n; k++ {
		var s complex128
		for j := 0; j < n; j++ {
			s += x[j] * cmplx.Exp(complex(0, -2*math.Pi*float64(j*k)/float64(n)))
		}
		if cmplx.Abs(s-complex(re[k], im[k])) > 1e-9 {
			t.Fatalf("FFT[%d] = %v, richtig %v", k, complex(re[k], im[k]), s)
		}
	}
}

func BenchmarkVergleichZehnMinuten(b *testing.B) {
	x := AusPCM(alsPCM(inhalt(1, 300), musik(4242, 45), inhalt(2, 255)), 0)
	y := AusPCM(alsPCM(inhalt(3, 100), musik(4242, 45), inhalt(4, 455)), 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Vergleichen(x, y, VorspannMin)
	}
}

func BenchmarkAbdruckZehnMinuten(b *testing.B) {
	pcm := alsPCM(inhalt(1, 600))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		AusPCM(pcm, 0)
	}
}

// Zufällige Staffeln: Vorspann zwischen 20 und 90 s an zufälliger Stelle
// (oft bei null), jede sechste Folge ohne Vorspann, jede siebte ohne
// Abspann, Abspann zwischen 15 und 60 s, manchmal mit Vorschau danach.
// Gezählt wird, wie oft die Erkennung auf eine Sekunde genau stimmt.
func TestStaffelnZufaellig(t *testing.T) {
	if testing.Short() {
		t.Skip("rechnet einige Sekunden")
	}
	rng := rand.New(rand.NewSource(7))
	richtig, falsch, verpasst, gesamt := 0, 0, 0, 0
	var vergleich time.Duration
	for staffel := 0; staffel < 3; staffel++ {
		vorspannLaenge := 20 + rng.Float64()*70
		abspannLaenge := 15 + rng.Float64()*45
		var folgen []*Folge
		var wahr []wahrheit
		for i := 0; i < 6; i++ {
			kalt := 0.0
			if rng.Intn(2) == 0 {
				kalt = 5 + rng.Float64()*240
			}
			v, a, vorschau := vorspannLaenge, abspannLaenge, 0.0
			if (staffel*6+i)%6 == 5 {
				v = 0
			}
			if (staffel*6+i)%7 == 3 {
				a = 0
			}
			if rng.Intn(3) == 0 {
				vorschau = 10 + rng.Float64()*30
			}
			f, w := folgeBauen(i, int64(1000*staffel+i), kalt, v, a, vorschau)
			// Jede Staffel ihre eigene Musik: die Saat des Vorspanns ist in
			// folgeBauen fest; hier genügt, dass die Folgen verschieden sind.
			folgen, wahr = append(folgen, f), append(wahr, w)
		}
		start := time.Now()
		u := NeuerUntersucher()
		for i, f := range folgen {
			b := u.Untersuchen(f, nachbarn(folgen, i))
			for _, fall := range []struct {
				e        *Ergebnis
				von, bis float64
			}{{b.Vorspann, wahr[i].vorVon, wahr[i].vorBis}, {b.Abspann, wahr[i].abAb, -1}} {
				gesamt++
				erkannt := fall.e != nil && fall.e.Sicherheit >= Mindestsicherheit
				switch {
				case fall.von < 0 && !erkannt:
					richtig++
				case fall.von < 0 || (erkannt && (math.Abs(fall.e.Von-fall.von) > 1 || (fall.bis > 0 && math.Abs(fall.e.Bis-fall.bis) > 1))):
					falsch++
					t.Logf("Staffel %d Folge %d: falsch %+v, richtig %.1f–%.1f", staffel, i, fall.e, fall.von, fall.bis)
				case !erkannt:
					verpasst++
					t.Logf("Staffel %d Folge %d: verpasst %+v, richtig %.1f–%.1f", staffel, i, fall.e, fall.von, fall.bis)
				default:
					richtig++
				}
			}
		}
		vergleich += time.Since(start)
	}
	t.Logf("%d Fälle: richtig %d, falsch %d, verpasst %d; Vergleich je Folge %v",
		gesamt, richtig, falsch, verpasst, (vergleich / 18).Round(time.Millisecond))
	if falsch > 0 {
		t.Errorf("%d falsche Erkennungen — lieber keine als eine falsche", falsch)
	}
	if richtig < gesamt*9/10 {
		t.Errorf("nur %d von %d richtig", richtig, gesamt)
	}
}
