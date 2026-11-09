// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package uebernahme

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lumen/internal/netz"
)

// boeser baut einen „Emby", der sich anmelden lässt und danach mit der
// Titelliste angreift — so, wie es ein beliebiger Dienst unter einer
// eingetippten Adresse tun könnte.
func boeser(t *testing.T, titel http.HandlerFunc) *Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/System/Info/Public"):
			fmt.Fprint(w, `{"Id":"x","Version":"4.8"}`)
		case strings.HasSuffix(r.URL.Path, "/Users/AuthenticateByName"):
			fmt.Fprint(w, `{"AccessToken":"t","User":{"Id":"u"}}`)
		case strings.HasSuffix(r.URL.Path, "/Items"):
			titel(w, r)
		default:
			w.WriteHeader(204)
		}
	}))
	t.Cleanup(srv.Close)
	s, err := Verbinden(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Anmelden(context.Background(), "a", "b"); err != nil {
		t.Fatal(err)
	}
	return s
}

// Eine Seite mit einer Million winziger Einträge: Lumen hört nach
// eintraegeJeSeite auf, statt alle in den Speicher zu holen.
func TestZuVieleEintraegeJeSeite(t *testing.T) {
	s := boeser(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"TotalRecordCount":1000000,"Items":[`)
		for i := 0; i < 1_000_000; i++ {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprint(w, `{}`)
		}
		fmt.Fprint(w, `]}`)
	})
	_, _, err := s.Stand(context.Background(), nil)
	if !errors.Is(err, ErrZuViel) {
		t.Fatalf("erwartet ErrZuViel, bekommen %v", err)
	}
}

// Eine einzelne riesige Antwort (ein Name mit 20 MB): abgebrochen bei
// antwortHoechstens.
func TestZuGrosseSeite(t *testing.T) {
	s := boeser(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"TotalRecordCount":1,"Items":[{"Name":"`)
		block := strings.Repeat("x", 1<<20)
		for i := 0; i < 20; i++ {
			fmt.Fprint(w, block)
		}
		fmt.Fprint(w, `"}]}`)
	})
	_, _, err := s.Stand(context.Background(), nil)
	if !errors.Is(err, netz.ErrZuGross) {
		t.Fatalf("erwartet ErrZuGross, bekommen %v", err)
	}
}

// Ein Server, der nie aufhört: Seite um Seite mit gültigen Titeln und
// immer grösserer Gesamtzahl. Die Obergrenze für den ganzen Lauf greift.
func TestEndloseSeiten(t *testing.T) {
	n := 0
	s := boeser(t, func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString(`{"TotalRecordCount":99999999,"Items":[`)
		for i := 0; i < seite; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			n++
			fmt.Fprintf(&b, `{"Id":"%d","Type":"Movie","Name":"Film %d"}`, n, n)
		}
		b.WriteString(`]}`)
		fmt.Fprint(w, b.String())
	})
	_, _, err := s.Stand(context.Background(), nil)
	if !errors.Is(err, ErrZuViel) {
		t.Fatalf("erwartet ErrZuViel, bekommen %v", err)
	}
	if n > EintraegeHoechstens+seite {
		t.Fatalf("zu viel gelesen: %d", n)
	}
}

// Viele mittelgrosse Seiten: Die Grenze für alle Antworten zusammen
// greift, bevor 64 MB überschritten sind.
func TestGesamtgrenzeBytes(t *testing.T) {
	seiten := 0
	s := boeser(t, func(w http.ResponseWriter, r *http.Request) {
		seiten++
		fmt.Fprintf(w, `{"TotalRecordCount":99999999,"Items":[{"Id":"%d","Name":"%s"}],"Polster":"%s"}`,
			seiten, "a", strings.Repeat("p", 3<<20))
	})
	_, _, err := s.Stand(context.Background(), nil)
	if !errors.Is(err, netz.ErrZuGross) {
		t.Fatalf("erwartet ErrZuGross, bekommen %v", err)
	}
	if seiten > gesamtHoechstens/(3<<20)+1 {
		t.Fatalf("zu viele Seiten gelesen: %d", seiten)
	}
}

// Eine Anfrage an eine öffentliche Adresse geht mit dem Standard-Client
// gar nicht erst hinaus.
func TestNurHeimnetz(t *testing.T) {
	s, err := Verbinden("http://93.184.216.34:8920", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Info(context.Background()); !errors.Is(err, netz.ErrAdresse) {
		t.Fatalf("öffentliche Adresse abgefragt: %v", err)
	}
}
