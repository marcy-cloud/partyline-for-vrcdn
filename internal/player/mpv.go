package player

import (
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"vrcdn-lowlatency-player/internal/libmpv"
)

// tickInterval is how often the buffer and position are sampled.
const tickInterval = 250 * time.Millisecond

// levelInterval is how often the signal level is sampled for the meter:
// about 20 times a second once timer granularity is added.
const levelInterval = 40 * time.Millisecond

// The level meter is ffmpeg's astats filter, which tags each audio frame
// with its per-channel peak. It only measures; the audio passes through
// untouched and unbuffered, ahead of the volume control.
const (
	levelFilter   = "@levels:lavfi=[astats=metadata=1:reset=1:measure_perchannel=Peak_level:measure_overall=none]"
	levelProperty = "af-metadata/levels"
)

// MPVEngine plays audio through an embedded libmpv core configured for
// minimum latency.
type MPVEngine struct {
	mpv    *libmpv.Client
	events chan Event
	quit   chan struct{}
	done   chan struct{}
	once   sync.Once

	mu       sync.Mutex
	errLines []string
}

// lowLatencyOptions is what makes this a low-latency player. It is the
// explicit form of mpv's "low-latency" profile plus audio-only settings, so
// behaviour doesn't depend on which profiles a given libmpv build ships.
var lowLatencyOptions = [][2]string{
	// Audio only.
	{"vid", "no"},
	{"sid", "no"},
	{"audio-display", "no"},
	{"vo", "null"},

	// No buffering anywhere we control.
	{"cache", "no"},
	{"cache-pause", "no"},
	{"audio-buffer", "0"},
	{"stream-buffer-size", "4k"},
	{"demuxer-lavf-o", "fflags=+nobuffer"},
	{"demuxer-lavf-probe-info", "nostreams"},
	{"demuxer-lavf-analyzeduration", "0.1"},
	{"gapless-audio", "no"},
	// Not a buffer but a limit on how far the demuxer reads ahead. At the
	// default of 1 s, anything beyond that waits unseen in the network
	// socket: the buffer readout would top out near 1000 ms and a resync
	// would refill straight away. Reading everything in keeps the readout
	// honest and lets catch-up and resync act on the whole backlog. It must
	// stay above the largest hard-drop threshold (20 s).
	{"demuxer-readahead-secs", "30"},

	// Network.
	{"rtsp-transport", "tcp"},
	{"network-timeout", "10"},

	{"volume-max", "100"},
}

// embedOptions keep mpv from behaving like a standalone app.
var embedOptions = [][2]string{
	{"config", "no"},
	{"terminal", "no"},
	{"idle", "yes"},
	{"ytdl", "no"},
	{"load-scripts", "no"},
	{"osc", "no"},
	{"input-default-bindings", "no"},
	{"input-vo-keyboard", "no"},
	{"input-media-keys", "no"},
}

// NewMPVEngine loads libmpv and starts an idle core. clientName is how the
// app appears in the OS volume mixer.
func NewMPVEngine(clientName string) (*MPVEngine, error) {
	return newMPVEngine(clientName)
}

// newMPVEngine takes extra mpv options, applied last; tests use it to play
// to a null audio output.
func newMPVEngine(clientName string, extra ...[2]string) (*MPVEngine, error) {
	m, err := libmpv.New()
	if err != nil {
		return nil, err
	}

	// Options vary a little between mpv versions; an unknown one shouldn't
	// stop the player from starting.
	for _, group := range [][][2]string{embedOptions, lowLatencyOptions, {{"af", levelFilter}, {"audio-client-name", clientName}}, extra} {
		for _, kv := range group {
			_ = m.SetOption(kv[0], kv[1])
		}
	}

	if err := m.Initialize(); err != nil {
		m.Destroy()
		return nil, err
	}
	// Picks up anything newer mpv versions add to the profile.
	_ = m.Command("apply-profile", "low-latency")
	_ = m.RequestLogMessages("error")
	_ = m.ObserveProperty(1, "audio-device-list")

	e := &MPVEngine{
		mpv:    m,
		events: make(chan Event, 64),
		quit:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go e.loop()
	return e, nil
}

func (e *MPVEngine) Events() <-chan Event { return e.events }

func (e *MPVEngine) Load(url, rtspTransport string) (int64, error) {
	e.mu.Lock()
	e.errLines = nil
	e.mu.Unlock()

	if rtspTransport != "" {
		if err := e.mpv.SetProperty("rtsp-transport", rtspTransport); err != nil {
			return 0, err
		}
	}
	// Speed is a core-wide property; never inherit a catch-up rate.
	_ = e.mpv.SetProperty("speed", "1")

	res, err := e.mpv.CommandRet("loadfile", url, "replace")
	if err != nil {
		return 0, err
	}
	// mpv >= 0.33 reports the playlist entry it created, which the
	// start-file and end-file events then carry. Older versions report
	// nothing and every event carries 0, which still lines up.
	if m, ok := res.(map[string]any); ok {
		if id, ok := m["playlist_entry_id"].(int64); ok {
			return id, nil
		}
	}
	return 0, nil
}

func (e *MPVEngine) Stop() error { return e.mpv.Command("stop") }

func (e *MPVEngine) SetSpeed(speed float64) error {
	return e.mpv.SetProperty("speed", strconv.FormatFloat(speed, 'f', 3, 64))
}

func (e *MPVEngine) DropBuffers() error { return e.mpv.Command("drop-buffers") }

func (e *MPVEngine) SetVolume(volume float64) error {
	return e.mpv.SetProperty("volume", strconv.FormatFloat(min(max(volume, 0), 100), 'f', 1, 64))
}

func (e *MPVEngine) SetAudioDevice(name string) error {
	if name == "" {
		name = "auto"
	}
	return e.mpv.SetProperty("audio-device", name)
}

func (e *MPVEngine) AudioDevices() ([]AudioDevice, error) {
	node, err := e.mpv.GetNode("audio-device-list")
	if err != nil {
		return nil, err
	}
	list, _ := node.([]any)
	out := make([]AudioDevice, 0, len(list))
	for _, item := range list {
		m, _ := item.(map[string]any)
		name, _ := m["name"].(string)
		desc, _ := m["description"].(string)
		if name == "" || !wantDevice(name) {
			continue
		}
		if name == "auto" {
			desc = "System default"
		} else if desc == "" {
			desc = name
		}
		out = append(out, AudioDevice{Name: name, Description: desc})
	}
	return out, nil
}

// wantDevice hides the duplicate listings mpv produces for secondary audio
// backends on Windows, where WASAPI is the one that matters.
func wantDevice(name string) bool {
	if runtime.GOOS != "windows" {
		return true
	}
	return name == "auto" || strings.HasPrefix(name, "wasapi/")
}

func (e *MPVEngine) Describe() string {
	v, err := e.mpv.GetString("mpv-version")
	if err != nil {
		return "libmpv"
	}
	return v
}

// Close stops the event loop and destroys the core.
func (e *MPVEngine) Close() {
	e.once.Do(func() {
		close(e.quit)
		e.mpv.Wakeup()
		<-e.done
		e.mpv.Destroy()
	})
}

func (e *MPVEngine) emit(ev Event) bool {
	select {
	case e.events <- ev:
		return true
	case <-e.quit:
		return false
	}
}

func (e *MPVEngine) closing() bool {
	select {
	case <-e.quit:
		return true
	default:
		return false
	}
}

func (e *MPVEngine) loop() {
	defer close(e.done)
	defer close(e.events)

	var (
		active    int64 // entry between start-file and end-file
		loaded    bool
		started   bool
		info      StreamInfo
		lastTick  time.Time
		lastLevel time.Time
	)

	for !e.closing() {
		// Wake often enough for the meter while audio is playing, and only
		// for the slower tick otherwise.
		wait := tickInterval
		if started {
			wait = levelInterval
		}
		ev := e.mpv.WaitEvent(wait.Seconds())

		switch ev.ID {
		case libmpv.EventShutdown:
			return

		case libmpv.EventStartFile:
			active, loaded, started, info = ev.EntryID, true, false, StreamInfo{}

		case libmpv.EventPlaybackRestart:
			if loaded {
				started = true
				if !e.emit(Event{Kind: EvStarted, Entry: active}) {
					return
				}
			}

		case libmpv.EventEndFile:
			out := Event{Kind: EvEnded, Entry: ev.EntryID}
			switch ev.Reason {
			case libmpv.EndError:
				out.Reason = EndError
				out.Detail = e.failureDetail(ev.EndErr)
			case libmpv.EndEOF:
				out.Reason = EndEOF
			default:
				out.Reason = EndStopped
			}
			if ev.EntryID == active {
				loaded, started = false, false
			}
			if !e.emit(out) {
				return
			}

		case libmpv.EventLogMessage:
			e.mu.Lock()
			e.errLines = append(e.errLines, ev.LogText)
			if len(e.errLines) > 8 {
				e.errLines = e.errLines[len(e.errLines)-8:]
			}
			e.mu.Unlock()

		case libmpv.EventPropertyChange:
			if ev.Property == "audio-device-list" {
				if !e.emit(Event{Kind: EvDevices}) {
					return
				}
			}
		}

		// (Not a full interval: the wait above often returns a hair early,
		// which would otherwise skip every other reading.)
		if started && time.Since(lastLevel) >= levelInterval*3/4 {
			lastLevel = time.Now()
			if levels, ok := e.levels(); ok {
				if !e.emit(Event{Kind: EvLevels, Entry: active, Levels: levels}) {
					return
				}
			}
		}

		if started && time.Since(lastTick) >= tickInterval {
			lastTick = time.Now()
			if info.Codec == "" || info.SampleRate == 0 || info.Channels == 0 {
				info = e.streamInfo()
			}
			tick := Event{Kind: EvTick, Entry: active, Info: info}
			// Unavailable just means nothing is queued.
			tick.BufferSec, _ = e.mpv.GetDouble("demuxer-cache-duration")
			if pos, err := e.mpv.GetDouble("time-pos"); err == nil {
				tick.PosSec, tick.HavePos = pos, true
			}
			if !e.emit(tick) {
				return
			}
		}
	}
}

// levels reads the most recent frame's peak levels from the meter filter.
func (e *MPVEngine) levels() (Levels, bool) {
	node, err := e.mpv.GetNode(levelProperty)
	if err != nil {
		return Levels{}, false
	}
	meta, _ := node.(map[string]any)
	left, okL := peakLevel(meta, 1)
	right, okR := peakLevel(meta, 2)
	switch {
	case okL && okR:
		return Levels{Left: left, Right: right}, true
	case okL: // mono
		return Levels{Left: left, Right: left}, true
	}
	return Levels{}, false
}

// peakLevel picks one channel's peak (dBFS) out of astats' frame metadata,
// where it is a string such as "-12.3" or "-inf".
func peakLevel(meta map[string]any, channel int) (float64, bool) {
	text, ok := meta["lavfi.astats."+strconv.Itoa(channel)+".Peak_level"].(string)
	if !ok {
		return 0, false
	}
	db, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false
	}
	// -inf and NaN can't go into JSON; neither is louder than Silence.
	if !(db > Silence) {
		db = Silence
	}
	return min(db, 0), true
}

func (e *MPVEngine) streamInfo() StreamInfo {
	var info StreamInfo
	info.Codec, _ = e.mpv.GetString("audio-codec-name")
	if v, err := e.mpv.GetDouble("audio-params/samplerate"); err == nil {
		info.SampleRate = int(v)
	}
	if v, err := e.mpv.GetDouble("audio-params/channel-count"); err == nil {
		info.Channels = int(v)
	}
	return info
}

// failureDetail picks the most useful description of why loading failed:
// the error lines mpv/ffmpeg logged since the load, falling back to mpv's
// generic end-file error.
func (e *MPVEngine) failureDetail(endErr string) string {
	e.mu.Lock()
	lines := append([]string{}, e.errLines...)
	e.mu.Unlock()

	if len(lines) == 0 {
		return endErr
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	detail := strings.Join(lines, " · ")
	if len(detail) > 240 {
		detail = detail[:240] + "…"
	}
	return detail
}

// LibraryHelp explains how to make libmpv available on this OS, for display
// when loading it failed.
func LibraryHelp() string {
	switch runtime.GOOS {
	case "windows":
		return "This build has no usable libmpv built in. Put libmpv-2.dll next to this program (it is inside the mpv-dev-x86_64 download from the mpv Windows builds), then restart."
	case "darwin":
		return "Install mpv with Homebrew (brew install mpv), then restart."
	default:
		return "Install libmpv with your package manager (for example: sudo apt install libmpv2), then restart."
	}
}
