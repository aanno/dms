package dms

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAcceptsSnapcast(t *testing.T) {
	for accept, want := range map[string]bool{
		"":                                  false,
		"*/*":                               false,
		"audio/x-snapcast-tcp":              true,
		"Audio/X-Snapcast-TCP; q=0.9, */*":  true,
		"audio/flac, audio/x-snapcast-tcp":  true,
		"audio/x-snapcast-tcp;q=0, audio/*": false,
		"audio/x-snapcast-ws":               false,
		"audio/x-snapcast":                  false,
	} {
		r := httptest.NewRequest(http.MethodHead, "/res?path=a.flac", nil)
		if accept != "" {
			r.Header.Set("Accept", accept)
		}
		if got := acceptsSnapcast(r); got != want {
			t.Errorf("Accept %q: got %v, want %v", accept, got, want)
		}
	}
}
