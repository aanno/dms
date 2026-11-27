package dms

// PoC (gstreamer-snapcast-rs, docs/dlna-snapcast-protocol.md): synchronized multiroom
// playback through a Snapcast session, by content negotiation on the ordinary
// resource URL. Control points such as BubbleUPnP pass on only resources of MIME types
// they know, so there is no extra resource in the listing. A renderer that can play
// Snapcast sessions asks for the item's /res URL with
//
//	Accept: audio/x-snapcast-tcp
//
// (HEAD or GET) and gets that Content-Type and, for GET, the session's
// snapcast-tcp:// URI as one line of text. Any other request gets the media as
// before.

import (
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// snapcastTCPType is the MIME type of a Snapcast session reached over TCP (Snapcast's
// binary stream protocol). A WebSocket variant would be audio/x-snapcast-ws.
const snapcastTCPType = "audio/x-snapcast-tcp"

// acceptsSnapcast tells whether the request's Accept header asks for snapcastTCPType
// (with q > 0).
func acceptsSnapcast(r *http.Request) bool {
	for _, field := range r.Header.Values("Accept") {
		for _, part := range strings.Split(field, ",") {
			mt, params, err := mime.ParseMediaType(strings.TrimSpace(part))
			if err != nil || !strings.EqualFold(mt, snapcastTCPType) {
				continue
			}
			if q, err := strconv.ParseFloat(params["q"], 64); err == nil && q == 0 {
				continue // explicitly not acceptable
			}
			return true
		}
	}
	return false
}

// serveSnapcastSession answers a renderer that asks for the Snapcast session of the
// item at filePath (see the top of this file) and reports whether it did. Later this
// is where dms opens the session (starts feeding the item into the Snapcast server).
func (server *Server) serveSnapcastSession(w http.ResponseWriter, r *http.Request, filePath string) bool {
	if server.SnapcastStream == "" || !acceptsSnapcast(r) {
		return false
	}
	if mimeType, err := MimeTypeByPath(server.FS, filePath); err != nil || !mimeType.IsAudio() {
		return false
	}
	uri := strings.ReplaceAll(server.SnapcastStream, "{host}", snapcastHost(r))
	slog.Info("snapcast session requested", "path", r.URL.Query().Get("path"), "method", r.Method, "uri", uri, "remote", r.RemoteAddr)
	w.Header().Set("Content-Type", snapcastTCPType)
	w.Header().Set("Vary", "Accept")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodHead {
		w.Write([]byte(uri + "\n"))
	}
	return true
}

// snapcastHost is the address of this machine for the snapcast-tcp:// URI: the local
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
