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

	"github.com/wailsapp/go-webview2/pkg/webview2"
)

type selbsttest struct {
	f       *fenster
	ordner  string
	adresse string // des Schein-Servers

	mu        sync.Mutex
	zeilen    []string
	fehler    int
	schritt   int
	externOK  bool
	brueckeOK bool
	fertig    bool
}

func neuerSelbsttest(f *fenster, ordner string) *selbsttest {
	return &selbsttest{f: f, ordner: ordner}
}

func (t *selbsttest) notiz(ok bool, format string, a ...any) {
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
}

const testSeite = `<!doctype html><html lang="de"><head><meta charset="utf-8"><title>Testserver</title>
<style>body{margin:0;background:#050507;color:#fff;font:16px "Segoe UI",system-ui,sans-serif;display:flex;flex-direction:column;
align-items:center;justify-content:center;height:100vh;gap:14px}h1{font-size:28px;margin:0}p{margin:0;color:rgba(235,235,245,.7)}
.ok{color:#30d158}.nein{color:#ff6961}a{color:#0a84ff}</style></head><body>
<h1>Schein-Server für den Selbsttest</h1><p id="b">…</p><p id="u">…</p>
<p><a id="extern" href="https://www.themoviedb.org/movie/603" target="_blank" rel="noopener">TMDb (soll im Browser aufgehen)</a></p>
<script>
const d = window.lumenDesktop;
const b = document.getElementById('b'), u = document.getElementById('u');
b.textContent = d ? 'Brücke da: lumenDesktop ' + d.version + ', serverWechseln: ' + typeof d.serverWechseln : 'keine Brücke';
b.className = d && typeof d.serverWechseln === 'function' ? 'ok' : 'nein';
const ua = /LumenDesktop\/\S+/.exec(navigator.userAgent);
u.textContent = ua ? 'User-Agent: ' + ua[0] : 'User-Agent ohne LumenDesktop';
u.className = ua ? 'ok' : 'nein';
document.title = (d && ua) ? 'BRUECKE_OK' : 'BRUECKE_FEHLT';
</script></body></html>`

func (t *selbsttest) serverStarten() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	t.adresse = ln.Addr().String()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"server":"Lumen","name":"Selbsttest-Server","version":"0.0.0","setUp":true}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, testSeite)
	})
	go http.Serve(ln, mux)
	return nil
}

func (t *selbsttest) beginnen() {
	t.notiz(true, "Fenster und WebView2 bereit, Schein-Server auf %s", t.adresse)
	// Nach spätestens 90 Sekunden ist Schluss, was auch geschieht.
	time.AfterFunc(90*time.Second, func() {
		t.f.Auftrag(func() {
			t.notiz(false, "Zeit abgelaufen in Schritt %d", t.schritt)
			t.abschliessen()
		})
	})
}

func (t *selbsttest) spaeter(d time.Duration, fn func()) {
	time.AfterFunc(d, func() { t.f.Auftrag(fn) })
}

// geladen: Eine Seite ist fertig geladen. Daran hängen die Schritte.
func (t *selbsttest) geladen(adresse string) {
	log.Printf("Selbsttest: geladen %s", adresse)
	switch {
	case t.schritt == 0 && strings.HasPrefix(adresse, seitenAdresse):
		t.schritt = 1
		t.notiz(true, "eingebaute Seite geladen")
		t.spaeter(1500*time.Millisecond, func() {
			t.bild("1-server-hinzufuegen.png", func() {
				t.f.Skript(fmt.Sprintf(`(()=>{const e=document.getElementById('adresse');e.value=%q;document.getElementById('form').requestSubmit();})()`, t.adresse))
			})
		})
	case t.schritt == 1 && strings.HasPrefix(adresse, "http://"+t.adresse):
		t.schritt = 2
		t.notiz(true, "Server über die eingebaute Seite hinzugefügt und geladen")
		t.spaeter(1500*time.Millisecond, t.webAppPruefen)
	case t.schritt == 4 && strings.HasPrefix(adresse, seitenAdresse):
		t.schritt = 5
		t.notiz(true, "Strg+Umschalt+S zeigt die Serverauswahl")
		t.spaeter(1200*time.Millisecond, func() {
			t.bild("4-serverliste.png", func() {
				t.bildschirm("5-bildschirm-liste.png")
				t.abschliessen()
			})
		})
	}
}

func (t *selbsttest) webAppPruefen() {
	titel, _ := t.f.wv.GetDocumentTitle()
	t.notiz(titel == "BRUECKE_OK", "Brücke und User-Agent in der Web-App (Titel %q)", titel)
	t.bild("2-web-app.png", func() {
		t.bildschirm("2b-bildschirm.png")
		// Ein Link mit target=_blank soll nicht im Fenster aufgehen.
		t.f.Skript(`document.getElementById('extern').click()`)
		t.spaeter(800*time.Millisecond, func() {
			t.mu.Lock()
			ok := t.externOK
			t.mu.Unlock()
			t.notiz(ok, "externer Link geht an den Standardbrowser")
			quelle, _ := t.f.wv.GetSource()
			t.notiz(strings.HasPrefix(quelle, "http://"+t.adresse), "Fenster bleibt auf der Web-App (%s)", quelle)
			t.schritt = 3
			t.f.f11()
			t.spaeter(1200*time.Millisecond, func() {
				t.notiz(t.f.vollbild, "F11 füllt den Bildschirm")
				t.bildschirm("3-vollbild.png")
				t.f.f11()
				t.spaeter(600*time.Millisecond, func() {
					t.notiz(!t.f.vollbild, "F11 beendet das Vollbild")
					t.schritt = 4
					t.f.app.ServerWechseln()
				})
			})
		})
	})
}

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
	h := &bildFertig{fertig: func(code uintptr) {
		defer (*webview2.IStream)(strom).Release()
		if code != 0 {
			t.notiz(false, "Bild %s: Fehler %#x", name, code)
		} else if b := stromInhalt(strom); len(b) == 0 {
			t.notiz(false, "Bild %s: leer", name)
		} else {
			err := os.WriteFile(filepath.Join(t.ordner, name), b, 0o644)
			t.notiz(err == nil, "Bild %s (%d Bytes)", name, len(b))
		}
		weiter()
	}}
	handler := webview2.NewICoreWebView2CapturePreviewCompletedHandler(h)
	bildHandler = append(bildHandler, handler, h)
	if err := t.f.wv.CapturePreview(webview2.COREWEBVIEW2_CAPTURE_PREVIEW_IMAGE_FORMAT_PNG, (*webview2.IStream)(strom), handler); err != nil {
		t.notiz(false, "Bild %s: %v", name, err)
		weiter()
	}
}

// Die Empfänger dürfen nicht eingesammelt werden, solange WebView2 sie hält.
var bildHandler []any

type bildFertig struct{ fertig func(uintptr) }

func (b *bildFertig) QueryInterface(_, _ uintptr) uintptr { return 0 }
func (b *bildFertig) AddRef() uintptr                     { return 1 }
func (b *bildFertig) Release() uintptr                    { return 1 }
func (b *bildFertig) CapturePreviewCompleted(code uintptr) uintptr {
	b.fertig(code)
	return 0
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
