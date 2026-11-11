package player

import (
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vrcdn-lowlatency-player/internal/libmpv"
)

// These tests drive the real libmpv against a local stand-in for a live
// stream: an endless WAV sent in real time over HTTP. They are skipped where
// libmpv isn't available, and play to a null output, so they are silent and
// need no audio hardware.
//
//	go test -tags bundled_libmpv ./internal/player/     # with the built-in DLL
//	LIBMPV_PATH=/path/to/libmpv go test ./internal/player/

const (
	liveRate     = 48000
	liveChannels = 2
)

// liveWAV streams a 440 Hz tone for as long as the client stays connected.
func liveWAV(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "audio/wav")
	flusher := w.(http.Flusher)

	// A WAV header that claims the maximum length: the stream has no end.
	header := make([]byte, 44)
	copy(header[0:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:], 0xFFFFFFFF)
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1) // PCM
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
	ticker := time.NewTicker(chunk)
	defer ticker.Stop()
	for n := 0; ; {
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
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func liveEngine(t *testing.T) (*MPVEngine, *httptest.Server) {
	t.Helper()
	if err := libmpv.Load(); err != nil {
		t.Skipf("libmpv is not available: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(liveWAV))
	t.Cleanup(srv.Close)
	engine, err := newMPVEngine("player test", [2]string{"ao", "null"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.Close) // runs before srv.Close
	t.Logf("%s from %s", engine.Describe(), libmpv.LibraryPath())
	return engine, srv
}

// nextEvent waits for the next event of the wanted kind about entry.
func nextEvent(t *testing.T, e *MPVEngine, kind EventKind, entry int64) Event {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case ev, ok := <-e.Events():
			if !ok {
				t.Fatal("engine closed its event stream")
			}
			if ev.Kind == kind && ev.Entry == entry {
				return ev
			}
		case <-deadline:
			t.Fatalf("no event of kind %d for entry %d within 20s", kind, entry)
		}
	}
}

func TestMPVEngineLiveStream(t *testing.T) {
	e, srv := liveEngine(t)

	if _, err := e.AudioDevices(); err != nil {
		t.Errorf("AudioDevices: %v", err)
	}

	entry, err := e.Load(srv.URL+"/live.wav", "")
	if err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	nextEvent(t, e, EvStarted, entry)
	t.Logf("audio started %d ms after Load", time.Since(began).Milliseconds())

	// The fixture's tone peaks at 8000/32768 of full scale on both channels.
	wantDB := 20 * math.Log10(8000.0/32768.0)
	levels := nextEvent(t, e, EvLevels, entry).Levels
	// Readings must come often enough to animate a meter.
	began = time.Now()
	for i := 0; i < 15; i++ {
		levels = nextEvent(t, e, EvLevels, entry).Levels
	}
	if took := time.Since(began); took > 1500*time.Millisecond {
		t.Errorf("15 level readings took %v; want about 20 a second", took)
	}
	t.Logf("levels: L %.1f dB, R %.1f dB (tone is %.1f dB)", levels.Left, levels.Right, wantDB)
	if math.Abs(levels.Left-wantDB) > 1 || math.Abs(levels.Right-wantDB) > 1 {
		t.Errorf("levels = %+v, want about %.1f dB on both channels", levels, wantDB)
	}

	// Position must advance and the format must be reported.
	first := nextEvent(t, e, EvTick, entry)
	var last Event
	for i := 0; i < 6; i++ {
		last = nextEvent(t, e, EvTick, entry)
		t.Logf("tick: pos %.2fs buffer %.0f ms %s %d Hz %dch",
			last.PosSec, last.BufferSec*1000, last.Info.Codec, last.Info.SampleRate, last.Info.Channels)
	}
	if !first.HavePos || !last.HavePos || last.PosSec <= first.PosSec {
		t.Errorf("position did not advance: %+v -> %+v", first, last)
	}
	if last.Info.Codec == "" || last.Info.SampleRate != liveRate || last.Info.Channels != liveChannels {
		t.Errorf("stream info = %+v", last.Info)
	}

	// The fixture takes a couple of seconds to open (ffmpeg probes WAV data
	// for S/PDIF), so a backlog has built up by now. It must show up in the
	// reading in full, and a resync must clear it.
	if err := e.DropBuffers(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		last = nextEvent(t, e, EvTick, entry)
	}
	if last.BufferSec > 0.25 {
		t.Errorf("buffer is still %.0f ms after dropping it", last.BufferSec*1000)
	}

	for _, call := range []struct {
		name string
		err  error
	}{
		{"SetSpeed", e.SetSpeed(1.1)},
		{"DropBuffers", e.DropBuffers()},
		{"SetVolume", e.SetVolume(40)},
		{"SetAudioDevice", e.SetAudioDevice("auto")},
	} {
		if call.err != nil {
			t.Errorf("%s: %v", call.name, call.err)
		}
	}
	nextEvent(t, e, EvTick, entry) // still playing after all that

	// The server dropping the connection must be reported as the stream
	// ending, not go unnoticed.
	srv.CloseClientConnections()
	if end := nextEvent(t, e, EvEnded, entry); end.Reason == EndStopped {
		t.Errorf("a dropped connection was reported as a deliberate stop")
	}

	// And a deliberate stop as exactly that.
	entry2, err := e.Load(srv.URL+"/live.wav", "")
	if err != nil {
		t.Fatal(err)
	}
	nextEvent(t, e, EvStarted, entry2)
	if err := e.Stop(); err != nil {
		t.Fatal(err)
	}
	if end := nextEvent(t, e, EvEnded, entry2); end.Reason != EndStopped {
		t.Errorf("stop reported as reason %d (%s)", end.Reason, end.Detail)
	}
}

func TestMPVEngineUnreachableStream(t *testing.T) {
	e, srv := liveEngine(t)
	gone := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(gone.Close)
	_ = srv

	entry, err := e.Load(gone.URL+"/nothing.live.flv", "")
	if err != nil {
		t.Fatal(err)
	}
	end := nextEvent(t, e, EvEnded, entry)
	if end.Reason != EndError {
		t.Errorf("reason = %d, want an error", end.Reason)
	}
	t.Logf("detail: %q -> %q", end.Detail, explain(end.Detail))
	if got := explain(end.Detail); got != "Stream is offline or doesn't exist" {
		t.Errorf("a 404 is explained as %q", got)
	}
}

// The whole stack: the controller reconnects by itself when the real engine
// loses its stream.
func TestControllerReconnectsWithMPV(t *testing.T) {
	e, srv := liveEngine(t)

	states := make(chan State, 256)
	tuning := DefaultTuning()
	tuning.Backoff = func(int) time.Duration { return 200 * time.Millisecond }
	ctl := NewController(e, Options{AutoReconnect: true, AutoCatchUp: true, CatchUpAboveMs: 250}, tuning, Hooks{
		State: func(s State) {
			select {
			case states <- s:
			default:
			}
		},
	})
	t.Cleanup(ctl.Close)

	waitFor := func(want Status) State {
		t.Helper()
		deadline := time.After(20 * time.Second)
		for {
			select {
			case s := <-states:
				if s.Status == want {
					return s
				}
			case <-deadline:
				t.Fatalf("never reached %q (now %q)", want, ctl.State().Status)
			}
		}
	}

	// settled waits for a low buffer reading: the backlog from connecting
	// has been dealt with and playback is at the live edge.
	settled := func() {
		t.Helper()
		deadline := time.After(20 * time.Second)
		for {
			select {
			case s := <-states:
				if s.Status == Playing && s.BufferMs >= 0 && s.BufferMs <= 250 && !s.CatchingUp {
					return
				}
			case <-deadline:
				t.Fatalf("never reached the live edge (buffer %d ms)", ctl.State().BufferMs)
			}
		}
	}

	ctl.Play(Target{StreamID: "a", Label: "tone", URL: srv.URL + "/live.wav"})
	began := time.Now()
	waitFor(Playing)
	settled()
	t.Logf("at the live edge %d ms after Play", time.Since(began).Milliseconds())
	srv.CloseClientConnections()
	lost := waitFor(Reconnecting)
	t.Logf("after the drop: %q (attempt %d)", lost.Message, lost.Attempt)
	waitFor(Playing)
	settled()
	ctl.Stop()
	waitFor(Idle)
}
