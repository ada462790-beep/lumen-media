// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package relay

import (
	"bufio"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

// Vermittler läuft auf einer Maschine mit öffentlicher Adresse und bringt
// Besucher mit dem Heimserver zusammen.
//
// Er hält genau eine Leitung zu genau einem Heimserver. Das ist Absicht:
// wer mehrere Haushalte verbinden will, startet mehrere Vermittler auf
// verschiedenen Ports. Ein Vermittler, der mehrere Haushalte kennt, müsste
// zwischen ihnen unterscheiden — also mitlesen, wohin etwas gehört.
type Vermittler struct {
	Geheimnis string
	Log       *slog.Logger

	// Grenzen gegen Überlastung. Der Besucherport steht offen im Netz, und
	// jeder Besucher kostet hier zwei Verbindungen und zu Hause zwei
	// weitere. Ohne Obergrenze könnte jemand ohne jedes Geheimnis so lange
	// Verbindungen öffnen, bis Speicher oder Dateideskriptoren alle sind —
	// hier und beim Heimserver. Null heisst jeweils: die Vorgabe.
	MaxBesucher    int           // gleichzeitig offene Besucher insgesamt
	MaxJeAdresse   int           // davon von derselben Adresse
	MaxAnmeldungen int           // gleichzeitige Händedrücke am Tunnelport
	Ruhefrist      time.Duration // Stille, nach der ein Paar geschlossen wird

	mu       sync.Mutex
	leitung  *Leitung
	wartende map[string]chan net.Conn

	besucher    platzwart
	anmeldungen platzwart

	// zaehler sind die einzigen Zahlen, die der Vermittler führt: wie oft
	// jemand da war und wie viele Bytes durchliefen. Nicht wer, nicht was.
	zaehler Zaehler
}

// Zaehler ist alles, was sich über den Betrieb sagen lässt.
type Zaehler struct {
	Besuche      int64
	Offen        int64
	Abgewiesen   int64 // wegen voller Plätze gar nicht erst angenommen
	BytesHin     int64
	BytesZurueck int64
	Verbunden    bool
	Seit         time.Time
}

// Leitung ist die eine lange Verbindung zum Heimserver.
type Leitung struct {
	conn net.Conn
	r    *bufio.Reader
	mu   sync.Mutex // schützt das Schreiben: OEFFNE und PULS kommen aus
	// verschiedenen Goroutinen und dürfen sich nicht verschränken
	puls chan struct{}
	zu   chan struct{}
	once sync.Once
}

// Vorgaben für die Grenzen. 256 Besucher sind für einen Haushalt reichlich
// — ein Gerät öffnet selten mehr als eine Handvoll Verbindungen —, und 32
// je Adresse lassen auch eine Familie hinter einem gemeinsamen Anschluss
// durch, aber nicht einen Einzelnen alle Plätze belegen.
const (
	VorgabeMaxBesucher    = 256
	VorgabeMaxJeAdresse   = 32
	VorgabeMaxAnmeldungen = 128
)

func NeuerVermittler(geheimnis string, log *slog.Logger) (*Vermittler, error) {
	if geheimnis == "" {
		return nil, ErrGeheimnisFehlt
	}
	if log == nil {
		log = slog.Default()
	}
	return &Vermittler{
		Geheimnis: geheimnis,
		Log:       log,
		wartende:  map[string]chan net.Conn{},
	}, nil
}

func (v *Vermittler) Stand() Zaehler {
	v.mu.Lock()
	defer v.mu.Unlock()
	z := v.zaehler
	z.Verbunden = v.leitung != nil
	return z
}

// ---------------------------------------------------------------- Plätze

// platzwart zählt belegte Plätze, insgesamt und je Adresse. Geschützt
// durch Vermittler.mu.
type platzwart struct {
	offen int
	je    map[string]int
}

func (p *platzwart) belegen(adr string, gesamt, jeAdresse int) bool {
	if p.offen >= gesamt || p.je[adr] >= jeAdresse {
		return false
	}
	if p.je == nil {
		p.je = map[string]int{}
	}
	p.offen++
	p.je[adr]++
	return true
}

func (p *platzwart) freigeben(adr string) {
	p.offen--
	if p.je[adr]--; p.je[adr] <= 0 {
		// Austragen, sonst wüchse die Liste mit jeder Adresse, die je da
		// war — auch das wäre ein Weg, Speicher zu füllen.
		delete(p.je, adr)
	}
}

func oder[T int | time.Duration](wert, vorgabe T) T {
	if wert > 0 {
		return wert
	}
	return vorgabe
}

func (v *Vermittler) ruhefrist() time.Duration { return oder(v.Ruhefrist, Ruhefrist) }

// adresseVon ist der Schlüssel für die Grenze je Adresse. Bei IPv6 zählt
// das ganze /64: so viele Adressen bekommt jeder Anschluss ohnehin, eine
// Grenze je Einzeladresse wäre dort keine.
func adresseVon(c net.Conn) string {
	s := c.RemoteAddr().String()
	if rechner, _, err := net.SplitHostPort(s); err == nil {
		s = rechner
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return s
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// ---------------------------------------------------------------- Tunnelseite

// TunnelAnnehmen behandelt eine Verbindung vom Heimserver. Sie kehrt erst
// zurück, wenn die Verbindung vorbei ist.
func (v *Vermittler) TunnelAnnehmen(c net.Conn) {
	r := bufio.NewReader(c)

	a, err := v.anmelden(c, r)
	if err != nil {
		// Eine Fehlermeldung an jemanden, der kein Lumen spricht, sagt nur
		// aus, was hier läuft. Also still schliessen.
		_ = c.Close()
		return
	}
	if !GeheimnisStimmt(v.Geheimnis, a.Geheimnis) {
		_ = Ablehnen(c, "Geheimnis stimmt nicht")
		_ = c.Close()
		v.Log.Warn("Tunnel abgewiesen — falsches Geheimnis")
		return
	}

	switch a.Rolle {
	case Kontrolle:
		v.leitungFuehren(c, r)
	case Daten:
		v.datenwegUebergeben(c, a.Kennung)
	default:
		_ = c.Close()
	}
}

// anmelden liest den Händedruck, aber nur, wenn ein Platz frei ist. Bis das
// Geheimnis geprüft ist, kann am Tunnelport jeder anklopfen; jeder solche
// Versuch hält bis zu Begruessungsfrist lang eine Verbindung samt
// TLS-Zustand. Die Grenze gilt nur für diese Zeit — wer sich ausgewiesen
// hat, gibt seinen Platz sofort wieder frei.
func (v *Vermittler) anmelden(c net.Conn, r *bufio.Reader) (Anmeldung, error) {
	adr := adresseVon(c)
	v.mu.Lock()
	ok := v.anmeldungen.belegen(adr,
		oder(v.MaxAnmeldungen, VorgabeMaxAnmeldungen),
		oder(v.MaxJeAdresse, VorgabeMaxJeAdresse))
	if !ok {
		v.zaehler.Abgewiesen++
	}
	v.mu.Unlock()
	if !ok {
		return Anmeldung{}, errVoll
	}
	defer func() {
		v.mu.Lock()
		v.anmeldungen.freigeben(adr)
		v.mu.Unlock()
	}()
	return AnmeldungLesen(c, r)
}

var errVoll = errors.New("alle plätze belegt")

func (v *Vermittler) leitungFuehren(c net.Conn, r *bufio.Reader) {
	l := &Leitung{conn: c, r: r, puls: make(chan struct{}, 1), zu: make(chan struct{})}

	v.mu.Lock()
	alt := v.leitung
	v.leitung = l
	v.zaehler.Seit = time.Now()
	v.mu.Unlock()

	if alt != nil {
		// Der Heimserver hat neu gewählt, ohne dass wir das Ende der alten
		// Leitung bemerkt haben — typisch nach einem Anschlusswechsel.
		alt.schliessen()
		v.Log.Info("Leitung ersetzt — der Heimserver hat neu gewählt")
	}
	if err := Bereit(c); err != nil {
		l.schliessen()
		return
	}
	v.Log.Info("Heimserver verbunden")

	go v.pulsSchlagen(l)
	l.lesen(v.Log)

	v.mu.Lock()
	if v.leitung == l {
		v.leitung = nil
	}
	v.mu.Unlock()
	v.Log.Info("Heimserver weg — Besucher kommen jetzt nicht durch")
}

// lesen wartet auf die Antworten des Heimservers. Mehr kommt von dort auf
// der Leitung nicht: sie trägt keine Nutzdaten.
func (l *Leitung) lesen(log *slog.Logger) {
	defer l.schliessen()
	for {
		_ = l.conn.SetReadDeadline(time.Now().Add(Pulsfrist))
		zeile, err := zeileLesen(l.r)
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Debug("Leitung zu Ende", "grund", err)
			}
			return
		}
		if zeile == "PULS" {
			select {
			case l.puls <- struct{}{}:
			default:
			}
		}
	}
}

func (v *Vermittler) pulsSchlagen(l *Leitung) {
	t := time.NewTicker(Pulsabstand)
	defer t.Stop()
	for {
		select {
		case <-l.zu:
			return
		case <-t.C:
			if err := l.senden("PULS"); err != nil {
				l.schliessen()
				return
			}
			select {
			case <-l.puls:
			case <-time.After(Pulsfrist):
				v.Log.Info("Keine Antwort auf den Puls — Leitung gilt als tot")
				l.schliessen()
				return
			case <-l.zu:
				return
			}
		}
	}
}

func (l *Leitung) senden(zeile string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.conn.SetWriteDeadline(time.Now().Add(Begruessungsfrist))
	return zeileSchreiben(l.conn, "%s", zeile)
}

func (l *Leitung) schliessen() {
	l.once.Do(func() {
		close(l.zu)
		_ = l.conn.Close()
	})
}

// datenwegUebergeben reicht eine frisch gewählte Datenverbindung an den
// Besucher weiter, der darauf wartet.
func (v *Vermittler) datenwegUebergeben(c net.Conn, kennung string) {
	v.mu.Lock()
	warte, ok := v.wartende[kennung]
	if ok {
		delete(v.wartende, kennung)
	}
	v.mu.Unlock()

	if !ok {
		// Zu spät, oder eine Kennung, die es nie gab.
		_ = c.SetWriteDeadline(time.Now().Add(Begruessungsfrist))
		_ = Ablehnen(c, "zu dieser Kennung wartet niemand")
		_ = c.Close()
		return
	}
	// Ab hier ist der Eintrag aus wartende entfernt, und der Besucher
	// verlässt sich darauf, dass auf warte genau eine Antwort kommt — auch
	// wenn es schiefgeht, dann eben nil. Siehe aufgeben.
	_ = c.SetWriteDeadline(time.Now().Add(Begruessungsfrist))
	if err := Bereit(c); err != nil {
		_ = c.Close()
		warte <- nil
		return
	}
	_ = c.SetWriteDeadline(time.Time{})
	warte <- c
}

// ---------------------------------------------------------------- Besucherseite

// BesucherAnnehmen holt für einen Besucher einen Datenweg zum Heimserver
// und reicht beide aneinander durch.
func (v *Vermittler) BesucherAnnehmen(besucher net.Conn) {
	defer func() { _ = besucher.Close() }()

	adr := adresseVon(besucher)
	v.mu.Lock()
	frei := v.besucher.belegen(adr,
		oder(v.MaxBesucher, VorgabeMaxBesucher),
		oder(v.MaxJeAdresse, VorgabeMaxJeAdresse))
	if !frei {
		// Still schliessen, nichts protokollieren: wer die Plätze flutet,
		// soll damit nicht auch noch das Protokoll fluten. Die Zahl steht
		// auf der Statusseite.
		v.zaehler.Abgewiesen++
		v.mu.Unlock()
		return
	}
	v.zaehler.Besuche++
	v.zaehler.Offen++
	v.mu.Unlock()
	defer func() {
		v.mu.Lock()
		v.zaehler.Offen--
		v.besucher.freigeben(adr)
		v.mu.Unlock()
	}()

	// Erst muss der Besucher etwas sagen, dann erst wird zu Hause
	// angeklopft. Lumen spricht TLS, und dort redet der Besucher zuerst;
	// wer nur verbindet und schweigt, ist kein Gerät, sondern jemand, der
	// Plätze belegen will. Ohne diese Reihenfolge baute der Heimserver für
	// jede stumme Verbindung zwei eigene auf.
	erste, err := ersteBytes(besucher)
	if err != nil {
		return
	}

	v.mu.Lock()
	l := v.leitung
	v.mu.Unlock()

	if l == nil {
		// Ohne Heimserver gibt es nichts durchzureichen. Was hier
		// hinausgeht, ist eine knappe HTTP-Antwort — wer per Browser kommt,
		// soll nicht auf eine leere Seite starren.
		v.keinHeimserver(besucher)
		return
	}

	kennung := rand.Text()
	warte := make(chan net.Conn, 1)

	v.mu.Lock()
	v.wartende[kennung] = warte
	v.mu.Unlock()

	if err := l.senden("OEFFNE " + kennung); err != nil {
		v.aufgeben(kennung, warte)
		l.schliessen()
		return
	}

	var weg net.Conn
	select {
	case weg = <-warte:
	case <-time.After(Abholfrist):
		v.aufgeben(kennung, warte)
		v.Log.Warn("Heimserver hat den Datenweg nicht aufgebaut")
		return
	case <-l.zu:
		v.aufgeben(kennung, warte)
		return
	}
	if weg == nil {
		return
	}
	defer func() { _ = weg.Close() }()

	// Was schon gelesen wurde, zuerst nachreichen.
	_ = weg.SetWriteDeadline(time.Now().Add(Begruessungsfrist))
	if _, err := weg.Write(erste); err != nil {
		return
	}

	hin, zurueck := durchreichen(besucher, weg, v.ruhefrist())
	zurueck += int64(len(erste))

	v.mu.Lock()
	v.zaehler.BytesHin += hin
	v.zaehler.BytesZurueck += zurueck
	v.mu.Unlock()
}

// ersteBytes wartet höchstens Begruessungsfrist auf das, was der Besucher
// als Erstes schickt — bei TLS das ClientHello.
func ersteBytes(c net.Conn) ([]byte, error) {
	_ = c.SetReadDeadline(time.Now().Add(Begruessungsfrist))
	defer func() { _ = c.SetReadDeadline(time.Time{}) }()
	puffer := make([]byte, 4096)
	n, err := c.Read(puffer)
	if n == 0 {
		if err == nil {
			err = io.ErrNoProgress
		}
		return nil, err
	}
	return puffer[:n], nil
}

// aufgeben nimmt einen Besucher aus der Warteliste, der nicht mehr warten
// will. Hat der Heimserver seinen Datenweg schon gemeldet — der Eintrag ist
// dann bereits fort —, ist dieser Weg unterwegs und kommt gleich auf warte
// an. Dann muss er hier geschlossen werden; sonst bliebe eine angemeldete
// Verbindung für immer liegen, und jeder zu spät gekommene Datenweg wäre
// eine mehr.
func (v *Vermittler) aufgeben(kennung string, warte chan net.Conn) {
	v.mu.Lock()
	_, wartet := v.wartende[kennung]
	delete(v.wartende, kennung)
	v.mu.Unlock()
	if wartet {
		return
	}
	if weg := <-warte; weg != nil {
		_ = weg.Close()
	}
}

func (v *Vermittler) keinHeimserver(c net.Conn) {
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	const leib = "Der Heimserver ist gerade nicht verbunden.\n"
	_, _ = c.Write([]byte(
		"HTTP/1.1 502 Bad Gateway\r\n" +
			"Content-Type: text/plain; charset=utf-8\r\n" +
			"Content-Length: " + itoa(len(leib)) + "\r\n" +
			"Connection: close\r\n\r\n" + leib))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// ---------------------------------------------------------------- Lauschen

// Lauschen nimmt beide Seiten entgegen, bis stop geschlossen wird.
//
// tunnel trägt TLS, damit das Geheimnis nicht im Klartext übers Netz geht.
// besucher ist roh: was dort ankommt, ist schon verschlüsselt — der
// Vermittler soll es gerade nicht aufmachen.
func (v *Vermittler) Lauschen(tunnel, besucher net.Listener, stop <-chan struct{}) {
	var wg sync.WaitGroup
	annehmen := func(l net.Listener, f func(net.Conn)) {
		defer wg.Done()
		pause := time.Duration(0)
		for {
			c, err := l.Accept()
			if err != nil {
				select {
				case <-stop:
					return
				default:
				}
				if errors.Is(err, net.ErrClosed) {
					return
				}
				// Alles andere gilt als vorübergehend — allen voran „zu
				// viele offene Dateien". Früher endete hier die Schleife,
				// und wer einmal alle Deskriptoren belegt hatte, hatte den
				// Port für immer zu. Jetzt wird kurz gewartet, wachsend wie
				// bei net/http, und weiter angenommen.
				pause = min(max(2*pause, 5*time.Millisecond), time.Second)
				select {
				case <-stop:
					return
				case <-time.After(pause):
				}
				continue
			}
			pause = 0
			if t, ok := c.(*net.TCPConn); ok {
				_ = t.SetKeepAlive(true)
				_ = t.SetKeepAlivePeriod(2 * time.Minute)
			}
			go f(c)
		}
	}
	wg.Add(2)
	go annehmen(tunnel, v.TunnelAnnehmen)
	go annehmen(besucher, v.BesucherAnnehmen)

	<-stop
	_ = tunnel.Close()
	_ = besucher.Close()
	wg.Wait()

	v.mu.Lock()
	l := v.leitung
	v.leitung = nil
	v.mu.Unlock()
	if l != nil {
		l.schliessen()
	}
}

// TLSAufbau ist die Einstellung für den Tunnelport. Nur neuere Fassungen,
// damit nichts auf etwas Altes zurückfällt.
func TLSAufbau(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
}
