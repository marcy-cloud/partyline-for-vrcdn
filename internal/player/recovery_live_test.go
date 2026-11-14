package player

import (
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"partyline/internal/libmpv"
)

// flakyLink is a live stream whose delivery can be held up, as a listener's
// or a performer's connection dropping out does over TCP: the audio keeps
// being made, queues up while nothing gets through, and arrives in a rush
// afterwards.
type flakyLink struct {
	heldUntil atomic.Int64 // Unix ns; nothing is sent before then
	// rush is how many times faster than real time the queue drains.
	rush atomic.Int64
}

func (f *flakyLink) hold(d time.Duration) { f.heldUntil.Store(time.Now().Add(d).UnixNano()) }

func (f *flakyLink) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "audio/wav")
	flusher := w.(http.Flusher)
	header := make([]byte, 44)
	copy(header[0:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:], 0xFFFFFFFF)
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], liveChannels)
	binary.LittleEndian.PutUint32(header[24:], liveRate)
	binary.LittleEndian.PutUint32(header[28:], liveRate*liveChannels*2)
	binary.LittleEndian.PutUint16(header[32:], liveChannels*2)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], 0xFFFFFFFF)
	if _, err := w.Write(header); err != nil {
		return
	}

	const chunk = 20 * time.Millisecond
	frames := liveRate * int(chunk/time.Millisecond) / 1000
	buf := make([]byte, frames*liveChannels*2)
	began := time.Now()
	for n, sent := 0, 0; ; {
		if r.Context().Err() != nil {
			return
		}
		made := int(time.Since(began) / chunk) // chunks that exist by now
		if time.Now().UnixNano() < f.heldUntil.Load() || sent >= made {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		for i := 0; i < frames; i, n = i+1, n+1 {
			v := uint16(int16(8000 * math.Sin(2*math.Pi*440*float64(n)/liveRate)))
			for c := 0; c < liveChannels; c++ {
				binary.LittleEndian.PutUint16(buf[(i*liveChannels+c)*2:], v)
			}
		}
		if _, err := w.Write(buf); err != nil {
			return
		}
		flusher.Flush()
		sent++
		if made-sent > 1 { // behind: drain the queue at the rush rate
			time.Sleep(chunk / time.Duration(max(f.rush.Load(), 1)))
		}
	}
}

// spy notes what the controller asks of the engine.
type spy struct {
	*MPVEngine
	mu    sync.Mutex
	calls []string
}

func (s *spy) note(what string) {
	s.mu.Lock()
	s.calls = append(s.calls, what)
	s.mu.Unlock()
}
func (s *spy) take() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := strings.Join(s.calls, " ")
	s.calls = nil
	return out
}
func (s *spy) DropBuffers() error { s.note("DROP"); return s.MPVEngine.DropBuffers() }
func (s *spy) Stop() error        { s.note("STOP"); return s.MPVEngine.Stop() }
func (s *spy) SetSpeed(v float64) error {
	s.note(fmt.Sprintf("SPEED=%.2f", v))
	return s.MPVEngine.SetSpeed(v)
}
func (s *spy) Load(url, rtsp string) (int64, error) {
	s.note("LOAD")
	return s.MPVEngine.Load(url, rtsp)
}

// A listener must be back at live, at normal speed, soon after a dropout
// ends: not playing fast for a quarter of a minute to work through what
// queued up (which is what a 2 s dropout used to cost). Run with -v to see
// each case quarter-second by quarter-second.
func TestRecoveryAfterNetworkPause(t *testing.T) {
	if testing.Short() {
		t.Skip("takes half a minute")
	}
	if err := libmpv.Load(); err != nil {
		t.Skipf("libmpv is not available: %v", err)
	}
	for _, tc := range []struct {
		name string
		hold time.Duration
		rush int64
		// within is how soon after delivery resumes playback must be
		// steady. A queue that trickles in can't be skipped before it has
		// arrived, so that case gets as long as the trickle lasts.
		within time.Duration
	}{
		{"2 s gap, queue arrives at once", 2 * time.Second, 200, time.Second},
		{"1 s gap, queue arrives at once", time.Second, 200, 2 * time.Second},
		{"3 s gap, queue arrives at twice real time", 3 * time.Second, 2, 4500 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			link := &flakyLink{}
			link.rush.Store(tc.rush)
			srv := httptest.NewServer(link)
			defer srv.Close()
			engine, err := newMPVEngine("player test", [2]string{"ao", "null"})
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			e := &spy{MPVEngine: engine}
			ctl := NewController(e, Options{AutoReconnect: true, AutoCatchUp: true, CatchUpAboveMs: 250}, DefaultTuning(), Hooks{})
			defer ctl.Close()

			ctl.Play(Target{StreamID: "a", Label: "tone", URL: srv.URL + "/live.wav"})
			deadline := time.Now().Add(20 * time.Second)
			for {
				s := ctl.State()
				if s.Status == Playing && s.BufferMs >= 0 && s.BufferMs <= 250 && !s.CatchingUp {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("never reached the live edge: %+v", s)
				}
				time.Sleep(100 * time.Millisecond)
			}
			time.Sleep(2 * time.Second)
			e.take()

			link.hold(tc.hold)
			began := time.Now()
			var lastDisturbed, fast time.Duration
			for time.Since(began) < tc.hold+tc.within+4*time.Second {
				time.Sleep(250 * time.Millisecond)
				s := ctl.State()
				at := time.Since(began)
				calls := e.take()
				steady := s.Status == Playing && !s.CatchingUp && s.BufferMs <= 250 && calls == ""
				if !steady && at > tc.hold {
					lastDisturbed = at
				}
				if s.CatchingUp {
					fast += 250 * time.Millisecond
				}
				if calls != "" || !steady {
					t.Logf("%5.1fs %-12s buffer %5d ms catching-up=%-5v %s", at.Seconds(), s.Status, s.BufferMs, s.CatchingUp, calls)
				}
			}
			took := lastDisturbed - tc.hold
			t.Logf("steady at live %.1f s after delivery resumed; played fast for %.1f s", took.Seconds(), fast.Seconds())
			if took > tc.within {
				t.Errorf("took %.1f s to get back to steady live playback, want within %.1f s", took.Seconds(), tc.within.Seconds())
			}
			if fast > 2*time.Second {
				t.Errorf("played fast for %.1f s", fast.Seconds())
			}
		})
	}
}
