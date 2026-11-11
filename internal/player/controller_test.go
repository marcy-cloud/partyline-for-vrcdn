package player

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeEngine records what the controller asks for and lets tests inject
// engine events.
type fakeEngine struct {
	mu      sync.Mutex
	events  chan Event
	next    int64
	loads   []string
	via     []string // the RTSP transport of each load
	stops   int
	drops   int
	speeds  []float64
	loadErr error
}

func newFakeEngine() *fakeEngine { return &fakeEngine{events: make(chan Event, 64)} }

func (f *fakeEngine) Load(url, rtspTransport string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loadErr != nil {
		return 0, f.loadErr
	}
	f.next++
	f.loads = append(f.loads, url)
	f.via = append(f.via, rtspTransport)
	return f.next, nil
}
func (f *fakeEngine) Stop() error        { f.mu.Lock(); f.stops++; f.mu.Unlock(); return nil }
func (f *fakeEngine) DropBuffers() error { f.mu.Lock(); f.drops++; f.mu.Unlock(); return nil }
func (f *fakeEngine) SetSpeed(s float64) error {
	f.mu.Lock()
	f.speeds = append(f.speeds, s)
	f.mu.Unlock()
	return nil
}
func (f *fakeEngine) SetVolume(float64) error              { return nil }
func (f *fakeEngine) SetAudioDevice(string) error          { return nil }
func (f *fakeEngine) AudioDevices() ([]AudioDevice, error) { return nil, nil }
func (f *fakeEngine) Events() <-chan Event                 { return f.events }
func (f *fakeEngine) Describe() string                     { return "fake" }
func (f *fakeEngine) Close()                               {}

func (f *fakeEngine) loadCount() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.loads) }
func (f *fakeEngine) lastVia() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.via) == 0 {
		return ""
	}
	return f.via[len(f.via)-1]
}
func (f *fakeEngine) stopCount() int { f.mu.Lock(); defer f.mu.Unlock(); return f.stops }
func (f *fakeEngine) dropCount() int { f.mu.Lock(); defer f.mu.Unlock(); return f.drops }
func (f *fakeEngine) lastSpeed() float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.speeds) == 0 {
		return 1
	}
	return f.speeds[len(f.speeds)-1]
}
func (f *fakeEngine) entry() int64 { f.mu.Lock(); defer f.mu.Unlock(); return f.next }

func (f *fakeEngine) started() { f.events <- Event{Kind: EvStarted, Entry: f.entry()} }
func (f *fakeEngine) failed(detail string) {
	f.events <- Event{Kind: EvEnded, Entry: f.entry(), Reason: EndError, Detail: detail}
}
func (f *fakeEngine) tick(bufferSec, pos float64) {
	f.events <- Event{Kind: EvTick, Entry: f.entry(), BufferSec: bufferSec, PosSec: pos, HavePos: true}
}

func testTuning() Tuning {
	return Tuning{
		Backoff:        func(int) time.Duration { return 20 * time.Millisecond },
		ConnectTimeout: 5 * time.Second,
		FallbackAfter:  5 * time.Second,
		StallTimeout:   5 * time.Second,
		CatchUpSustain: 0,
		CatchUpSpeed:   1.1,
	}
}

var defaultOpts = Options{AutoReconnect: true, AutoCatchUp: true, CatchUpAboveMs: 250}

func start(t *testing.T, opts Options, tuning Tuning) (*Controller, *fakeEngine) {
	t.Helper()
	f := newFakeEngine()
	c := NewController(f, opts, tuning, Hooks{})
	t.Cleanup(c.Close)
	return c, f
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func waitStatus(t *testing.T, c *Controller, want Status) State {
	t.Helper()
	waitFor(t, "status "+string(want), func() bool { return c.State().Status == want })
	return c.State()
}

var djA = Target{StreamID: "a", Label: "DJ A", URL: "rtsp://x/a", RTSPTransports: []string{"tcp"}}
var djB = Target{StreamID: "b", Label: "DJ B", URL: "rtsp://x/b", RTSPTransports: []string{"tcp"}}

// The default RTSP setting: UDP first, then TCP.
var udpFirstA = Target{StreamID: "a", Label: "DJ A", URL: "rtsp://x/a", RTSPTransports: []string{"udp", "tcp"}}
var udpFirstB = Target{StreamID: "b", Label: "DJ B", URL: "rtsp://x/b", RTSPTransports: []string{"udp", "tcp"}}

func TestPlayThenStop(t *testing.T) {
	c, f := start(t, defaultOpts, testTuning())

	if s := c.State(); s.Status != Idle || s.BufferMs != -1 {
		t.Fatalf("initial state = %+v", s)
	}

	c.Play(djA)
	s := waitStatus(t, c, Connecting)
	if s.StreamID != "a" || s.Label != "DJ A" || s.URL != djA.URL {
		t.Errorf("state = %+v", s)
	}

	f.started()
	s = waitStatus(t, c, Playing)
	if s.SinceMs == 0 {
		t.Error("SinceMs not set when playing")
	}

	f.tick(0.04, 1)
	waitFor(t, "buffer reading", func() bool { return c.State().BufferMs == 40 })

	c.Stop()
	s = waitStatus(t, c, Idle)
	if s.StreamID != "" || f.stopCount() != 1 {
		t.Errorf("after stop: state=%+v stops=%d", s, f.stopCount())
	}
}

func TestReconnectsUntilStreamComesUp(t *testing.T) {
	c, f := start(t, defaultOpts, testTuning())
	c.Play(djA)
	waitStatus(t, c, Connecting)

	f.failed("rtsp: method DESCRIBE failed: 404 Stream Not Found")
	s := waitStatus(t, c, Reconnecting)
	if s.Message != "Stream is offline or doesn't exist" || s.Attempt != 1 || s.Label != "DJ A" {
		t.Errorf("state = %+v", s)
	}

	waitFor(t, "second load", func() bool { return f.loadCount() == 2 })
	f.failed("404")
	waitFor(t, "attempt 2", func() bool { return c.State().Attempt == 2 })

	waitFor(t, "third load", func() bool { return f.loadCount() == 3 })
	f.started()
	s = waitStatus(t, c, Playing)
	if s.Attempt != 0 || s.Message != "" {
		t.Errorf("state after recovery = %+v", s)
	}
}

func TestDropWhilePlayingReconnects(t *testing.T) {
	c, f := start(t, defaultOpts, testTuning())
	c.Play(djA)
	f.started()
	waitStatus(t, c, Playing)

	f.events <- Event{Kind: EvEnded, Entry: f.entry(), Reason: EndEOF}
	s := waitStatus(t, c, Reconnecting)
	if s.Message != "Stream ended" || s.BufferMs != -1 || s.SinceMs != 0 {
		t.Errorf("state = %+v", s)
	}
	waitFor(t, "reload", func() bool { return f.loadCount() == 2 })
}

func TestFailsWithoutAutoReconnect(t *testing.T) {
	opts := defaultOpts
	opts.AutoReconnect = false
	c, f := start(t, opts, testTuning())

	c.Play(djA)
	waitStatus(t, c, Connecting)
	f.failed("401 Unauthorized")
	s := waitStatus(t, c, Failed)
	if s.Message != "Stream is offline or doesn't exist" || s.Label != "DJ A" {
		t.Errorf("state = %+v", s)
	}
	time.Sleep(60 * time.Millisecond)
	if f.loadCount() != 1 {
		t.Errorf("loads = %d, want no retry", f.loadCount())
	}
}

func TestLoadErrorIsReported(t *testing.T) {
	opts := defaultOpts
	opts.AutoReconnect = false
	c, f := start(t, opts, testTuning())
	f.loadErr = errors.New("mpv: loadfile: invalid parameter")

	c.Play(djA)
	s := waitStatus(t, c, Failed)
	if s.Message != "mpv: loadfile: invalid parameter" {
		t.Errorf("message = %q", s.Message)
	}
}

func TestEventsFromReplacedStreamAreIgnored(t *testing.T) {
	c, f := start(t, defaultOpts, testTuning())
	c.Play(djA)
	waitStatus(t, c, Connecting)
	oldEntry := f.entry()

	c.Play(djB)
	waitFor(t, "switch to B", func() bool { return c.State().StreamID == "b" })

	// The old stream's death arriving late must not disturb the new one.
	f.events <- Event{Kind: EvEnded, Entry: oldEntry, Reason: EndError, Detail: "404"}
	f.events <- Event{Kind: EvEnded, Entry: oldEntry, Reason: EndStopped}
	f.started()
	s := waitStatus(t, c, Playing)
	if s.StreamID != "b" || s.Attempt != 0 {
		t.Errorf("state = %+v", s)
	}
	time.Sleep(60 * time.Millisecond)
	if f.loadCount() != 2 {
		t.Errorf("loads = %d, want 2 (no spurious retry)", f.loadCount())
	}
}

func TestStopCancelsPendingReconnect(t *testing.T) {
	c, f := start(t, defaultOpts, testTuning())
	c.Play(djA)
	waitStatus(t, c, Connecting)
	f.failed("404")
	waitStatus(t, c, Reconnecting)

	c.Stop()
	waitStatus(t, c, Idle)
	time.Sleep(60 * time.Millisecond)
	if f.loadCount() != 1 {
		t.Errorf("loads = %d, want 1 (retry cancelled)", f.loadCount())
	}
}

func TestConnectTimeout(t *testing.T) {
	tuning := testTuning()
	tuning.ConnectTimeout = 40 * time.Millisecond
	c, f := start(t, defaultOpts, tuning)

	c.Play(djA)
	s := waitStatus(t, c, Reconnecting)
	if s.Message != "Timed out connecting" {
		t.Errorf("message = %q", s.Message)
	}
	if f.stopCount() != 1 {
		t.Errorf("stops = %d, want the hung load to be stopped", f.stopCount())
	}
	waitFor(t, "retry", func() bool { return f.loadCount() >= 2 })
}

func TestStallIsDetected(t *testing.T) {
	tuning := testTuning()
	tuning.StallTimeout = 40 * time.Millisecond
	c, f := start(t, defaultOpts, tuning)

	c.Play(djA)
	f.started()
	waitStatus(t, c, Playing)

	// Position advancing: healthy.
	for i := 0; i < 4; i++ {
		f.tick(0.02, float64(i))
		time.Sleep(15 * time.Millisecond)
	}
	if s := c.State(); s.Status != Playing {
		t.Fatalf("state = %+v, want still playing", s)
	}

	// Position frozen.
	waitFor(t, "stall detection", func() bool {
		f.tick(0.02, 3)
		return c.State().Status == Reconnecting
	})
	if s := c.State(); s.Message != "Stream stalled" {
		t.Errorf("message = %q", s.Message)
	}
}

func TestCatchUp(t *testing.T) {
	c, f := start(t, defaultOpts, testTuning())
	c.Play(djA)
	f.started()
	waitStatus(t, c, Playing)

	f.tick(0.10, 1) // under the 250 ms threshold
	waitFor(t, "reading", func() bool { return c.State().BufferMs == 100 })
	if c.State().CatchingUp {
		t.Fatal("catching up below threshold")
	}

	f.tick(0.60, 2)
	waitFor(t, "catch-up to start", func() bool { return c.State().CatchingUp })
	if f.lastSpeed() != 1.1 {
		t.Errorf("speed = %v, want 1.1", f.lastSpeed())
	}

	f.tick(0.20, 3) // draining but not there yet
	waitFor(t, "reading", func() bool { return c.State().BufferMs == 200 })
	if !c.State().CatchingUp {
		t.Error("catch-up ended too early")
	}

	f.tick(0.05, 4)
	waitFor(t, "catch-up to end", func() bool { return !c.State().CatchingUp })
	if f.lastSpeed() != 1 {
		t.Errorf("speed = %v, want 1", f.lastSpeed())
	}

	// Far too much queued: drop it rather than play it off.
	f.tick(3.5, 5)
	waitFor(t, "hard drop", func() bool { return f.dropCount() == 1 })
}

func TestCatchUpNeedsSustainedExcess(t *testing.T) {
	tuning := testTuning()
	tuning.CatchUpSustain = time.Hour
	c, f := start(t, defaultOpts, tuning)
	c.Play(djA)
	f.started()
	waitStatus(t, c, Playing)
	f.tick(0.01, 1)
	waitFor(t, "reading", func() bool { return c.State().BufferMs == 10 })

	f.tick(0.6, 2)
	waitFor(t, "reading", func() bool { return c.State().BufferMs == 600 })
	if c.State().CatchingUp || f.dropCount() != 0 {
		t.Error("a single over-threshold reading should not start catch-up")
	}
}

// Audio queued while connecting is skipped, so playback begins at the live
// edge instead of spending seconds catching up to it.
func TestStartsAtTheLiveEdge(t *testing.T) {
	c, f := start(t, defaultOpts, testTuning())
	c.Play(djA)
	f.started()
	waitStatus(t, c, Playing)

	f.tick(0.8, 1)
	waitFor(t, "drop at start", func() bool { return f.dropCount() == 1 })
	if c.State().CatchingUp {
		t.Error("the start-up backlog should be dropped, not played off fast")
	}

	// Only the first reading is treated that way.
	f.tick(0.8, 2)
	waitFor(t, "catch-up", func() bool { return c.State().CatchingUp })
	if f.dropCount() != 1 {
		t.Errorf("drops = %d, want 1", f.dropCount())
	}

	// A reconnect is a new start.
	f.failed("connection reset")
	waitStatus(t, c, Reconnecting)
	waitFor(t, "reload", func() bool { return f.loadCount() == 2 })
	f.started()
	waitStatus(t, c, Playing)
	f.tick(0.8, 1)
	waitFor(t, "drop after reconnect", func() bool { return f.dropCount() == 2 })
}

func TestCatchUpDisabled(t *testing.T) {
	opts := defaultOpts
	opts.AutoCatchUp = false
	c, f := start(t, opts, testTuning())
	c.Play(djA)
	f.started()
	waitStatus(t, c, Playing)

	f.tick(5, 1)
	waitFor(t, "reading", func() bool { return c.State().BufferMs == 5000 })
	if c.State().CatchingUp || f.dropCount() != 0 || len(f.speeds) != 0 {
		t.Error("catch-up acted while disabled")
	}
}

func TestSwitchingStreamsResetsSpeed(t *testing.T) {
	c, f := start(t, defaultOpts, testTuning())
	c.Play(djA)
	f.started()
	waitStatus(t, c, Playing)
	f.tick(0.01, 1)
	f.tick(0.6, 2)
	waitFor(t, "catch-up", func() bool { return c.State().CatchingUp })

	c.Play(djB)
	waitFor(t, "switch", func() bool { return c.State().StreamID == "b" })
	if f.lastSpeed() != 1 || c.State().CatchingUp {
		t.Errorf("speed = %v catchingUp = %v after switching", f.lastSpeed(), c.State().CatchingUp)
	}
}

func TestResync(t *testing.T) {
	tuning := testTuning()
	tuning.Backoff = func(int) time.Duration { return time.Hour }
	c, f := start(t, defaultOpts, tuning)

	c.Resync() // idle: nothing to do
	c.Play(djA)
	f.started()
	waitStatus(t, c, Playing)

	c.Resync()
	waitFor(t, "drop", func() bool { return f.dropCount() == 1 })

	// While waiting out a (long) backoff, Resync retries immediately.
	f.failed("404")
	waitStatus(t, c, Reconnecting)
	if f.loadCount() != 1 {
		t.Fatalf("loads = %d", f.loadCount())
	}
	c.Resync()
	waitFor(t, "immediate retry", func() bool { return f.loadCount() == 2 })
}

func TestTurningOffAutoReconnectWhileWaiting(t *testing.T) {
	tuning := testTuning()
	tuning.Backoff = func(int) time.Duration { return time.Hour }
	c, f := start(t, defaultOpts, tuning)
	c.Play(djA)
	waitStatus(t, c, Connecting)
	f.failed("404")
	waitStatus(t, c, Reconnecting)

	opts := defaultOpts
	opts.AutoReconnect = false
	c.SetOptions(opts)
	waitStatus(t, c, Failed)
}

func TestExplain(t *testing.T) {
	cases := map[string]string{
		"": "Connection lost",
		"method DESCRIBE failed: 404 Stream Not Found": "Stream is offline or doesn't exist",
		"Server returned 401 Unauthorized":             "Stream is offline or doesn't exist",
		"loading failed":                               "Stream is offline or unreachable",
		"Connection refused":                           "Can't reach the server",
		"something else":                               "something else",
	}
	for in, want := range cases {
		if got := explain(in); got != want {
			t.Errorf("explain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHooks(t *testing.T) {
	f := newFakeEngine()
	var mu sync.Mutex
	var states []Status
	devices := 0
	c := NewController(f, defaultOpts, testTuning(), Hooks{
		State:   func(s State) { mu.Lock(); states = append(states, s.Status); mu.Unlock() },
		Devices: func() { mu.Lock(); devices++; mu.Unlock() },
	})
	defer c.Close()

	f.events <- Event{Kind: EvDevices}
	c.Play(djA)
	f.started()
	waitStatus(t, c, Playing)
	waitFor(t, "hooks", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return devices == 1 && len(states) == 2
	})
	mu.Lock()
	defer mu.Unlock()
	if states[0] != Connecting || states[1] != Playing {
		t.Errorf("state hook saw %v", states)
	}
}

func TestUDPIsTriedFirst(t *testing.T) {
	tuning := testTuning()
	tuning.FallbackAfter = 40 * time.Millisecond
	c, f := start(t, defaultOpts, tuning)
	c.Play(udpFirstA)
	if f.lastVia() != "udp" {
		t.Fatalf("first attempt over %q, want udp", f.lastVia())
	}
	f.started()
	st := waitStatus(t, c, Playing)
	if st.Via != "udp" {
		t.Errorf("Via = %q, want udp", st.Via)
	}

	// Audio arrived in time, so nothing falls back.
	time.Sleep(3 * tuning.FallbackAfter)
	if f.loadCount() != 1 || c.State().Status != Playing {
		t.Errorf("loads = %d, status = %s after UDP worked", f.loadCount(), c.State().Status)
	}
}

func TestFallsBackToTCPWhenUDPStaysSilent(t *testing.T) {
	tuning := testTuning()
	tuning.FallbackAfter = 40 * time.Millisecond
	c, f := start(t, defaultOpts, tuning)
	c.Play(udpFirstA)

	waitFor(t, "TCP attempt", func() bool { return c.State().Via == "tcp" })
	if f.lastVia() != "tcp" || f.loadCount() != 2 {
		t.Fatalf("after %d loads the engine is on %q, want tcp on the second", f.loadCount(), f.lastVia())
	}
	// Still the same connection attempt as far as the user is concerned.
	if st := c.State(); st.Status != Connecting || st.Attempt != 0 || st.Message != "No audio over UDP, trying TCP" {
		t.Errorf("state during fallback = %+v", st)
	}

	f.started()
	st := waitStatus(t, c, Playing)
	if st.Via != "tcp" || st.Message != "" {
		t.Errorf("state after fallback = %+v", st)
	}

	// The network doesn't pass UDP, so the next stream goes straight to TCP
	// instead of sitting through the same silence.
	c.Play(udpFirstB)
	if f.lastVia() != "tcp" || f.loadCount() != 3 {
		t.Errorf("next stream: via %q after %d loads, want tcp straight away", f.lastVia(), f.loadCount())
	}
	// ...and a stream pinned to UDP is still honoured.
	c.Play(Target{StreamID: "c", URL: "rtsp://x/c", RTSPTransports: []string{"udp"}})
	if f.lastVia() != "udp" {
		t.Errorf("UDP-only stream went over %q", f.lastVia())
	}
}

func TestOfflineStreamDoesNotFallBack(t *testing.T) {
	c, f := start(t, defaultOpts, testTuning())
	c.Play(udpFirstA)
	f.failed("Server returned 404 Not Found")
	waitStatus(t, c, Reconnecting)

	// The retry is over UDP again: being offline says nothing about UDP.
	waitFor(t, "retry", func() bool { return f.loadCount() == 2 })
	if f.lastVia() != "udp" {
		t.Errorf("retry over %q, want udp", f.lastVia())
	}
}

func TestFallsBackAtOnceWhenUDPIsRefused(t *testing.T) {
	c, f := start(t, defaultOpts, testTuning()) // FallbackAfter is far away
	c.Play(udpFirstA)
	f.failed("method SETUP failed: 461 Unsupported transport")

	waitFor(t, "TCP attempt", func() bool { return c.State().Via == "tcp" })
	if st := c.State(); f.lastVia() != "tcp" || st.Status != Connecting || st.Attempt != 0 {
		t.Errorf("via %q, state %+v", f.lastVia(), st)
	}
}

func TestStopCancelsFallback(t *testing.T) {
	tuning := testTuning()
	tuning.FallbackAfter = 40 * time.Millisecond
	c, f := start(t, defaultOpts, tuning)
	c.Play(udpFirstA)
	c.Stop()
	time.Sleep(3 * tuning.FallbackAfter)
	if f.loadCount() != 1 || c.State().Status != Idle {
		t.Errorf("loads = %d, status = %s after stop", f.loadCount(), c.State().Status)
	}
}
