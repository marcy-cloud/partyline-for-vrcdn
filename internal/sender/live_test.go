package sender

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/bluenviron/gortmplib"
	"github.com/bluenviron/gortmplib/pkg/codecs"

	"partyline/internal/libmpv"
)

// rtmpServer is an RTMP ingest on this computer that counts what arrives.
type rtmpServer struct {
	ln net.Listener

	mu        sync.Mutex
	key       string // the last part of the address published to
	rate      int
	channels  int
	frames    int
	lastFrame time.Duration
}

func startRTMPServer(t *testing.T) *rtmpServer {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &rtmpServer{ln: ln}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return s
}

func (s *rtmpServer) url() string { return fmt.Sprintf("rtmp://%s/live", s.ln.Addr()) }

func (s *rtmpServer) serve(conn net.Conn) {
	defer conn.Close()
	sc := &gortmplib.ServerConn{RW: conn}
	if sc.Initialize() != nil || sc.Accept() != nil || !sc.Publish {
		return
	}
	s.mu.Lock()
	s.key = sc.URL.Path
	s.mu.Unlock()

	r := &gortmplib.Reader{Conn: sc}
	if r.Initialize() != nil {
		return
	}
	for _, track := range r.Tracks() {
		aac, ok := track.Codec.(*codecs.MPEG4Audio)
		if !ok {
			continue
		}
		s.mu.Lock()
		s.rate, s.channels = aac.Config.SampleRate, aac.Config.ChannelCount
		s.mu.Unlock()
		r.OnDataMPEG4Audio(track, func(pts time.Duration, _ []byte) {
			s.mu.Lock()
			s.frames++
			s.lastFrame = pts
			s.mu.Unlock()
		})
	}
	for r.Read() == nil {
	}
}

// Each encoder this computer has must put the test tone on air: AAC, 48 kHz
// stereo, in real time, at an RTMP server that is told the key. This runs
// the real libmpv (and Apple's encoder where there is one) against a server
// in this process, and is skipped where libmpv isn't available.
//
//	go test -tags bundled_libmpv ./internal/sender/
func TestGoesOnAirWithEachEncoder(t *testing.T) {
	if err := libmpv.Load(); err != nil {
		t.Skipf("libmpv is not available: %v", err)
	}
	for _, enc := range Encoders() {
		t.Run(enc.ID, func(t *testing.T) {
			srv := startRTMPServer(t)
			live := make(chan struct{}, 1)
			failed := make(chan string, 1)
			s := New(DefaultTuning(), Hooks{State: func(st State) {
				switch st.Status {
				case Live:
					select {
					case live <- struct{}{}:
					default:
					}
				case Failed:
					select {
					case failed <- st.Message:
					default:
					}
				}
			}})
			err := s.Start(Config{Ingest: srv.url(), Key: "a_test_key", Device: TestTone.ID, BitrateKbps: Bitrates[0], Encoder: enc.ID})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Stop()

			select {
			case <-live:
			case msg := <-failed:
				t.Fatalf("failed: %s", msg)
			case <-time.After(20 * time.Second):
				t.Fatalf("not live within 20 s: %+v", s.State())
			}

			// The server needs a moment to recognise the audio before it
			// counts any; then three seconds of it should be three seconds.
			count := func() (int, time.Duration) {
				srv.mu.Lock()
				defer srv.mu.Unlock()
				return srv.frames, srv.lastFrame
			}
			deadline := time.Now().Add(10 * time.Second)
			for n, _ := count(); n == 0; n, _ = count() {
				if time.Now().After(deadline) {
					t.Fatal("the server got no audio within 10 s of going live")
				}
				time.Sleep(50 * time.Millisecond)
			}
			n0, pts0 := count()
			time.Sleep(3 * time.Second)
			n1, pts1 := count()

			srv.mu.Lock()
			key, rate, channels := srv.key, srv.rate, srv.channels
			srv.mu.Unlock()
			if key != "/live/a_test_key" {
				t.Errorf("published to %q", key)
			}
			if rate != 48000 || channels != 2 {
				t.Errorf("audio is %d Hz, %d channels; want 48000 Hz stereo", rate, channels)
			}
			// 1024 samples a frame: 46.9 frames a second.
			if got := n1 - n0; got < 120 || got > 165 {
				t.Errorf("%d frames in 3 s, want about 141", got)
			}
			if span := pts1 - pts0; span < 2500*time.Millisecond || span > 3500*time.Millisecond {
				t.Errorf("3 s of sending carried %v of audio", span)
			}
			t.Logf("%s: %d frames in 3 s carrying %v of audio", enc.Name, n1-n0, pts1-pts0)
		})
	}
}
