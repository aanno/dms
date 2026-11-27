package dms

// PoC (gstreamer-snapcast-rs, docs/dlna-snapcast-protocol.md): synchronized multiroom
// playback through a Snapcast session, by content negotiation on the ordinary
// resource URL. Control points such as BubbleUPnP pass on only resources of MIME types
// they know, so there is no extra resource in the listing. A renderer that can play
// Snapcast sessions asks for the item's /res URL (HEAD or GET) with the transports it
// can play, e.g.
//
//	Accept: audio/x-snapcast-tcp, audio/x-snapcast-ws;q=0.9
//
// and gets the chosen transport's MIME type as Content-Type and, for GET, the
// session URI as one line of text. Any other request gets the media as before.

import (
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

// snapcastTransports maps the URI scheme of a Snapcast session to the MIME type that
// names its transport in the negotiation.
var snapcastTransports = map[string]string{
	"snapcast-tcp": "audio/x-snapcast-tcp", // Snapcast's binary protocol
	"snapcast-ws":  "audio/x-snapcast-ws",  // over WebSocket
}

// SnapcastMimeType is the transport's MIME type for a session URI, or "".
func SnapcastMimeType(uri string) string {
	scheme, _, ok := strings.Cut(uri, "://")
	if !ok {
		return ""
	}
	return snapcastTransports[strings.ToLower(scheme)]
}

// acceptQuality is the q value the request's Accept header gives mimeType: 0 if it
// isn't listed explicitly (wildcards don't count: only a renderer that names a
// Snapcast transport gets a session).
func acceptQuality(r *http.Request, mimeType string) float64 {
	best := 0.0
	for _, field := range r.Header.Values("Accept") {
		for _, part := range strings.Split(field, ",") {
			mt, params, err := mime.ParseMediaType(strings.TrimSpace(part))
			if err != nil || !strings.EqualFold(mt, mimeType) {
				continue
			}
			q := 1.0
			if v, ok := params["q"]; ok {
				if q, err = strconv.ParseFloat(v, 64); err != nil {
					continue
				}
			}
			best = max(best, q)
		}
	}
	return best
}

// chooseSnapcastSession picks the session URI (template) for the request: among
// sessions, the one whose transport the request accepts with the highest q; on a
// tie, the earlier one. "" if none is accepted.
func chooseSnapcastSession(r *http.Request, sessions []string) (uri, mimeType string) {
	bestQ := 0.0
	for _, s := range sessions {
		mt := SnapcastMimeType(s)
		if mt == "" {
			continue
		}
		if q := acceptQuality(r, mt); q > bestQ {
			bestQ, uri, mimeType = q, s, mt
		}
	}
	return uri, mimeType
}

// serveSnapcastSession answers a renderer that asks for the Snapcast session of the
// item at filePath (see the top of this file) and reports whether it did. Later this
// is where dms opens the session (starts feeding the item into the Snapcast server).
func (server *Server) serveSnapcastSession(w http.ResponseWriter, r *http.Request, filePath string) bool {
	if len(server.SnapcastStreams) == 0 || r.Header.Get("Accept") == "" {
		return false
	}
	template, mimeType := chooseSnapcastSession(r, server.SnapcastStreams)
	if template == "" {
		return false
	}
	if mt, err := MimeTypeByPath(server.FS, filePath); err != nil || !mt.IsAudio() {
		return false
	}
	uri := strings.ReplaceAll(template, "{host}", snapcastHost(r))
	if strings.Contains(uri, "{stream}") {
		// A session per item: the GET opens it (snapcast_feeder.go). HEAD only asks.
		stream := snapcastStreamName(filePath)
		uri = strings.ReplaceAll(uri, "{stream}", url.PathEscape(stream))
		if r.Method != http.MethodHead && server.SnapcastFeeder != nil {
			// The feeder runs in dms's working directory, not the media root (as
			// ffmpeg does for transcoding).
			absPath, err := filepath.Abs(filepath.Join(server.rootPath, filePath))
			if err == nil {
				err = server.SnapcastFeeder.Open(r.Context(), stream, absPath)
			}
			if err != nil {
				// No session after all: the renderer gets the media instead.
				slog.Error("snapcast session not opened; serving the media", "path", filePath, "stream", stream, "err", err)
				return false
			}
		}
	}
	slog.Info("snapcast session requested", "path", r.URL.Query().Get("path"), "method", r.Method, "uri", uri, "remote", r.RemoteAddr)
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Vary", "Accept")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodHead {
		w.Write([]byte(uri + "\n"))
	}
	return true
}

// snapcastHost is the address of this machine for the session URI: the local
// address of the request, but IPv4 if the request came over IPv6 and the interface
// has an IPv4 address (the Snapcast server may listen on IPv4 only).
func snapcastHost(r *http.Request) string {
	addr, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			return r.Host
		}
		return host
	}
	if ip4 := tcp.IP.To4(); ip4 != nil {
		return ip4.String()
	}
	if ip4 := ipv4OfInterfaceWith(tcp.IP); ip4 != nil {
		return ip4.String()
	}
	return "[" + tcp.IP.String() + "]"
}

func ipv4OfInterfaceWith(ip net.IP) net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		var v4 net.IP
		found := false
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ipnet.IP.Equal(ip) {
				found = true
			}
			if v4 == nil && ipnet.IP.To4() != nil {
				v4 = ipnet.IP.To4()
			}
		}
		if found {
			return v4
		}
	}
	return nil
}
