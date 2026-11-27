package dms

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChooseSnapcastSession(t *testing.T) {
	const (
		tcp = "snapcast-tcp://{host}:1704/s?control=1705"
		ws  = "snapcast-ws://{host}:1780/s"
	)
	for _, c := range []struct {
		accept   string
		sessions []string
		want     string
	}{
		{"", []string{tcp}, ""},
		{"*/*", []string{tcp}, ""},
		{"audio/*", []string{tcp}, ""},
		{"audio/x-snapcast-tcp", []string{tcp}, tcp},
		{"Audio/X-Snapcast-TCP; q=0.9, */*", []string{tcp}, tcp},
		{"audio/flac, audio/x-snapcast-tcp", []string{tcp}, tcp},
		{"audio/x-snapcast-tcp;q=0, audio/*", []string{tcp}, ""},
		{"audio/x-snapcast-ws", []string{tcp}, ""},
		{"audio/x-snapcast", []string{tcp}, ""},
		// The renderer's preference (q) wins, the server's order breaks ties.
		{"audio/x-snapcast-tcp;q=0.5, audio/x-snapcast-ws", []string{tcp, ws}, ws},
		{"audio/x-snapcast-tcp, audio/x-snapcast-ws;q=0.9", []string{ws, tcp}, tcp},
		{"audio/x-snapcast-tcp, audio/x-snapcast-ws", []string{ws, tcp}, ws},
	} {
		r := httptest.NewRequest(http.MethodHead, "/res?path=a.flac", nil)
		if c.accept != "" {
			r.Header.Set("Accept", c.accept)
		}
		got, mt := chooseSnapcastSession(r, c.sessions)
		if got != c.want {
			t.Errorf("Accept %q, sessions %v: got %q, want %q", c.accept, c.sessions, got, c.want)
		}
		if got != "" && mt != SnapcastMimeType(got) {
			t.Errorf("Accept %q: MIME type %q for %q", c.accept, mt, got)
		}
	}
}
