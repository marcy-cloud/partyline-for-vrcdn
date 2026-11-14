// Package sender pushes audio, and only audio, to an RTMP ingest: the DJ's
// side of the link that the player listens to. It captures from an input
// device, encodes AAC and sends it with libmpv's encoding mode, and keeps
// the connection up.
package sender

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"partyline/internal/libmpv"
	"partyline/internal/meter"
)

// Status is the coarse state of the sender.
type Status string

const (
	Off          Status = "off"
	Connecting   Status = "connecting"
	Live         Status = "live"
	Reconnecting Status = "reconnecting"
	Failed       Status = "failed"
)

// State is a snapshot of the sender for the UI.
type State struct {
	Status Status `json:"status"`
	// Message explains Reconnecting and Failed.
	Message string `json:"message"`
	// Attempt counts reconnect attempts since audio last went out.
	Attempt int `json:"attempt"`
	// SinceMs is when this broadcast first went live (Unix ms), or 0.
	SinceMs int64 `json:"sinceMs"`
}

// Config is what one broadcast needs.
type Config struct {
	// Ingest is the RTMP address without the key, e.g.
	// rtmp://ingest.vrcdn.live/live.
	Ingest string
	// Key is the stream key. It is a secret: it is never logged, and is
	// removed from any engine message before that leaves this package.
	Key string
	// Device is a Device.ID.
	Device string
	// BitrateKbps is the AAC bitrate.
	BitrateKbps int
	// Encoder is an Encoder.ID; "" means the default.
	Encoder string
}

// Encoder is a choice of AAC encoder.
type Encoder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// The AAC encoders. FFmpeg's is built in and always there. Apple's, which
// OBS calls CoreAudio AAC, is used only if Apple's own software (iTunes)
// has put it on the PC: it may not be redistributed.
var (
	EncoderFFmpeg    = Encoder{ID: "ffmpeg", Name: "FFmpeg AAC"}
	EncoderCoreAudio = Encoder{ID: "coreaudio", Name: "CoreAudio AAC (Apple)"}
)

// Encoders lists the encoders that can be used on this PC, default first.
func Encoders() []Encoder {
	if coreAudioInstalled() {
		return []Encoder{EncoderFFmpeg, EncoderCoreAudio}
	}
	return []Encoder{EncoderFFmpeg}
}

// openSession starts a connection with the encoder the config asks for.
func openSession(cfg Config) (session, error) {
	if cfg.Encoder == EncoderCoreAudio.ID {
		return openCoreAudio(cfg)
	}
	return openMPV(cfg)
}

// Bitrates are the AAC bitrates offered, in kbps. VRCDN's limit is 320.
var Bitrates = []int{320, 256, 192}

// Hooks are optional callbacks, called from the sender's goroutine.
type Hooks struct {
	State  func(State)
	Levels func(meter.Levels)
}

// Tuning holds timings that are fixed in the app but shortened in tests.
type Tuning struct {
	// Backoff is the wait before reconnect attempt n (1-based).
	Backoff func(attempt int) time.Duration
	// GiveUpAfter is how many attempts in a row may fail before the first
	// audio has ever gone out. A wrong key or address fails every time, and
	// there is no point hammering the server with it.
	GiveUpAfter int
	// StallTimeout is how long the input may deliver nothing while live.
	StallTimeout time.Duration
}

// DefaultTuning returns the timings used by the app.
func DefaultTuning() Tuning {
	return Tuning{
		Backoff:      func(attempt int) time.Duration { return time.Duration(min(attempt, 3)) * time.Second },
		GiveUpAfter:  3,
		StallTimeout: 5 * time.Second,
	}
}

// Sender runs at most one broadcast at a time.
type Sender struct {
	hooks  Hooks
	tuning Tuning
	// open starts one connection; replaced in tests.
	open func(Config) (session, error)

	mu    sync.Mutex
	state State
	stop  chan struct{} // closed to end the current broadcast
	done  chan struct{} // closed when its goroutine has finished
}

// New returns an idle sender.
func New(tuning Tuning, hooks Hooks) *Sender {
	return &Sender{hooks: hooks, tuning: tuning, open: openSession, state: State{Status: Off}}
}

// State returns the current snapshot.
func (s *Sender) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Start begins broadcasting. It returns once the broadcast is under way in
// the background; progress arrives through the State hook.
func (s *Sender) Start(cfg Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	s.Stop()

	s.mu.Lock()
	s.stop, s.done = make(chan struct{}), make(chan struct{})
	stop, done := s.stop, s.done
	s.mu.Unlock()

	s.set(State{Status: Connecting})
	go func() {
		defer close(done)
		s.run(cfg, stop)
	}()
	return nil
}

// Stop ends the broadcast, if there is one, and waits for it to finish.
func (s *Sender) Stop() {
	s.mu.Lock()
	stop, done := s.stop, s.done
	s.stop, s.done = nil, nil
	s.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
	s.set(State{Status: Off})
}

// LocalIngest is the start of an address that isn't an RTMP server: a TCP
// listener on this PC, which is sent the same AAC in FLV without RTMP around
// it. Measurement tools use it to time the sender with no network involved.
// Only the FFmpeg encoder can send to it.
const LocalIngest = "tcp://127.0.0.1:"

func (cfg Config) validate() error {
	switch {
	case !strings.HasPrefix(cfg.Ingest, "rtmp://") && !strings.HasPrefix(cfg.Ingest, "rtmps://") && !strings.HasPrefix(cfg.Ingest, LocalIngest):
		return errors.New("choose a server to send to")
	case cfg.Key == "":
		return errors.New("enter your stream key first")
	case strings.ContainsAny(cfg.Key, " \t\r\n/"):
		return errors.New("that doesn't look like a stream key: it has spaces or slashes in it")
	case cfg.Device == "":
		return errors.New("choose an input to send")
	case cfg.BitrateKbps < 64 || cfg.BitrateKbps > 320:
		return errors.New("the bitrate must be between 64 and 320 kbps")
	}
	return nil
}

func (cfg Config) target() string { return strings.TrimRight(cfg.Ingest, "/") + "/" + cfg.Key }

// scrub removes the stream key from text that is about to be shown.
func (cfg Config) scrub(text string) string {
	return strings.ReplaceAll(text, cfg.Key, "<key>")
}

func (s *Sender) set(next State) {
	s.mu.Lock()
	changed := s.state != next
	s.state = next
	s.mu.Unlock()
	if changed && s.hooks.State != nil {
		s.hooks.State(next)
	}
}

// update changes the state unless the broadcast has been stopped meanwhile.
func (s *Sender) update(stop <-chan struct{}, change func(*State)) {
	select {
	case <-stop:
		return
	default:
	}
	next := s.State()
	change(&next)
	s.set(next)
}

// session is one connection to the ingest.
type session interface {
	// wait blocks until something happens or the timeout passes.
	wait(timeout time.Duration) sessionEvent
	// position is how much audio has been encoded so far, in seconds.
	position() (float64, bool)
	levels() (meter.Levels, bool)
	close()
}

type sessionEvent struct {
	kind   sessionEventKind
	detail string // sessionEnded: why, if known
}

type sessionEventKind int

const (
	sessionNothing sessionEventKind = iota
	sessionStarted                  // audio is going out
	sessionEnded
)

// run keeps a broadcast going until it is stopped or has to give up.
func (s *Sender) run(cfg Config, stop <-chan struct{}) {
	everLive := false
	failures := 0
	for {
		detail, live := s.attempt(cfg, stop)
		select {
		case <-stop:
			return
		default:
		}
		if live {
			everLive, failures = true, 0
		}
		failures++
		msg := explain(cfg.scrub(detail))

		if !everLive && failures >= s.tuning.GiveUpAfter {
			s.update(stop, func(st *State) { *st = State{Status: Failed, Message: msg} })
			return
		}
		s.update(stop, func(st *State) {
			st.Status, st.Message, st.Attempt = Reconnecting, msg, failures
		})
		select {
		case <-stop:
			return
		case <-time.After(s.tuning.Backoff(failures)):
		}
	}
}

// attempt makes one connection and stays with it until it ends. It reports
// why it ended and whether audio ever went out on it.
func (s *Sender) attempt(cfg Config, stop <-chan struct{}) (detail string, live bool) {
	sess, err := s.open(cfg)
	if err != nil {
		return err.Error(), false
	}
	defer sess.close()

	const tick = 40 * time.Millisecond
	var lastPos float64
	lastProgress := time.Now()
	for {
		select {
		case <-stop:
			return "", live
		default:
		}
		ev := sess.wait(tick)
		switch ev.kind {
		case sessionStarted:
			live = true
			lastProgress = time.Now()
			s.update(stop, func(st *State) {
				if st.SinceMs == 0 {
					st.SinceMs = time.Now().UnixMilli()
				}
				st.Status, st.Message, st.Attempt = Live, "", 0
			})
		case sessionEnded:
			return ev.detail, live
		}
		if !live {
			continue
		}
		if l, ok := sess.levels(); ok && s.hooks.Levels != nil {
			s.hooks.Levels(l)
		}
		// An input that stops delivering doesn't end the connection; the
		// encoder just has nothing to send.
		if pos, ok := sess.position(); ok && pos != lastPos {
			lastPos, lastProgress = pos, time.Now()
		} else if time.Since(lastProgress) > s.tuning.StallTimeout {
			return "the input stopped delivering audio", live
		}
	}
}

// explain turns engine failure text into something a DJ can act on.
func explain(detail string) string {
	low := strings.ToLower(detail)
	has := func(parts ...string) bool {
		for _, p := range parts {
			if strings.Contains(low, p) {
				return true
			}
		}
		return false
	}
	switch {
	case detail == "":
		return "The connection to the server was lost"
	case has("stream is locked"):
		return "This stream key is already in use. Is something else sending to it? If it only just stopped, give it a moment."
	case has("auth failed", "invalid stream key", "code:403", "unauthorized", "badauth"):
		return "The server refused the stream key. Check it against the VRCDN panel."
	case has("netstream.publish", "badname", "rejected"):
		return "The server refused the stream. Is something else already sending to this key?"
	case has("failed to resolve", "cannot open connection", "connection refused", "network is unreachable", "timed out",
		"no such host", "dial tcp", "deadline exceeded"):
		return "Can't reach the server"
	case has("could not find audio only device"):
		return "The input device isn't available. Is it plugged in?"
	}
	return detail
}

// mpvSession is a session on a libmpv core in encoding mode.
type mpvSession struct {
	m *libmpv.Client
	// app is the program being sent, when the input is one.
	app     *appSource
	errors  []string
	started bool
}

func openMPV(cfg Config) (session, error) {
	// A program's sound is captured here and handed to libmpv as raw PCM.
	var app *appSource
	if strings.HasPrefix(cfg.Device, AppInput) {
		var err error
		if app, err = openApp(cfg.Device); err != nil {
			return nil, err
		}
		addr, err := app.serve()
		if err != nil {
			app.close()
			return nil, err
		}
		cfg.Device = PCMInput + addr
	}
	s, err := openMPVInput(cfg)
	if err != nil {
		if app != nil {
			app.close()
		}
		return nil, err
	}
	s.app = app
	return s, nil
}

func openMPVInput(cfg Config) (*mpvSession, error) {
	input, inputOptions, err := inputURL(cfg.Device)
	if err != nil {
		return nil, err
	}
	m, err := libmpv.New()
	if err != nil {
		return nil, err
	}
	options := [][2]string{
		{"config", "no"}, {"terminal", "no"}, {"ytdl", "no"}, {"load-scripts", "no"},
		{"vid", "no"}, {"sid", "no"}, {"audio-display", "no"},
		// Nothing may sit between the input and the encoder.
		{"cache", "no"},
		{"network-timeout", "10"},
		// Encode instead of play: AAC in FLV, the form RTMP carries.
		{"o", cfg.target()},
		{"of", "flv"},
		{"oac", "aac"},
		{"oacopts", fmt.Sprintf("b=%dk", cfg.BitrateKbps)},
		// What VR platforms expect; mpv converts if the input differs.
		{"audio-samplerate", "48000"},
		{"audio-channels", "stereo"},
		{"af", filters(cfg.Device, meter.Filter)},
	}
	for _, kv := range append(options, inputOptions...) {
		if err := m.SetOption(kv[0], kv[1]); err != nil {
			m.Destroy()
			return nil, fmt.Errorf("option %s: %w", kv[0], err)
		}
	}
	if err := m.Initialize(); err != nil {
		m.Destroy()
		return nil, err
	}
	_ = m.RequestLogMessages("error")
	if err := m.Command("loadfile", input); err != nil {
		m.Destroy()
		return nil, err
	}
	return &mpvSession{m: m}, nil
}

func (s *mpvSession) wait(timeout time.Duration) sessionEvent {
	if s.app != nil {
		select {
		case detail := <-s.app.ended:
			return sessionEvent{kind: sessionEnded, detail: detail}
		default:
		}
	}
	ev := s.m.WaitEvent(timeout.Seconds())
	switch ev.ID {
	case libmpv.EventLogMessage:
		s.errors = append(s.errors, ev.LogText)
		if len(s.errors) > 6 {
			s.errors = s.errors[len(s.errors)-6:]
		}
	case libmpv.EventPlaybackRestart:
		if !s.started {
			s.started = true
			return sessionEvent{kind: sessionStarted}
		}
	case libmpv.EventEndFile, libmpv.EventShutdown:
		detail := strings.Join(s.errors, " · ")
		if detail == "" {
			detail = ev.EndErr
		}
		return sessionEvent{kind: sessionEnded, detail: detail}
	}
	return sessionEvent{}
}

func (s *mpvSession) position() (float64, bool) {
	pos, err := s.m.GetDouble("time-pos")
	return pos, err == nil
}

func (s *mpvSession) levels() (meter.Levels, bool) { return meter.Read(s.m) }

// Destroying the core closes the output properly.
func (s *mpvSession) close() {
	s.m.Destroy()
	if s.app != nil {
		s.app.close()
	}
}
