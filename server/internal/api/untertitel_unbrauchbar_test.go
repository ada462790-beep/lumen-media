package api

import (
	"testing"

	"lumen/internal/store"
)

// Spuren, die FFmpeg weder als Text noch als Bild liefern kann (Teletext,
// Closed Captions, unbekannte „none"-Spuren), bietet Lumen nicht an — und
// versucht auch nicht täglich, sie umzuwandeln.
func TestUnbrauchbareUntertitelWerdenNichtAngeboten(t *testing.T) {
	h := newHarness(t)
	_, fileID := h.film("Spurensuche", 2)
	if err := h.st.SpurenSetzen(t.Context(), fileID, []store.Track{
		{Index: 1, Kind: "audio", Codec: "aac"},
		{Index: 2, Kind: "subtitle", Codec: "subrip", Language: "de"},
		{Index: 3, Kind: "subtitle", Codec: ""},
		{Index: 4, Kind: "subtitle", Codec: "eia_608"},
		{Index: 5, Kind: "subtitle", Codec: "dvb_teletext"},
		{Index: 6, Kind: "subtitle", Codec: "hdmv_pgs_subtitle"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	spuren, err := h.st.Spuren(t.Context(), fileID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int]bool{}
	for _, s := range spuren {
		got[s.Index] = true
	}
	for _, i := range []int{1, 2, 6} {
		if !got[i] {
			t.Errorf("Spur %d fehlt", i)
		}
	}
	for _, i := range []int{3, 4, 5} {
		if got[i] {
			t.Errorf("Spur %d sollte nicht angeboten werden", i)
		}
	}
}
