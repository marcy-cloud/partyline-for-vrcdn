package sender

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"partyline/internal/meter"
)

// fakeSession is a connection the test drives by hand.
type fakeSession struct {
	events chan sessionEvent
	mu     sync.Mutex
	pos    float64
	closed bool
}

func (f *fakeSession) wait(timeout time.Duration) sessionEvent {
	select {
	case ev := <-f.events:
		return ev
	case <-time.After(timeout):
		f.mu.Lock()
		f.pos += timeout.Seconds() // audio keeps flowing
		f.mu.Unlock()
		return sessionEvent{}
	}
}
func (f *fakeSession) position() (float64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pos, true
}
func (f *fakeSession) levels() (meter.Levels, bool) { return meter.Levels{Left: -12, Right: -12}, true }
func (f *fakeSession) close()                       { f.mu.Lock(); f.closed = true; f.mu.Unlock() }

type harness struct {
	s        *Sender
	sessions chan *fakeSession
	states   chan State
	openErr  error
}

func newHarness(t *testing.T) *harness {
	h := &harness{sessions: make(chan *fakeSession, 16), states: make(chan State, 64)}
	tuning := Tuning{
		Backoff:      func(int) time.Duration { return 10 * time.Millisecond },
		GiveUpAfter:  3,
		StallTimeout: time.Hour,
	}
	h.s = New(tuning, Hooks{State: func(st State) { h.states <- st }})
	h.s.open = func(Config) (session, error) {
		if h.openErr != nil {
			return nil, h.openErr
		}
		f := &fakeSession{events: make(chan sessionEvent, 4)}
		h.sessions <- f
		return f, nil
	}
	t.Cleanup(h.s.Stop)
	return h
}

func (h *harness) session(t *testing.T) *fakeSession {
	t.Helper()
	select {
	case f := <-h.sessions:
		return f
	case <-time.After(2 * time.Second):
		t.Fatal("no connection was opened")
		return nil
	}
}

func (h *harness) waitFor(t *testing.T, want Status) State {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case st := <-h.states:
			if st.Status == want {
				return st
			}
		case <-deadline:
			t.Fatalf("never reached %q (now %+v)", want, h.s.State())
		}
	}
}

var good = Config{Ingest: "rtmp://ingest.example/live", Key: "sekrit_key_123", Device: TestTone.ID, BitrateKbps: 320}

func TestGoLiveAndStop(t *testing.T) {
	h := newHarness(t)
	if err := h.s.Start(good); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, Connecting)
	f := h.session(t)
	f.events <- sessionEvent{kind: sessionStarted}
	if st := h.waitFor(t, Live); st.SinceMs == 0 {
		t.Error("no start time while live")
	}

	h.s.Stop()
	if st := h.s.State(); st.Status != Off {
		t.Errorf("after stop: %+v", st)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		t.Error("the connection was left open")
	}
}

func TestReconnectsAfterADrop(t *testing.T) {
	h := newHarness(t)
	h.s.Start(good)
	first := h.session(t)
	first.events <- sessionEvent{kind: sessionStarted}
	since := h.waitFor(t, Live).SinceMs

	first.events <- sessionEvent{kind: sessionEnded, detail: "Connection reset by peer"}
	if st := h.waitFor(t, Reconnecting); st.Attempt != 1 || st.Message == "" {
		t.Errorf("after the drop: %+v", st)
	}
	second := h.session(t)
	second.events <- sessionEvent{kind: sessionStarted}
	// One broadcast as far as the DJ is concerned: the clock keeps running.
	if st := h.waitFor(t, Live); st.SinceMs != since || st.Attempt != 0 {
		t.Errorf("back live: %+v, want the original start time %d", st, since)
	}
}

// A wrong key fails every time; three tries and it stops, rather than
// knocking on the server's door all night.
func TestGivesUpWhenItNeverGoesLive(t *testing.T) {
	h := newHarness(t)
	h.s.Start(good)
	for i := 0; i < 3; i++ {
		h.session(t).events <- sessionEvent{kind: sessionEnded, detail: "NetStream.Publish.BadName for rtmp://ingest.example/live/sekrit_key_123"}
	}
	st := h.waitFor(t, Failed)
	if !strings.Contains(st.Message, "already sending") {
		t.Errorf("message = %q", st.Message)
	}
	select {
	case <-h.sessions:
		t.Error("kept trying after giving up")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestTheKeyNeverAppearsInMessages(t *testing.T) {
	h := newHarness(t)
	h.s.Start(good)
	h.session(t).events <- sessionEvent{kind: sessionEnded, detail: "weird failure writing to rtmp://ingest.example/live/sekrit_key_123"}
	st := h.waitFor(t, Reconnecting)
	if strings.Contains(st.Message, good.Key) || !strings.Contains(st.Message, "<key>") {
		t.Errorf("message leaks or lost the key marker: %q", st.Message)
	}
}

func TestOpenFailureIsReported(t *testing.T) {
	h := newHarness(t)
	h.openErr = errors.New("libmpv was not found")
	h.s.Start(good)
	if st := h.waitFor(t, Failed); st.Message != "libmpv was not found" {
		t.Errorf("state = %+v", st)
	}
}

func TestStalledInputReconnects(t *testing.T) {
	h := newHarness(t)
	h.s.tuning.StallTimeout = 60 * time.Millisecond
	h.s.Start(good)
	f := h.session(t)
	f.events <- sessionEvent{kind: sessionStarted}
	h.waitFor(t, Live)
	// Freeze the encoder's clock: nothing is coming from the device.
	f.mu.Lock()
	f.pos = -1e9
	f.mu.Unlock()
	hold := make(chan struct{})
	go func() {
		for {
			select {
			case <-hold:
				return
			default:
				f.mu.Lock()
				f.pos = 42
				f.mu.Unlock()
				time.Sleep(time.Millisecond)
			}
		}
	}()
	st := h.waitFor(t, Reconnecting)
	close(hold)
	if !strings.Contains(st.Message, "stopped delivering") {
		t.Errorf("message = %q", st.Message)
	}
}

func TestValidate(t *testing.T) {
	bad := []func(*Config){
		func(c *Config) { c.Ingest = "" },
		func(c *Config) { c.Ingest = "https://example.com" },
		func(c *Config) { c.Key = "" },
		func(c *Config) { c.Key = "two words" },
		func(c *Config) { c.Key = "a/b" },
		func(c *Config) { c.Device = "" },
		func(c *Config) { c.BitrateKbps = 1000 },
	}
	for i, change := range bad {
		cfg := good
		change(&cfg)
		if cfg.validate() == nil {
			t.Errorf("case %d: %+v should be refused", i, cfg)
		}
	}
	if err := good.validate(); err != nil {
		t.Error(err)
	}
	if good.target() != "rtmp://ingest.example/live/sekrit_key_123" {
		t.Errorf("target = %q", good.target())
	}
}

func TestParseDevices(t *testing.T) {
	lines := []string{
		`dshow: "USB2.0 HD UVC WebCam" (video)`,
		`dshow:   Alternative name "@device_pnp_\\?\usb#vid_13d3"`,
		`dshow: "Microphone (Realtek(R) Audio)" (audio)`,
		`dshow:   Alternative name "@device_cm_{33D9}\wave_{BA72}"`,
		`dshow: "Line In (Mixer "Pro")" (audio)`,
		`dshow:   Alternative name "@device_cm_{33D9}\wave_{C13A}"`,
		`dshow: "Orphan with no alternative name" (audio)`,
		`dshow: something else entirely`,
	}
	want := []Device{
		{ID: `@device_cm_{33D9}\wave_{BA72}`, Name: "Microphone (Realtek(R) Audio)"},
		{ID: `@device_cm_{33D9}\wave_{C13A}`, Name: `Line In (Mixer "Pro")`},
	}
	if got := parseDevices(lines); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

// Messages as libmpv and ffmpeg actually word them.
func TestExplain(t *testing.T) {
	cases := map[string]string{
		`rtmp: Server error: [auth failed]: code:403 msg:"Invalid stream key or user disabled" · could not open 'rtmp://x/live/<key>'`:                          "refused the stream key",
		`tcp: Failed to resolve hostname nope.invalid · rtmp: Cannot open connection tcp://nope.invalid:1935 · could not open 'rtmp://nope.invalid/live/<key>'`: "Can't reach the server",
		`dshow: Could not find audio only device with name [@device_cm_{0}] among source devices of type audio. · avformat_open_input() failed`:                 "isn't available",
		"":                             "was lost",
		"something nobody anticipated": "something nobody anticipated",
	}
	for detail, want := range cases {
		if got := explain(detail); !strings.Contains(got, want) {
			t.Errorf("explain(%q) = %q, want it to mention %q", detail, got, want)
		}
	}
}
