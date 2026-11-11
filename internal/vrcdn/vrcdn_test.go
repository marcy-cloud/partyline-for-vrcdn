package vrcdn

import "testing"

func TestParseVRCDNInputs(t *testing.T) {
	// Every endpoint the VRCDN dashboard shows should reduce to the name.
	inputs := []string{
		"somedj",
		"  somedj  ",
		"rtmp://stream.vrcdn.live/live/somedj",
		"rtsp://stream.vrcdn.live/live/somedj",
		"rtspt://stream.vrcdn.live/live/somedj",
		"https://stream.vrcdn.live/live/somedj.live.ts",
		"https://stream.vrcdn.live/live/somedj.live.mp4",
		"https://stream.vrcdn.live/live/somedj.live.flv",
		"wss://stream.vrcdn.live/live/somedj.live.flv",
		"https://STREAM.vrcdn.live/live/somedj.LIVE.FLV",
		"stream.vrcdn.live/live/somedj",
	}
	for _, in := range inputs {
		got, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", in, err)
			continue
		}
		if got.Name != "somedj" || got.Custom != "" {
			t.Errorf("Parse(%q) = %+v, want Name=somedj", in, got)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		"has space",
		"semi;colon",
		"https://stream.vrcdn.live/live/",
		"https://stream.vrcdn.live/",
		"file:///etc/passwd",
		"av://lavfi:sine",
		"://nope",
	} {
		if got, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %+v, want error", in, got)
		}
	}
}

func TestParseCustom(t *testing.T) {
	got, err := Parse("rtspt://example.com:8554/live/x")
	if err != nil {
		t.Fatal(err)
	}
	if got.Custom != "rtsp://example.com:8554/live/x" || got.Name != "" {
		t.Errorf("got %+v", got)
	}
	if u := got.URL(FLV); u != got.Custom {
		t.Errorf("custom URL should ignore transport, got %q", u)
	}
}

func TestURL(t *testing.T) {
	s := Source{Name: "somedj"}
	cases := map[Transport]string{
		RTSPTCP: "rtsp://stream.vrcdn.live/live/somedj",
		RTSPUDP: "rtsp://stream.vrcdn.live/live/somedj",
		FLV:     "https://stream.vrcdn.live/live/somedj.live.flv",
		MPEGTS:  "https://stream.vrcdn.live/live/somedj.live.ts",
	}
	for tr, want := range cases {
		if got := s.URL(tr); got != want {
			t.Errorf("URL(%s) = %q, want %q", tr, got, want)
		}
	}
}

func TestTransport(t *testing.T) {
	if !RTSPTCP.Valid() || Transport("hls").Valid() {
		t.Error("Valid() wrong")
	}
	if RTSPTCP.RTSPTransport() != "tcp" || RTSPUDP.RTSPTransport() != "udp" || FLV.RTSPTransport() != "" {
		t.Error("RTSPTransport() wrong")
	}
}
