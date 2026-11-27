package dms

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
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

func TestProcessFeederOpensOnceAndReportsFailures(t *testing.T) {
	dir := t.TempDir()
	starts := dir + "/starts"
	// A stand-in helper: counts its starts, checks its arguments, is ready at once,
	// then lives a while.
	ok := &ProcessFeeder{Command: []string{"sh", "-c",
		`echo "$*" >> ` + starts + `; echo "Session ready: x"; sleep 2`, "helper"}}
	for i := 0; i < 3; i++ {
		if err := ok.Open(context.Background(), "dms-1", "/music/a.flac"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(starts)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "--paused /music/a.flac dms-1\n" {
		t.Errorf("one start with the session's arguments expected, got %q", got)
	}

	failing := &ProcessFeeder{Command: []string{"sh", "-c", "echo broken; exit 3"}}
	if err := failing.Open(context.Background(), "dms-2", "/music/b.flac"); err == nil {
		t.Error("a helper that exits before it is ready must fail Open")
	}
	missing := &ProcessFeeder{Command: []string{dir + "/no-such-helper"}}
	if err := missing.Open(context.Background(), "dms-3", "/music/c.flac"); err == nil {
		t.Error("a missing helper must fail Open")
	}
	slow := &ProcessFeeder{Command: []string{"sh", "-c", "sleep 5"}, ReadyTimeout: 200 * time.Millisecond}
	if err := slow.Open(context.Background(), "dms-4", "/music/d.flac"); err == nil {
		t.Error("a helper that never gets ready must fail Open after the timeout")
	}
}

func TestSnapcastStreamNameIsStablePerItem(t *testing.T) {
	a, b := snapcastStreamName("/music/a.flac"), snapcastStreamName("/music/b.flac")
	if a != snapcastStreamName("/music/a.flac") || a == b || !strings.HasPrefix(a, "dms-") {
		t.Errorf("names %q, %q", a, b)
	}
}
