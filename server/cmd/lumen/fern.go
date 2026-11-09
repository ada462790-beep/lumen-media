// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"lumen/internal/api"
	"lumen/internal/cert"
	"lumen/internal/relay"
)

// verschluesseltHoeren öffnet den zweiten, verschlüsselten Eingang.
//
// Warum zwei Eingänge und nicht einer: im Heimnetz ist der gewöhnliche
// bequemer — kein Zertifikat, keine Warnung, einfach die Adresse eintippen.
// Von aussen darf dagegen nichts im Klartext laufen. Also beides, jeder für
// seinen Zweck.
func verschluesseltHoeren(o Optionen, h http.Handler, log *slog.Logger,
	errCh chan<- error) (*http.Server, string, *tls.Config, error) {

	if !o.TLSAn {
		return nil, "", nil, nil
	}

	namen := []string{"localhost", "127.0.0.1"}
	if o.TLSName != "" {
		namen = append([]string{o.TLSName}, namen...)
	}
	for _, ip := range eigeneAdressen() {
		namen = append(namen, ip)
	}

	zert, woher, err := cert.Laden(o.TLSCert, o.TLSKey, o.DataDir, namen)
	if err != nil {
		return nil, "", nil, fmt.Errorf("zertifikat: %w", err)
	}
	fingerabdruck := cert.Fingerabdruck(zert)

	tlsConf := &tls.Config{Certificates: []tls.Certificate{zert}, MinVersion: tls.VersionTLS12}
	if o.TLSCert != "" {
		// Ein mitgebrachtes Zertifikat (etwa von Let's Encrypt) wird
		// erneuert, während Lumen läuft — dann das neue nehmen.
		tlsConf = &tls.Config{GetCertificate: cert.NeuerNachlader(o.TLSCert, o.TLSKey, zert).Zertifikat,
			MinVersion: tls.VersionTLS12}
	}
	srv := &http.Server{
		Addr:              o.TLSAddr,
		Handler:           h,
		TLSConfig:         tlsConf,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	l, err := net.Listen("tcp", o.TLSAddr)
	if err != nil {
		return nil, "", nil, fmt.Errorf("verschlüsselter eingang %s: %w", o.TLSAddr, err)
	}

	art := "mitgebrachtes Zertifikat"
	if woher.SelbstAusgestellt {
		art = "eigenes Zertifikat"
		if woher.NeuAngelegt {
			art = "eigenes Zertifikat, gerade angelegt"
		}
	}
	log.Info("Verschlüsselter Eingang offen", "adresse", o.TLSAddr, "zertifikat", art,
		"fingerabdruck", cert.Lesbar(fingerabdruck))

	go func() {
		if err := srv.ServeTLS(l, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	return srv, fingerabdruck, tlsConf, nil
}

// fernzugriffStarten hält die Leitung zum Vermittler und meldet der
// Verwaltung, wie es darum steht.
func fernzugriffStarten(ctx context.Context, o Optionen, srv *api.Server,
	eigenerFingerabdruck string, tlsConf *tls.Config, h http.Handler,
	log *slog.Logger) (*relay.Heimseite, error) {

	if o.RelayGeheim == "" {
		return nil, errors.New("für den fernzugriff fehlt das geheimnis " +
			"(--relay-secret oder LUMEN_RELAY_SECRET).\n" +
			"Es muss dasselbe sein wie beim Vermittler.")
	}
	if o.RelayFinger == "" {
		// Ohne Fingerabdruck wird nach den üblichen Regeln geprüft. Das
		// genügt, wenn der Vermittler ein öffentliches Zertifikat hat —
		// bei einem selbst ausgestellten aber nicht, und dann scheitert
		// die Verbindung. Lieber vorher sagen.
		log.Info("Kein Fingerabdruck für den Vermittler angegeben — " +
			"sein Zertifikat muss dann von einer öffentlichen Stelle sein")
	}

	// Das Ziel ist ein eigener verschlüsselter Eingang nur für den
	// Vermittler, auf 127.0.0.1 und einem freien Port. Dadurch verlässt
	// nichts Unverschlüsseltes den Rechner, der Vermittler bekommt nur
	// Bytes zu sehen, mit denen er nichts anfangen kann — und Lumen weiss
	// bei jeder Anfrage, die hier ankommt, sicher: Sie kommt von unterwegs.
	// Das Heimnetz allein wäre kein Beweis; der Vermittler-Tunnel endet ja
	// auch auf diesem Rechner.
	if tlsConf == nil {
		return nil, errors.New("fernzugriff ohne verschlüsselten eingang")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("eingang für den vermittler: %w", err)
	}
	fernSrv := &http.Server{
		Handler:           api.VonAussen(h),
		TLSConfig:         tlsConf,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		_ = fernSrv.Close()
	}()
	go func() {
		if err := fernSrv.ServeTLS(l, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("eingang für den vermittler", "err", err)
		}
	}()
	ziel := l.Addr().String()
	srv.SetzeErlaubteHosts(o.Vermittler)

	hs := &relay.Heimseite{
		Vermittler:           o.Vermittler,
		Geheimnis:            o.RelayGeheim,
		Ziel:                 ziel,
		Fingerabdruck:        o.RelayFinger,
		EigenerFingerabdruck: cert.Lesbar(eigenerFingerabdruck),
		Log:                  log,
	}
	srv.SetFernzugriff(func() any { return hs.Zustand() })

	go hs.Laufen(ctx)
	log.Info("Fernzugriff eingerichtet", "vermittler", o.Vermittler, "ziel", ziel)
	return hs, nil
}

// eigeneAdressen sammelt die IP-Adressen dieses Rechners, damit das eigene
// Zertifikat auch dann passt, wenn jemand die Adresse direkt eintippt.
func eigeneAdressen() []string {
	var out []string
	adr, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range adr {
		n, ok := a.(*net.IPNet)
		if !ok || n.IP.IsLoopback() {
			continue
		}
		if v4 := n.IP.To4(); v4 != nil {
			out = append(out, v4.String())
		}
	}
	if name, err := os.Hostname(); err == nil && name != "" {
		out = append(out, name)
	}
	return out
}
