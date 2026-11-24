package dms

import (
	"net"
	"strings"

	"github.com/anacrolix/dms/upnpav"
)

// snapcastResource is the PoC Snapcast resource of an audio item (see
// Server.SnapcastStream): protocol "snapcast", the stream's content format, and no
// DLNA parameters.
func snapcastResource(stream, format, host, duration string) upnpav.Resource {
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}
	if strings.Contains(hostname, ":") {
		hostname = "[" + hostname + "]" // IPv6 literal
	}
	return upnpav.Resource{
		URL:          strings.ReplaceAll(stream, "{host}", hostname),
		ProtocolInfo: "snapcast:*:" + format + ":*",
		Duration:     duration,
	}
}
