// Package vrcdn turns whatever a VJ pastes (a bare stream name, or any of the
// URLs from the VRCDN dashboard) into a playable URL for a chosen transport.
package vrcdn

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Host is the VRCDN ingest/egress host all stream URLs are served from.
const Host = "stream.vrcdn.live"

// Transport selects which VRCDN endpoint a stream is pulled from.
type Transport string

const (
	// RTSP is RTSP that tries RTP over UDP first and falls back to TCP when
	// no audio arrives. The default.
	RTSP Transport = "rtsp"
	// RTSPUDP is RTSP with RTP over UDP only. Lowest latency, but often
	// blocked by NAT/firewalls.
	RTSPUDP Transport = "rtsp-udp"
	// RTSPTCP is RTSP interleaved over TCP only. Works almost everywhere.
	RTSPTCP Transport = "rtsp-tcp"
	// FLV is HTTP-FLV over HTTPS.
	FLV Transport = "flv"
	// MPEGTS is MPEG-TS over HTTPS.
	MPEGTS Transport = "mpegts"
)

// Transports lists every supported transport, default first.
var Transports = []Transport{RTSP, RTSPUDP, RTSPTCP, FLV, MPEGTS}

// Valid reports whether t is a known transport.
func (t Transport) Valid() bool {
	for _, k := range Transports {
		if k == t {
			return true
		}
	}
	return false
}

// Source is a parsed stream input.
type Source struct {
	// Name is the VRCDN stream name. Empty when Custom is set.
	Name string
	// Custom is a non-VRCDN URL to be played verbatim.
	Custom string
}

var (
	// ErrEmpty is returned for blank input.
	ErrEmpty = errors.New("enter a stream name or URL")

	nameRe = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,64}$`)

	// Suffixes VRCDN appends to the stream name on its HTTP endpoints.
	liveSuffixes = []string{".live.flv", ".live.ts", ".live.mp4", ".live.m3u8"}

	// Schemes accepted for custom (non-VRCDN) URLs. Deliberately excludes
	// file:// and friends so a saved entry can only ever open a network stream.
	customSchemes = map[string]bool{
		"rtsp": true, "rtsps": true, "rtmp": true, "rtmps": true,
		"http": true, "https": true, "srt": true, "udp": true, "rtp": true,
	}
)

// Parse accepts a bare stream name or a URL. VRCDN URLs of any flavour
// (rtmp, rtsp, rtspt, https .live.*, wss) are reduced to the stream name so
// the transport can be chosen independently. Other network URLs are kept as
// custom sources.
func Parse(input string) (Source, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return Source{}, ErrEmpty
	}

	if !strings.Contains(s, "://") {
		// "stream.vrcdn.live/live/name" pasted without a scheme.
		if strings.HasPrefix(strings.ToLower(s), Host+"/") {
			return Parse("https://" + s)
		}
		if !nameRe.MatchString(s) {
			return Source{}, fmt.Errorf("%q is not a valid stream name", s)
		}
		return Source{Name: s}, nil
	}

	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return Source{}, fmt.Errorf("%q is not a valid URL", s)
	}
	scheme := strings.ToLower(u.Scheme)

	if strings.EqualFold(u.Hostname(), Host) {
		name := nameFromPath(u.Path)
		if name == "" {
			return Source{}, fmt.Errorf("could not find a stream name in %q", s)
		}
		return Source{Name: name}, nil
	}

	// VRChat writes RTSP-over-TCP as rtspt://; libav only knows rtsp://.
	if scheme == "rtspt" {
		scheme = "rtsp"
		u.Scheme = scheme
	}
	if !customSchemes[scheme] {
		return Source{}, fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	return Source{Custom: u.String()}, nil
}

// nameFromPath extracts the stream name from a VRCDN URL path such as
// /live/name or /live/name.live.flv.
func nameFromPath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	last := parts[len(parts)-1]
	lower := strings.ToLower(last)
	for _, suf := range liveSuffixes {
		if strings.HasSuffix(lower, suf) {
			last = last[:len(last)-len(suf)]
			break
		}
	}
	if !nameRe.MatchString(last) || strings.EqualFold(last, "live") {
		return ""
	}
	return last
}

// URL returns the URL to hand to the player for the given transport. Custom
// sources are returned unchanged.
func (s Source) URL(t Transport) string {
	if s.Custom != "" {
		return s.Custom
	}
	switch t {
	case FLV:
		return "https://" + Host + "/live/" + s.Name + ".live.flv"
	case MPEGTS:
		return "https://" + Host + "/live/" + s.Name + ".live.ts"
	default:
		return "rtsp://" + Host + "/live/" + s.Name
	}
}

// Display is a short human-readable form of the source for list rows.
func (s Source) Display() string {
	if s.Custom != "" {
		return s.Custom
	}
	return s.Name
}

// FallbackURL returns the URL to play when RTSP gets no response at all: the
// same stream as MPEG-TS over HTTPS, which passes networks that filter RTSP.
// Only the default RTSP setting falls back; a stream pinned to a transport
// stays on it. It is "" when there is no fallback.
func (s Source) FallbackURL(t Transport) string {
	if s.Custom != "" || t != RTSP {
		return ""
	}
	return s.URL(MPEGTS)
}

// LiveCheckURL returns an HTTP URL that answers 404 at once when the stream
// is offline, which RTSP does not do. "" for custom sources.
func (s Source) LiveCheckURL() string {
	if s.Custom != "" {
		return ""
	}
	return s.URL(MPEGTS)
}

// Canonical is the form stored in the config: the bare name for VRCDN
// streams, the full URL for custom ones.
func (s Source) Canonical() string { return s.Display() }

// RTSPTransports returns the libav rtsp_transport values to try for t, in
// order, or nil when the transport is not RTSP.
func (t Transport) RTSPTransports() []string {
	switch t {
	case RTSP:
		return []string{"udp", "tcp"}
	case RTSPUDP:
		return []string{"udp"}
	case RTSPTCP:
		return []string{"tcp"}
	}
	return nil
}

// RTSPTransports returns the rtsp_transport values to try for this source
// with the stream's transport setting. A custom rtsp:// URL has no setting
// of its own and gets the default order; rtsps:// is TCP only.
func (s Source) RTSPTransports(t Transport) []string {
	if s.Custom == "" {
		return t.RTSPTransports()
	}
	switch low := strings.ToLower(s.Custom); {
	case strings.HasPrefix(low, "rtsp://"):
		return RTSP.RTSPTransports()
	case strings.HasPrefix(low, "rtsps://"):
		return RTSPTCP.RTSPTransports()
	}
	return nil
}
