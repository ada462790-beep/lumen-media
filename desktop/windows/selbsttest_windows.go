// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// Der Selbsttest läuft beim Bauen auf einem Windows-Rechner von GitHub:
//
//	Lumen.exe --selbsttest <ordner>
//
// Er startet einen kleinen Schein-Server (antwortet auf /api/v1/hello
// wie Lumen und liefert eine Testseite), öffnet das Fenster, fügt den
// Server über die eingebaute Seite hinzu, lädt dessen Seite, prüft Brücke,
// User-Agent, externe Links und Vollbild und macht unterwegs Bilder.
// Am Ende stehen im Ordner die Bilder, protokoll.txt und lumen.log;
// der Rückgabewert ist 0, wenn alles stimmte.

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type selbsttest struct {
	f        *fenster
	ordner   string
	web      string // Ordner der echten Web-App (server/cmd/lumen/web), wenn angegeben
	adresse  string // des Schein-Servers
	geladenK chan string

	mu       sync.Mutex
	zeilen   []string
	fehler   int
	externOK bool
	fertig   bool
}

func neuerSelbsttest(f *fenster, ordner, web string) *selbsttest {
	return &selbsttest{f: f, ordner: ordner, web: web, geladenK: make(chan string, 16)}
}

func (t *selbsttest) notiz(ok bool, format string, a ...any) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	zeichen := "ok  "
	if !ok {
		zeichen = "FEHL"
		t.fehler++
	}
	z := zeichen + " " + fmt.Sprintf(format, a...)
	t.zeilen = append(t.zeilen, z)
	log.Print("Selbsttest: " + z)
	return ok
}

const testSeite = `<!doctype html><html lang="de"><head><meta charset="utf-8"><title>Testserver</title>
<style>body{margin:0;background:#050507;color:#fff;font:16px "Segoe UI",system-ui,sans-serif;display:flex;flex-direction:column;
align-items:center;justify-content:center;height:100vh;gap:14px}h1{font-size:28px;margin:0}p{margin:0;color:rgba(235,235,245,.7)}
.ok{color:#30d158}.nein{color:#ff6961}a{color:#0a84ff}</style></head><body>
<h1>Schein-Server für den Selbsttest</h1><p id="b">…</p><p id="u">…</p><p id="m">…</p>
<p><a id="extern" href="https://www.themoviedb.org/movie/603" target="_blank" rel="noopener">TMDb (soll im Browser aufgehen)</a></p>
<p><button id="vollbild" style="font:inherit;padding:8px 16px;border-radius:999px;border:0">Vollbild wie im Player</button></p>
<script>
const d = window.lumenDesktop;
const b = document.getElementById('b'), u = document.getElementById('u'), m = document.getElementById('m');
b.textContent = d ? 'Brücke da: lumenDesktop ' + d.version + ', serverWechseln: ' + typeof d.serverWechseln : 'keine Brücke';
b.className = d && typeof d.serverWechseln === 'function' ? 'ok' : 'nein';
const ua = /LumenDesktop\/\S+/.exec(navigator.userAgent);
u.textContent = ua ? 'User-Agent: ' + ua[0] : 'User-Agent ohne LumenDesktop';
u.className = ua ? 'ok' : 'nein';
m.textContent = 'navigator.mediaSession: ' + ('mediaSession' in navigator ? 'da' : 'fehlt');
m.className = 'mediaSession' in navigator ? 'ok' : 'nein';
document.title = (d && ua) ? 'BRUECKE_OK' : 'BRUECKE_FEHLT';
document.getElementById('vollbild').addEventListener('click', () => document.documentElement.requestFullscreen());
</script></body></html>`

func (t *selbsttest) serverStarten() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	t.adresse = ln.Addr().String()
	mux := http.NewServeMux()
	json := func(w http.ResponseWriter, status int, text string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, text)
	}
	mux.HandleFunc("/api/v1/hello", func(w http.ResponseWriter, r *http.Request) {
		json(w, 200, `{"server":"Lumen","name":"Selbsttest-Server","version":"0.0.0","setUp":true,"sprache":"de-CH","license":"AGPL-3.0-only"}`)
	})
	mux.HandleFunc("/api/v1/profiles", func(w http.ResponseWriter, r *http.Request) {
		json(w, 200, `[{"id":"a","name":"Anna","lockKind":"none","avatar":"av-b"},{"id":"b","name":"Ben","lockKind":"pin","avatar":"av-c"},{"id":"k","name":"Kino","lockKind":"none","isKid":true,"avatar":"fig-stern"}]`)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		json(w, 401, `{"error":"nicht angemeldet"}`)
	})
	mux.HandleFunc("/selbsttest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, testSeite)
	})
	if t.web != "" {
		mux.Handle("/", http.FileServer(http.Dir(t.web)))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/selbsttest", http.StatusFound)
		})
	}
	go http.Serve(ln, mux)
	return nil
}

// ---------------------------------------------------------------- Ablauf
//
// Der Ablauf läuft nebenher und schiebt jeden Schritt, der das Fenster
// anfasst, in den Fenster-Thread (ui) — wie ein Mensch, der klickt und
// wartet.

func (t *selbsttest) beginnen() {
	t.notiz(true, "Fenster und WebView2 bereit, Schein-Server auf %s", t.adresse)
	go func() {
		ende := time.AfterFunc(120*time.Second, func() {
			t.notiz(false, "Zeit abgelaufen")
			t.ui(t.abschliessen)
		})
		defer ende.Stop()
		t.ablauf()
		t.ui(t.abschliessen)
	}()
}

func (t *selbsttest) ui(fn func()) {
	fertig := make(chan struct{})
	t.f.Auftrag(func() { fn(); close(fertig) })
	<-fertig
}

func (t *selbsttest) geladen(adresse string) {
	log.Printf("Selbsttest: geladen %s", adresse)
	select {
	case t.geladenK <- adresse:
	default:
	}
}

// warteGeladen wartet, bis eine Seite mit diesem Anfang fertig ist.
func (t *selbsttest) warteGeladen(anfang string) bool {
	frist := time.After(20 * time.Second)
	for {
		select {
		case a := <-t.geladenK:
			if strings.HasPrefix(a, anfang) {
				return true
			}
		case <-frist:
			return false
		}
	}
}

func (t *selbsttest) skript(js string) { t.ui(func() { t.f.Skript(js) }) }

func (t *selbsttest) titel() string {
	var s string
	t.ui(func() { s, _ = comText(t.f.wv, wvGetDocumentTitle) })
	return s
}

// frage legt das Ergebnis eines Ausdrucks in den Titel der Seite und
// liest es von dort (ExecuteScript mit Ergebnis bietet edge nicht an).
func (t *selbsttest) frage(ausdruck string) string {
	t.skript(`document.title='ERG:'+String(` + ausdruck + `)`)
	time.Sleep(400 * time.Millisecond)
	return strings.TrimPrefix(t.titel(), "ERG:")
}

// klick klickt wirklich mit der Maus auf ein Element: Vollbild verlangt
// eine echte Geste.
func (t *selbsttest) klick(selektor string) bool {
	pos := t.frage(fmt.Sprintf(`(()=>{const e=document.querySelector(%q);if(!e)return 'nichts';const r=e.getBoundingClientRect();`+
		`return Math.round((r.left+r.width/2)*devicePixelRatio)+','+Math.round((r.top+r.height/2)*devicePixelRatio)})()`, selektor))
	var x, y int32
	if _, err := fmt.Sscanf(pos, "%d,%d", &x, &y); err != nil {
		return t.notiz(false, "%s nicht gefunden (%q)", selektor, pos)
	}
	t.ui(func() {
		p := punkt{x, y}
		pClientToScreen.Call(t.f.hwnd, uintptr(unsafe.Pointer(&p)))
		pSetForegroundWindow.Call(t.f.hwnd)
		pSetCursorPos.Call(uintptr(p.X), uintptr(p.Y))
		const links, losgelassen = 0x0002, 0x0004
		pMouseEvent.Call(links, 0, 0, 0, 0)
		pMouseEvent.Call(losgelassen, 0, 0, 0, 0)
	})
	return true
}

func (t *selbsttest) bildWarte(name string) {
	fertig := make(chan struct{})
	t.f.Auftrag(func() { t.bild(name, func() { close(fertig) }) })
	select {
	case <-fertig:
	case <-time.After(10 * time.Second):
		t.notiz(false, "Bild %s: keine Antwort", name)
	}
}

func (t *selbsttest) bildschirmWarte(name string) { t.ui(func() { t.bildschirm(name) }) }

func (t *selbsttest) ablauf() {
	server := "http://" + t.adresse

	// 1. Erster Start: die eingebaute Seite.
	if !t.notiz(t.warteGeladen(seitenAdresse), "eingebaute Seite geladen") {
		return
	}
	time.Sleep(1200 * time.Millisecond)
	t.bildWarte("1-server-hinzufuegen.png")

	// 2. Adresse eintippen wie ein Mensch (ohne http://), verbinden.
	t.skript(fmt.Sprintf(`(()=>{const e=document.getElementById('adresse');e.value=%q;document.getElementById('form').requestSubmit();})()`, t.adresse))
	if !t.notiz(t.warteGeladen(server), "Server über die eingebaute Seite hinzugefügt, Web-App geladen") {
		return
	}

	// 3. Die echte Web-App: „Wer schaut?" mit „Server wechseln".
	if t.web != "" {
		time.Sleep(3 * time.Second)
		t.bildWarte("2-web-app.png")
		t.notiz(t.frage(`document.querySelectorAll('[data-desktop-server]').length`) == "1", "Web-App zeigt „Server wechseln“")
		t.bildschirmWarte("2b-bildschirm.png")
		if t.klick("[data-desktop-server]") {
			t.notiz(t.warteGeladen(seitenAdresse+"?wechseln=1"), "„Server wechseln“ in der Web-App öffnet die Auswahl")
		}
		time.Sleep(800 * time.Millisecond)
	}

	// 4. Prüfseite: Brücke, User-Agent, Update-Hinweis, Links, Vollbild.
	t.ui(func() { t.f.Navigieren(server + "/selbsttest") })
	if !t.notiz(t.warteGeladen(server+"/selbsttest"), "Prüfseite geladen") {
		return
	}
	time.Sleep(800 * time.Millisecond)
	titel := t.titel()
	t.notiz(titel == "BRUECKE_OK", "Brücke und User-Agent (Titel %q)", titel)
	t.skript(`lumenDesktop._update({verfuegbar:true,version:'9.9.9',phase:'angeboten'})`)
	time.Sleep(600 * time.Millisecond)
	t.bildWarte("3-pruefseite-update-hinweis.png")
	t.skript(`lumenDesktop._update(null)`)

	t.skript(`document.getElementById('extern').click()`)
	time.Sleep(800 * time.Millisecond)
	t.mu.Lock()
	ok := t.externOK
	t.mu.Unlock()
	t.notiz(ok, "externer Link geht an den Standardbrowser")
	var quelle string
	t.ui(func() { quelle, _ = comText(t.f.wv, wvGetSource) })
	t.notiz(strings.HasPrefix(quelle, server), "Fenster bleibt auf der Web-App (%s)", quelle)

	t.ui(t.f.f11)
	time.Sleep(1200 * time.Millisecond)
	t.notiz(t.f.vollbild, "F11 füllt den Bildschirm")
	t.bildschirmWarte("4-f11-vollbild.png")
	t.ui(t.f.f11)
	time.Sleep(800 * time.Millisecond)
	t.notiz(!t.f.vollbild, "F11 beendet das Vollbild")

	if t.klick("#vollbild") {
		time.Sleep(1500 * time.Millisecond)
		t.notiz(t.f.domVollbild && t.f.vollbild, "requestFullscreen (wie im Player) füllt den Bildschirm")
		t.bildschirmWarte("5-player-vollbild.png")
		t.skript(`document.fullscreenElement&&document.exitFullscreen()`)
		time.Sleep(1200 * time.Millisecond)
		t.notiz(!t.f.domVollbild && !t.f.vollbild, "exitFullscreen gibt das Fenster zurück")
	}

	// 5. Strg+Umschalt+S: die Auswahl mit dem gemerkten Server.
	t.ui(t.f.app.ServerWechseln)
	if t.notiz(t.warteGeladen(seitenAdresse+"?wechseln=1"), "Serverauswahl über das Tastenkürzel") {
		time.Sleep(1200 * time.Millisecond)
		t.bildWarte("6-serverliste.png")
		t.bildschirmWarte("7-bildschirm-liste.png")
	}
}

var (
	pClientToScreen = user32.NewProc("ClientToScreen")
	pSetCursorPos   = user32.NewProc("SetCursorPos")
	pMouseEvent     = user32.NewProc("mouse_event")
)

func (t *selbsttest) extern(adresse string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if strings.Contains(adresse, "themoviedb.org") {
		t.externOK = true
	}
	log.Printf("Selbsttest: extern %s", adresse)
}

func (t *selbsttest) abschliessen() {
	if t.fertig {
		return
	}
	t.fertig = true
	server, _ := t.f.app.liste.Kopie()
	t.notiz(len(server) == 1 && server[0].Name == "Selbsttest-Server", "Serverliste gespeichert (%d Einträge)", len(server))
	t.mu.Lock()
	text := strings.Join(t.zeilen, "\r\n") + "\r\n"
	t.mu.Unlock()
	os.WriteFile(filepath.Join(t.ordner, "protokoll.txt"), []byte(text), 0o644)
	t.f.Schliessen()
}

func (t *selbsttest) ergebnis() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.fehler > 0 || !t.fertig {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------- Bilder

// bild hält fest, was WebView2 zeigt (CapturePreview), und ruft danach
// weiter auf.
func (t *selbsttest) bild(name string, weiter func()) {
	var strom unsafe.Pointer
	if r, _, _ := pCreateStreamOnHGlob.Call(0, 1, uintptr(unsafe.Pointer(&strom))); r != 0 || strom == nil {
		t.notiz(false, "Bild %s: kein Speicher", name)
		weiter()
		return
	}
	h := fertig(func(code uintptr) {
		defer comAufruf(com(strom), 2) // Release
		if code != 0 {
			t.notiz(false, "Bild %s: Fehler %#x", name, code)
		} else if b := stromInhalt(strom); len(b) == 0 {
			t.notiz(false, "Bild %s: leer", name)
		} else {
			err := os.WriteFile(filepath.Join(t.ordner, name), b, 0o644)
			t.notiz(err == nil, "Bild %s (%d Bytes)", name, len(b))
		}
		weiter()
	})
	// COREWEBVIEW2_CAPTURE_PREVIEW_IMAGE_FORMAT_PNG = 0
	if hr := comAufruf(t.f.wv, wvCapturePreview, 0, uintptr(strom), uintptr(unsafe.Pointer(h))); hr != 0 {
		t.notiz(false, "Bild %s: Fehler %#x", name, hr)
		comAufruf(com(strom), 2)
		weiter()
	}
}

// stromInhalt liest einen IStream aus CreateStreamOnHGlobal: so viele
// Bytes, wie geschrieben wurden (Position nach dem Schreiben).
func stromInhalt(strom unsafe.Pointer) []byte {
	vt := *(**[16]uintptr)(strom)
	var pos uint64
	// IStream::Seek(0, STREAM_SEEK_CUR, &pos)
	if r, _, _ := syscall.SyscallN(vt[5], uintptr(strom), 0, 1, uintptr(unsafe.Pointer(&pos))); r != 0 || pos == 0 {
		return nil
	}
	var hglobal uintptr
	if r, _, _ := pGetHGlobalFromStream.Call(uintptr(strom), uintptr(unsafe.Pointer(&hglobal))); r != 0 {
		return nil
	}
	p, _, _ := pGlobalLock.Call(hglobal)
	if p == 0 {
		return nil
	}
	defer pGlobalUnlock.Call(hglobal)
	quelle := unsafe.Slice(*(**byte)(unsafe.Pointer(&p)), int(pos))
	return bytes.Clone(quelle)
}

// bildschirm macht ein Bild des ganzen Bildschirms — mit Rahmen,
// Titelleiste und Symbol, die CapturePreview nicht sieht.
func (t *selbsttest) bildschirm(name string) {
	img, err := bildschirmfoto()
	if err != nil {
		t.notiz(false, "Bildschirm %s: %v", name, err)
		return
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	err = os.WriteFile(filepath.Join(t.ordner, name), buf.Bytes(), 0o644)
	t.notiz(err == nil, "Bildschirm %s (%dx%d)", name, img.Bounds().Dx(), img.Bounds().Dy())
}

var (
	pGetDC                  = user32.NewProc("GetDC")
	pReleaseDC              = user32.NewProc("ReleaseDC")
	pCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pGetDIBits              = gdi32.NewProc("GetDIBits")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pDeleteDC               = gdi32.NewProc("DeleteDC")
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

func bildschirmfoto() (image.Image, error) {
	const smCxScreen, smCyScreen = 0, 1
	w, _, _ := pGetSystemMetrics.Call(smCxScreen)
	h, _, _ := pGetSystemMetrics.Call(smCyScreen)
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("kein Bildschirm")
	}
	quelle, _, _ := pGetDC.Call(0)
	if quelle == 0 {
		return nil, fmt.Errorf("GetDC")
	}
	defer pReleaseDC.Call(0, quelle)
	ziel, _, _ := pCreateCompatibleDC.Call(quelle)
	defer pDeleteDC.Call(ziel)
	bmp, _, _ := pCreateCompatibleBitmap.Call(quelle, w, h)
	defer pDeleteObject.Call(bmp)
	alt, _, _ := pSelectObject.Call(ziel, bmp)
	const srcCopy, captureBlt = 0x00CC0020, 0x40000000
	pBitBlt.Call(ziel, 0, 0, w, h, quelle, 0, 0, srcCopy|captureBlt)
	pSelectObject.Call(ziel, alt)

	bi := bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	pix := make([]byte, int(w)*int(h)*4)
	if r, _, _ := pGetDIBits.Call(ziel, bmp, 0, h, uintptr(unsafe.Pointer(&pix[0])), uintptr(unsafe.Pointer(&bi)), 0); r == 0 {
		return nil, fmt.Errorf("GetDIBits")
	}
	img := image.NewNRGBA(image.Rect(0, 0, int(w), int(h)))
	for i := 0; i < len(pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = pix[i+2], pix[i+1], pix[i], 255
	}
	return img, nil
}
