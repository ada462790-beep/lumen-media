// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package stream

import (
	"os"
	"path/filepath"
	"testing"
)

// Nur eine Datei mit HEIF-Kennung gilt als HEIF — eine SVG oder ein
// Skript mit der Endung .heic nicht. Erst dann darf ImageMagick ran.
func TestIstHEIF(t *testing.T) {
	dir := t.TempDir()
	faelle := map[string]struct {
		inhalt []byte
		heif   bool
	}{
		"echt.heic":    {append([]byte{0, 0, 0, 24}, []byte("ftypheic\x00\x00\x00\x00mif1heic")...), true},
		"mif1.heic":    {append([]byte{0, 0, 0, 20}, []byte("ftypmif1\x00\x00\x00\x00heic")...), true},
		"vertraeglich": {append([]byte{0, 0, 0, 24}, []byte("ftypavif\x00\x00\x00\x00mif1miaf")...), true},
		"svg.heic":     {[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><image href="file:///etc/passwd"/></svg>`), false},
		"mp4.heic":     {append([]byte{0, 0, 0, 20}, []byte("ftypisom\x00\x00\x02\x00mp41")...), false},
		"kurz.heic":    {[]byte("ftyp"), false},
		"leer.heic":    {nil, false},
	}
	for name, f := range faelle {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, f.inhalt, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := IstHEIF(p); got != f.heif {
			t.Errorf("%s: %v, erwartet %v", name, got, f.heif)
		}
	}
	if IstHEIF(filepath.Join(dir, "gibtsnicht")) {
		t.Error("fehlende Datei gilt als HEIF")
	}
}
