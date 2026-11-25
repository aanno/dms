package dms

// PoC (gstreamer-snapcast-rs, docs/dlna.md): synchronized multiroom playback through a
// Snapcast session. Control points such as BubbleUPnP don't pass resources with a
// protocol other than http-get on to renderers, so the session is offered as an
// http-get resource of its own MIME type (audio/x-snapcast). A renderer that knows
// the type fetches the URL and gets the snapcast:// URI of the session to play.

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/anacrolix/dms/upnpav"
)

const snapcastPath = "/snapcast"

// snapcastResource is the Snapcast resource of an audio item (see
// Server.SnapcastStream). host is the host the client reached dms by.
func snapcastResource(host, path, format, duration string) upnpav.Resource {
	return upnpav.Resource{
		URL: (&url.URL{
			Scheme:   "http",
			Host:     host,
			Path:     snapcastPath,
			RawQuery: url.Values{"path": {path}}.Encode(),
		}).String(),
		ProtocolInfo: "http-get:*:" + format + ":*",
		Duration:     duration,
	}
}

// serveSnapcast answers a renderer's request for an item's Snapcast session with the
// session's snapcast:// URI (text, one line). Later this is where dms opens the
// session (starts feeding the item into the Snapcast server).
func (server *Server) serveSnapcast(w http.ResponseWriter, r *http.Request) {
	if server.SnapcastStream == "" {
		http.Error(w, "snapcast sessions are off (-snapcastStream)", http.StatusNotFound)
		return
	}
	path := r.URL.Query().Get("path")
	filePath := server.filePath(path)
	if ignored, err := server.IgnorePath(filePath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else if ignored {
		http.Error(w, "no such object", http.StatusNotFound)
		return
	}
	uri := strings.ReplaceAll(server.SnapcastStream, "{host}", snapcastHost(r))
	slog.Info("snapcast session requested", "path", path, "uri", uri, "remote", r.RemoteAddr)
	mime := server.SnapcastFormat
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = mime[:i]
	}
	w.Header().Set("Content-Type", mime)
	w.Write([]byte(uri + "\n"))
}

// snapcastHost is the address of this machine for the snapcast:// URI: the local
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
