// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package api ist die HTTP-Schnittstelle. Alle vier Apps sprechen nur hierüber
// mit dem Server — es gibt keinen zweiten Weg an die Daten.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"lumen/internal/meta"
	"lumen/internal/scan"
	"lumen/internal/store"
	"lumen/internal/stream"
	"lumen/internal/update"
)

type Server struct {
	st        *store.Store
	scanner   *scan.Scanner
	ff        *stream.FFmpeg
	tr        *stream.Transcoder
	live      *stream.Live
	meta      *meta.Resolver
	dataDir   string
	sourceURL string
	// fernzugriff gibt den Zustand des Vermittlers, falls einer eingerichtet
	// ist. Die API kennt den Vermittler nicht — sie fragt nur eine Funktion,
	// damit das Paket api nichts von Netzwerk-Tunneln wissen muss.
	fernzugriff func() any
	log         *slog.Logger
	version     string
	started     time.Time

	// Weitere Namen, unter denen Lumen erreichbar ist (siehe herkunft.go).
	erlaubteHosts []string
	// Gegen Durchprobieren von PIN, Passwort, Codes (siehe bremse.go).
	bremse *bremse
	// Wo die Zwischendateien beim Umwandeln liegen (für die Übersicht).
	umwandlungDir string
	// grund endet, wenn Lumen herunterfährt. Arbeit im Hintergrund
	// (Durchläufe, Metadaten) hängt daran statt an der Anfrage, die sie
	// angestossen hat — siehe hintergrund().
	grund        context.Context
	grundBeenden func()
	// Welcher Browser-Player zu welcher Wiedergabe gehört (laeufe.go).
	laeufe laufBuch
	// Sprache des Servers und der Profile im Speicher (sprache.go).
	sprachen sprachSpeicher
	// Bildschirmzeit der Kinderprofile (bildschirmzeit.go).
	bz bzWaechter
	// jetzt: die Uhr für Bildschirmzeit-Fenster; nil = time.Now (Tests stellen sie).
	jetzt func() time.Time
}

// SetzeGrund gibt den Kontext, der beim Herunterfahren endet.
func (s *Server) SetzeGrund(ctx context.Context) { s.grund = ctx }

// SetzeBeenden nennt die Funktion, die alle Arbeit im Hintergrund abbricht
// (das Ende von grund). Ein Neustart nach Update oder Wiederherstellen
// ruft sie vor dem Neustart auf.
func (s *Server) SetzeBeenden(f func()) { s.grundBeenden = f }

// neustarten startet Lumen neu — aber erst, nachdem Umwandlungen und Arbeit
// im Hintergrund beendet sind. Vorher liefen FFmpeg-Prozesse über den
// Neustart hinaus weiter und blieben im Container als Zombies liegen.
func (s *Server) neustarten() error {
	if s.grundBeenden != nil {
		s.grundBeenden()
	}
	if s.tr != nil {
		s.tr.StopAll()
	}
	if !s.ArbeitAbwarten(10 * time.Second) {
		s.log.Warn("Arbeit im Hintergrund hörte vor dem Neustart nicht rechtzeitig auf")
	}
	err := update.Neustart()
	if err != nil {
		// Die Arbeit im Hintergrund ist schon beendet. Im Container startet
		// Docker Lumen neu, wenn es endet — das ist besser als ein Lumen,
		// das weiterläuft, aber nie mehr prüft, lädt oder sichert.
		s.log.Error("Neustart gescheitert — Lumen bitte von Hand neu starten", "err", err)
		if imContainer() {
			os.Exit(1)
		}
	}
	return err
}

// hintergrund ist der Kontext für Arbeit, die eine Anfrage anstösst, die
// aber länger läuft als diese: Sie endet nicht mit der Anfrage, wohl aber,
// wenn Lumen herunterfährt. Früher hing sie an context.WithoutCancel der
// Anfrage und lief beim Herunterfahren einfach weiter — in eine
// Datenbank, die main gerade schloss.
func (s *Server) hintergrund() context.Context {
	if s.grund == nil {
		return context.Background()
	}
	return s.grund
}

// SetUmwandlungsOrdner nennt den Ordner der Zwischendateien.
func (s *Server) SetUmwandlungsOrdner(d string) { s.umwandlungDir = d }

func New(st *store.Store, sc *scan.Scanner, ff *stream.FFmpeg, tr *stream.Transcoder,
	live *stream.Live, mt *meta.Resolver, dataDir string, log *slog.Logger,
	version string) *Server {
	srv := &Server{st: st, scanner: sc, ff: ff, tr: tr, live: live, meta: mt,
		dataDir: dataDir, log: log, version: version, started: time.Now(), bremse: neueBremse()}
	datenOrdnerSetzen(dataDir)
	// Die Wiedergabe hat Vorrang: wird gerade etwas umgewandelt, geht der
	// Durchlauf langsamer.
	if sc != nil && live != nil {
		sc.Bremse = func() bool { return live.Load().Transcodes > 0 }
	}
	// Wer den Quelltext anderswo liegen hat — etwa weil er etwas geändert
	// hat —, trägt die Stelle über LUMEN_SOURCE_URL ein. Das ist keine
	// Höflichkeit, sondern was §13 der AGPL verlangt.
	// Frühere Fassungen legten gewandelte Untertitel als Datei ab — mit
	// Zeitstempel des ersten Abspielens. Weg damit, samt Resten.
	if dataDir != "" {
		_ = os.RemoveAll(filepath.Join(dataDir, "untertitel"))
		if alt, _ := filepath.Glob(filepath.Join(dataDir, "vtt-*")); alt != nil {
			for _, p := range alt {
				_ = os.Remove(p)
			}
		}
	}
	// Von OpenSubtitles geholte Untertitel liegen im Datenverzeichnis,
	// nicht neben dem Film (untertitel_suche.go).
	if dataDir != "" && st != nil {
		st.SetGeholteUntertitel(filepath.Join(dataDir, "untertitel-geholt"))
	}
	srv.sourceURL = os.Getenv("LUMEN_SOURCE_URL")
	if srv.sourceURL == "" {
		srv.sourceURL = defaultSourceURL
	}
	return srv
}

// SetResolver tauscht die Metadaten-Quelle aus, wenn der Besitzer die
// Schlüssel ändert — ohne Neustart.
func (s *Server) SetResolver(r *meta.Resolver) { s.meta = r }

// SetFernzugriff hinterlegt, woher der Zustand des Fernzugriffs kommt.
func (s *Server) SetFernzugriff(f func() any) { s.fernzugriff = f }

// SourceURL ist die Fundstelle des Quelltexts, wie sie ausgeliefert wird.
func (s *Server) SourceURL() string { return s.sourceURL }

// SourceURLIstEigen sagt, ob der Betreiber eine eigene Fundstelle
// eingetragen hat. Ist das nicht so, gilt die Voreinstellung — und die
// zeigt auf den unveränderten Quelltext, was für eine unveränderte
// Fassung genau richtig ist.
//
// Hier stand vorher eine Warnung, solange die Voreinstellung galt. Das
// war richtig, als dort ein Platzhalter stand und der Link ins Leere
// zeigte. Jetzt zeigt er auf etwas Echtes, und eine Warnung, die bei
// jedem unveränderten Server angeht, lehrt nur, Warnungen zu übersehen.
func (s *Server) SourceURLIstEigen() bool {
	return s.sourceURL != defaultSourceURL
}

// defaultSourceURL ist, wo der unveränderte Quelltext liegt. Wer etwas
// ändert, muss seine eigene Fassung erreichbar machen — über
// LUMEN_SOURCE_URL. Das verlangt §13 der AGPL.
const defaultSourceURL = "https://github.com/ada462790-beep/lumen-media"

// ---------------------------------------------------------------- Routen

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Offen — vor der Anmeldung erreichbar.
	// Diese Wege prüfen den Hostnamen (gegen DNS-Rebinding) und kennen den
	// Unterschied zwischen zu Hause und unterwegs (herkunft.go).
	bekannt := s.nurBekannterHost
	mux.HandleFunc("GET /api/v1/hello", s.hello)
	// Für die Gesundheitsprüfung von Docker: 200 nur, wenn die Datenbank
	// antwortet. Verrät nichts.
	mux.HandleFunc("GET /api/v1/health", s.gesund)
	mux.HandleFunc("POST /api/v1/setup", bekannt(s.setup))
	mux.HandleFunc("GET /api/v1/profiles", bekannt(s.profiles))
	mux.HandleFunc("POST /api/v1/unlock", bekannt(s.unlock))
	// Anmelden mit Code (Fernseher): anfragen und nachfragen ohne
	// Anmeldung, freigeben nur angemeldet.
	mux.HandleFunc("POST /api/v1/code/start", bekannt(s.codeStart))
	mux.HandleFunc("GET /api/v1/code/{geheim}", bekannt(s.codeAbfragen))
	mux.HandleFunc("POST /api/v1/invite/redeem", bekannt(s.redeemInvite))

	// Angemeldet.
	auth := s.requireAuth
	mux.Handle("GET /api/v1/me", auth(s.me))
	mux.Handle("POST /api/v1/code/approve", auth(s.codeFreigeben))
	mux.Handle("PATCH /api/v1/me", auth(s.updateMe))
	mux.Handle("POST /api/v1/me/avatar", auth(s.meAvatarSetzen))
	// Profilbilder: Die Profilauswahl zeigt sie vor der Anmeldung — zu
	// Hause. Von unterwegs (wo es keine Auswahl gibt) nur angemeldet.
	mux.Handle("GET /api/v1/avatar/{id}", s.zuHauseOffen(s.avatarBild))
	mux.Handle("GET /api/v1/me/media-key", auth(s.meinMedienSchluessel))
	mux.Handle("GET /api/v1/news", auth(s.neuigkeiten))
	mux.Handle("POST /api/v1/news/seen", auth(s.neuigkeitenGesehen))
	// Derselbe Griff auch als POST. Nicht aus Beliebigkeit: auf Android
	// gibt es nur HttpURLConnection, und die kennt PATCH nicht — ein
	// `setRequestMethod("PATCH")` wirft dort eine ProtocolException. Die
	// App könnte also sonst das eigene Profil nicht ändern.
	mux.Handle("POST /api/v1/me", auth(s.updateMe))
	mux.Handle("GET /api/v1/me/devices", auth(s.myDevices))
	mux.Handle("DELETE /api/v1/me/devices/{id}", auth(s.geraetAbmelden))
	// PIN oder Passwort des eigenen Profils ändern (schutz.go).
	mux.Handle("PUT /api/v1/me/sperre", auth(s.meineSperre))
	mux.Handle("POST /api/v1/me/sign-out-everywhere", auth(s.signOutEverywhere))

	mux.Handle("GET /api/v1/home", auth(s.home))
	mux.Handle("GET /api/v1/items", auth(s.items))
	mux.Handle("GET /api/v1/search", auth(s.suche))
	mux.Handle("GET /api/v1/items/{id}", auth(s.item))
	mux.Handle("GET /api/v1/items/{id}/children", auth(s.itemChildren))
	mux.Handle("GET /api/v1/items/{id}/weiterschauen", auth(s.weiterInSerie))

	mux.Handle("POST /api/v1/items/{id}/progress", auth(s.saveProgress))
	mux.Handle("POST /api/v1/items/{id}/finished", auth(s.markFinished))
	mux.Handle("DELETE /api/v1/items/{id}/finished", auth(s.clearFinished))
	mux.Handle("PUT /api/v1/items/{id}/rating", auth(s.setRating))
	mux.Handle("PUT /api/v1/watchlist/{id}", auth(s.addWatchlist))
	mux.Handle("DELETE /api/v1/watchlist/{id}", auth(s.removeWatchlist))
	mux.Handle("PUT /api/v1/favorites/{id}", auth(s.addFavorit))
	mux.Handle("DELETE /api/v1/favorites/{id}", auth(s.removeFavorit))
	mux.Handle("GET /api/v1/items/letters", auth(s.buchstaben))
	mux.Handle("GET /api/v1/persons/{name}", auth(s.person))
	mux.Handle("GET /api/v1/persons/{name}/bild", auth(s.personBild))
	mux.Handle("DELETE /api/v1/me/progress", auth(s.clearAllProgress))
	mux.Handle("DELETE /api/v1/me/device", auth(s.meinGeraetAbmelden))
	mux.Handle("GET /api/v1/me/playbacks", auth(s.meineWiedergaben))
	mux.Handle("DELETE /api/v1/me/playbacks/{id}", auth(s.meineWiedergabeBeenden))
	mux.Handle("GET /api/v1/me/bildschirmzeit", auth(s.meineBildschirmzeit))

	mux.Handle("GET /api/v1/wishes", auth(s.wishes))
	mux.Handle("POST /api/v1/wishes", auth(s.createWish))

	mux.Handle("GET /api/v1/stream/{fileId}", auth(s.streamFile))
	mux.Handle("GET /api/v1/stream/{fileId}/plan", auth(s.streamPlan))
	mux.Handle("GET /api/v1/stream/{fileId}/browser.mp4", auth(s.browserStream))
	mux.Handle("GET /api/v1/stream/{fileId}/seek", auth(s.seekPoint))
	mux.Handle("GET /api/v1/stream/{fileId}/browser/grund", auth(s.browserGrund))
	mux.Handle("GET /api/v1/stream/{fileId}/hls/master.m3u8", auth(s.hlsMaster))
	mux.Handle("GET /api/v1/stream/{fileId}/hls/{session}/media.m3u8", auth(s.hlsMedia))
	mux.Handle("GET /api/v1/stream/{fileId}/hls/{session}/{segment}", auth(s.hlsSegment))
	mux.Handle("DELETE /api/v1/stream/{fileId}/hls/{session}", auth(s.hlsStop))
	mux.Handle("GET /api/v1/stream/{fileId}/subtitles/{spur}", auth(s.untertitel))
	mux.Handle("GET /api/v1/stream/{fileId}/untertitel/{spur}", auth(s.hlsUntertitel))
	// Herunterladen, Vorschaubilder beim Spulen (download.go, hintergrund.go).
	mux.Handle("GET /api/v1/items/{id}/download/info", auth(s.downloadInfo))
	mux.Handle("GET /api/v1/stream/{fileId}/download", auth(s.download))
	mux.Handle("GET /api/v1/stream/{fileId}/trickplay", auth(s.trickplay))
	mux.Handle("GET /api/v1/stream/{fileId}/trickplay/{blatt}", auth(s.trickplayBlatt))

	// Nur der Besitzer.
	// admin: Besitzer und Mitverwalter (Gruppe mit Verwaltung), mit
	// offener Verwaltungssperre. owner: nur der Besitzer selbst.
	admin := s.requireAdmin
	owner := s.requireOwner
	mux.Handle("GET /api/v1/admin/overview", admin(s.adminOverview))
	mux.Handle("GET /api/v1/admin/remote", admin(s.adminRemote))
	mux.Handle("GET /api/v1/admin/libraries", admin(s.adminLibraries))
	// Bibliotheken anlegen und entfernen und dabei Ordner auf dem NAS
	// durchsehen: nur der Besitzer. Ein Mitverwalter könnte sonst jeden
	// Ordner des Rechners einbinden.
	mux.Handle("POST /api/v1/admin/libraries", owner(s.adminCreateLibrary))
	mux.Handle("DELETE /api/v1/admin/libraries/{id}", owner(s.adminDeleteLibrary))
	// Alle wie viele Minuten Lumen von selbst nachsieht.
	mux.Handle("POST /api/v1/admin/libraries/{id}", admin(s.adminLibrarySetzen))
	// Metadaten neu holen: alle, eine Bibliothek, ein Titel — oder einen
	// Titel von Hand dem richtigen Eintrag zuweisen.
	mux.Handle("POST /api/v1/admin/metadata/reload", admin(s.adminMetadatenNeu))
	mux.Handle("POST /api/v1/admin/items/{id}/metadata/reload", admin(s.adminTitelNeu))
	mux.Handle("GET /api/v1/admin/items/{id}/metadata/search", admin(s.adminTitelSuchen))
	mux.Handle("POST /api/v1/admin/items/{id}/metadata/assign", admin(s.adminTitelZuweisen))
	mux.Handle("PUT /api/v1/admin/items/{id}/intro", admin(s.adminVorspannSetzen))
	mux.Handle("DELETE /api/v1/admin/items/{id}/intro", admin(s.adminVorspannLoeschen))
	mux.Handle("PUT /api/v1/admin/items/{id}/abspann", admin(s.adminAbspannSetzen))
	mux.Handle("DELETE /api/v1/admin/items/{id}/abspann", admin(s.adminAbspannLoeschen))
	// Fassungen und Teile.
	mux.Handle("PUT /api/v1/admin/items/{id}/fassung", admin(s.adminFassungWaehlen))
	mux.Handle("POST /api/v1/admin/files/{id}/abtrennen", admin(s.adminDateiAbtrennen))
	mux.Handle("POST /api/v1/admin/libraries/{id}/scan", admin(s.adminScan))
	// Ordner anklicken statt den Pfad blind eintippen.
	mux.Handle("GET /api/v1/admin/browse", owner(s.adminBrowse))
	// Die Wünsche des Haushalts, für den Besitzer an einer Stelle.
	mux.Handle("GET /api/v1/admin/wishes", admin(s.adminWishes))
	// Die Sperre selbst: lesen und entsperren gehen auch gesperrt — sonst
	// käme niemand je hinein.
	mux.Handle("GET /api/v1/admin/lock", auth(s.adminSperre))
	mux.Handle("POST /api/v1/admin/unlock", auth(s.adminEntsperren))
	mux.Handle("POST /api/v1/admin/relock", auth(s.adminSperren))
	mux.Handle("POST /api/v1/admin/lock/totp", admin(s.adminSperreTOTP))
	mux.Handle("PUT /api/v1/admin/lock", admin(s.adminSperreSetzen))
	// Gruppen: nur der Besitzer.
	mux.Handle("GET /api/v1/admin/groups", admin(s.adminGruppen))
	// Rechte ändern verlangt den Code noch einmal.
	mux.Handle("POST /api/v1/admin/groups", owner(s.bestaetigt(s.adminGruppeAnlegen)))
	mux.Handle("PUT /api/v1/admin/groups/{id}", owner(s.bestaetigt(s.adminGruppeAendern)))
	mux.Handle("DELETE /api/v1/admin/groups/{id}", owner(s.bestaetigt(s.adminGruppeLoeschen)))
	mux.Handle("PUT /api/v1/admin/accounts/{id}/group", owner(s.bestaetigt(s.adminKontoGruppe)))
	mux.Handle("PUT /api/v1/admin/accounts/{id}/rolle", owner(s.bestaetigt(s.adminKontoRolle)))
	mux.Handle("DELETE /api/v1/admin/accounts/{id}", owner(s.bestaetigt(s.adminKontoEntfernen)))
	mux.Handle("PUT /api/v1/admin/server-name", owner(s.adminServerName))
	mux.Handle("PUT /api/v1/admin/sprache", owner(s.adminSprache))
	// Schreibt in die Medienordner — nur der Besitzer.
	mux.Handle("POST /api/v1/admin/items/{id}/teile-zusammenfuehren", owner(s.adminTeileZusammenfuehren))
	// Sicherung: sehen, jetzt sichern, wiederherstellen — nur der Besitzer.
	mux.Handle("GET /api/v1/admin/backups", owner(s.adminSicherungen))
	mux.Handle("POST /api/v1/admin/backups", owner(s.adminSichernJetzt))
	mux.Handle("POST /api/v1/admin/backups/{nr}/restore", owner(s.adminWiederherstellen))
	mux.Handle("GET /api/v1/admin/update", owner(s.adminUpdate))
	mux.Handle("POST /api/v1/admin/update/check", owner(s.adminUpdatePruefen))
	mux.Handle("POST /api/v1/admin/update/settings", owner(s.adminUpdateEinstellen))
	mux.Handle("POST /api/v1/admin/update/run", owner(s.adminUpdateJetzt))
	// Umwandeln: Hardware, Vorschaubilder, Untertitel (umwandlung_einstellungen.go).
	mux.Handle("GET /api/v1/admin/umwandlung", owner(s.adminUmwandlung))
	mux.Handle("PUT /api/v1/admin/umwandlung", owner(s.adminUmwandlungSetzen))
	mux.Handle("POST /api/v1/admin/umwandlung/pruefen", owner(s.adminUmwandlungPruefen))

	// Filmreihen — von Lumen selbst gebildet. Lesen darf jeder, was er
	// sehen darf; umbenennen und auflösen nur der Besitzer.
	mux.Handle("GET /api/v1/collections", auth(s.reihen))
	mux.Handle("GET /api/v1/collections/{id}", auth(s.reihe))
	mux.Handle("GET /api/v1/admin/collections", admin(s.adminReihen))
	mux.Handle("POST /api/v1/admin/collections/{id}", admin(s.adminReiheAendern))

	// Wiedergabelisten. Jede gehört einer Person; der Besitzer des
	// Servers hat hier keine Sonderrechte.
	mux.Handle("GET /api/v1/playlists", auth(s.listen))
	mux.Handle("POST /api/v1/playlists", auth(s.listeAnlegen))
	mux.Handle("GET /api/v1/playlists/{id}", auth(s.liste))
	mux.Handle("POST /api/v1/playlists/{id}", auth(s.listeAendern))
	mux.Handle("DELETE /api/v1/playlists/{id}", auth(s.listeLoeschen))
	mux.Handle("POST /api/v1/playlists/{id}/items", auth(s.listeHinzufuegen))
	mux.Handle("DELETE /api/v1/playlists/{id}/items/{item}", auth(s.listeEntfernen))
	mux.Handle("POST /api/v1/playlists/{id}/order", auth(s.listeOrdnen))
	mux.Handle("GET /api/v1/admin/sessions", admin(s.adminSessions))
	mux.Handle("DELETE /api/v1/admin/sessions/{id}", admin(s.adminStopSession))
	mux.Handle("GET /api/v1/admin/log", admin(s.adminLog))
	mux.Handle("GET /api/v1/admin/cleanup", admin(s.adminCleanup))
	mux.Handle("GET /api/v1/admin/accounts", admin(s.adminAccounts))
	mux.Handle("POST /api/v1/admin/invites", admin(s.adminCreateInvite))
	mux.Handle("PUT /api/v1/admin/accounts/{id}/limits", admin(s.bestaetigt(s.adminSetLimits)))
	// Bildschirmzeit eines Kinderprofils; das Geschenk („heute 30 Min.
	// mehr") ohne erneuten Code, es gilt nur heute (bildschirmzeit.go).
	mux.Handle("PUT /api/v1/admin/accounts/{id}/bildschirmzeit", admin(s.bestaetigt(s.adminBildschirmzeit)))
	mux.Handle("POST /api/v1/admin/accounts/{id}/bildschirmzeit/geschenk", admin(s.adminBildschirmzeitGeschenk))
	// PIN eines Haushaltsprofils zurücksetzen — nie die des Besitzers.
	mux.Handle("PUT /api/v1/admin/accounts/{id}/sperre", admin(s.bestaetigt(s.adminKontoSperre)))
	mux.Handle("PUT /api/v1/admin/wishes/{id}", admin(s.adminSetWishState))
	mux.Handle("GET /api/v1/admin/metadata", admin(s.adminMetaSources))
	// Die Schlüssel für TMDb und OMDb: nur Besitzer und Vollzugriff. Ein
	// Mitverwalter könnte sonst die Quelle auf einen eigenen Schlüssel
	// umstellen oder den des Besitzers löschen.
	mux.Handle("PUT /api/v1/admin/metadata", owner(s.adminSetMetaSources))
	mux.Handle("POST /api/v1/admin/metadata/resolve", admin(s.adminResolve))
	mux.Handle("GET /api/v1/admin/metadata/unmatched", admin(s.adminUnmatched))
	mux.Handle("GET /api/v1/art/{item}/{kind}", auth(s.artwork))

	// Handbearbeitung, Sammlungen, Übernahme, Untertitel (routen_funktionen.go).
	s.routenFunktionen(mux)

	return s.withCommonHeaders(mux)
}

// ---------------------------------------------------------------- Anmeldung

type ctxKey int

const (
	ctxAccount ctxKey = iota
	ctxDevice
)

func accountOf(r *http.Request) *store.Account {
	a, _ := r.Context().Value(ctxAccount).(*store.Account)
	return a
}

func deviceOf(r *http.Request) *store.Device {
	d, _ := r.Context().Value(ctxDevice).(*store.Device)
	return d
}

// requireAuth zieht das Token aus dem Authorization-Kopf. Wo ein Player
// keinen Kopf schicken kann (Bilder, Videos, Untertitel), gilt stattdessen
// der Medienschlüssel in der Adresse (?key=…, siehe medienschluessel.go).
func (s *Server) requireAuth(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var acc *store.Account
		var dev *store.Device
		var err error
		if tok := bearer(r); tok != "" {
			acc, dev, err = s.st.LookupToken(r.Context(), tok)
		} else if k := r.URL.Query().Get("key"); k != "" && medienWeg(r) {
			sum, gueltig := medien.pruefen(k)
			if !gueltig {
				fail(w, http.StatusUnauthorized, "medienschlüssel abgelaufen")
				return
			}
			acc, dev, err = s.st.LookupTokenHash(r.Context(), sum)
		} else {
			// ?token= in der Adresse gibt es nicht mehr: Alle Apps und der
			// Browser nehmen den Medienschlüssel. Ein Token in der Adresse
			// landet in Verläufen und Protokollen von Proxys.
			fail(w, http.StatusUnauthorized, "nicht angemeldet")
			return
		}
		if err != nil {
			fail(w, http.StatusUnauthorized, "anmeldung abgelaufen")
			return
		}
		ctx := context.WithValue(r.Context(), ctxAccount, acc)
		ctx = context.WithValue(ctx, ctxDevice, dev)
		w.Header().Set(kopfSprache, s.wirksameSprache(r.Context(), acc.ID))
		h(w, r.WithContext(ctx))
	})
}

// bearer liest das Token aus dem Kopf. Nur dort — nicht aus Cookies (es
// gibt keine) und nicht aus der Adresse.
func bearer(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return ""
}

// ---------------------------------------------------------------- Kopfzeilen

func (s *Server) withCommonHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		// Vor der Anmeldung: Sprache des Geräts oder des Servers.
		// requireAuth ersetzt sie durch die des Profils.
		h.Set(kopfSprache, s.anfrageSprache(r))
		// Kein CORS: Die Oberfläche kommt vom selben Ursprung, die Apps
		// sind keine Webseiten und brauchen es nicht. Früher bekam jeder
		// Ursprung die Erlaubnis — damit hätte jede fremde Webseite, die
		// jemand im Heimnetz öffnet, Antworten von Lumen lesen können.
		//
		// Ausnahme: Ströme mit Medienschlüssel in der Adresse. Ein Chromecast
		// spielt sie in einer Seite von Google ab und braucht dafür CORS. Der
		// Schlüssel steckt schon in der Adresse — wer sie hat, darf ohnehin
		// abspielen; ohne Schlüssel bleibt alles beim Alten.
		if castStrom(r) {
			h.Set("Access-Control-Allow-Origin", "*")
			h.Set("Access-Control-Allow-Headers", "Range")
			h.Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges")
			h.Set("Cross-Origin-Resource-Policy", "cross-origin")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// castStrom: eine Stromanfrage mit Medienschlüssel (für den Chromecast).
func castStrom(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		return false
	}
	return strings.HasPrefix(r.URL.Path, "/api/v1/stream/") && r.URL.Query().Get("key") != ""
}

// ---------------------------------------------------------------- JSON

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Antworten der API sind persönlich (Fortschritt, Listen, Konten) —
	// kein Browser-Zwischenspeicher und kein Proxy soll sie aufheben.
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func ok(w http.ResponseWriter, v any) { writeJSON(w, http.StatusOK, v) }

type errBody struct {
	Error string `json:"error"`
	// Code: feste Kennung für Apps, wo es eine gibt (fehlerCodes).
	Code string `json:"code,omitempty"`
}

// fail antwortet mit einer Meldung in der Sprache der Anfrage (siehe
// sprache.go). msg ist der deutsche Text und zugleich der Schlüssel im
// Katalog; ohne Übersetzung bleibt er, wie er ist.
func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errBody{Error: satzAnfang(tr(w, msg)), Code: fehlerCodes[msg]})
}

// satzAnfang: Eine Meldung beginnt gross — auch die alten, kurzen
// Schlüssel wie „nicht gefunden", die als Satz auf dem Bildschirm landen.
func satzAnfang(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 || !unicode.IsLower(r) {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// failW wie fail, mit Werten für die Platzhalter im Text:
// failW(w, 409, "dort liegt schon eine datei „{name}“", "name", n).
func failW(w http.ResponseWriter, status int, schluessel string, werte ...any) {
	writeJSON(w, status, errBody{Error: satzAnfang(tr(w, schluessel, werte...)), Code: fehlerCodes[schluessel]})
}

// failErr übersetzt die Fehler des Speichers in Statuscodes.
// apiFehler trägt Status und Text für die Antwort selbst.
type apiFehler struct {
	status int
	text   string // deutscher Text = Schlüssel im Katalog
	werte  []any  // für Platzhalter wie {feld}: abwechselnd Name und Wert
}

func (e *apiFehler) Error() string { return e.text }

// failErr übersetzt einen Fehler in eine Antwort. Was nicht ausdrücklich
// für das Gerät gedacht ist, verliert vorher jeden Pfad — eine Meldung
// des Betriebssystems nennt sonst gern den Ort einer Datei.
func failErr(w http.ResponseWriter, err error) {
	var af *apiFehler
	switch {
	case errors.As(err, &af):
		failW(w, af.status, af.text, af.werte...)
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "nicht gefunden")
	case errors.Is(err, store.ErrLocked):
		fail(w, http.StatusUnauthorized, "falscher code")
	case errors.Is(err, store.ErrNichtErlaubt):
		fail(w, http.StatusForbidden, err.Error())
	case errors.Is(err, store.ErrNameVergeben):
		failCode(w, http.StatusConflict, "name_vergeben", "Diesen Namen gibt es schon. Bitte wähle einen anderen.")
	default:
		fail(w, http.StatusBadRequest, stream.Bereinigt(err.Error()))
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{
			Error: satzAnfang(tr(w, "ungültige anfrage: {fehler}", "fehler", err.Error())), Code: "ungueltige_anfrage"})
		return false
	}
	return true
}

// qSek liest eine Stelle im Film in Sekunden, auch mit Nachkommastellen
// („104.54"). Früher nur ganze Sekunden: Ein Ton- oder Untertitelwechsel
// setzte bis zu einer Sekunde zu früh wieder an.
func qSek(r *http.Request, key string) float64 {
	v := r.URL.Query().Get(key)
	if v == "" {
		return 0
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return 0
	}
	if f > 1e7 {
		f = 1e7
	}
	return math.Round(f*1000) / 1000
}

func qInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
