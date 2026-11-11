package main

import (
	"context"
	"errors"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"vrcdn-lowlatency-player/internal/player"
	"vrcdn-lowlatency-player/internal/store"
	"vrcdn-lowlatency-player/internal/vrcdn"
)

const appTitle = "VRCDN Low Latency Player"

// App is the API the frontend calls. It owns nothing clever: saved streams
// live in the store, playback policy in the player controller.
type App struct {
	ctx    context.Context
	store  *store.Store
	engine player.Engine
	ctl    *player.Controller

	// engineErr is set when libmpv couldn't be loaded; the UI shows it with
	// instructions and playback is disabled.
	engineErr string
	// notice is a non-fatal startup problem, e.g. an unreadable config.
	notice string
}

// Bootstrap is everything the UI needs to draw its first frame.
type Bootstrap struct {
	Streams    []store.Stream       `json:"streams"`
	Settings   store.Settings       `json:"settings"`
	State      player.State         `json:"state"`
	Devices    []player.AudioDevice `json:"devices"`
	Engine     string               `json:"engine"`
	EngineErr  string               `json:"engineError"`
	EngineHelp string               `json:"engineHelp"`
	Notice     string               `json:"notice"`
	ConfigPath string               `json:"configPath"`
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	path, err := store.DefaultPath()
	if err != nil {
		path = "config.json"
	}
	a.store, err = store.Open(path)
	if err != nil {
		a.notice = err.Error()
	}
	settings := a.store.Settings()

	engine, err := player.NewMPVEngine(appTitle)
	if err != nil {
		a.engineErr = err.Error()
		return
	}
	a.engine = engine
	_ = engine.SetVolume(settings.Volume)
	_ = engine.SetAudioDevice(settings.AudioDevice)

	a.ctl = player.NewController(engine, controllerOptions(settings), player.DefaultTuning(), player.Hooks{
		State: func(s player.State) { runtime.EventsEmit(ctx, "state", s) },
		Devices: func() {
			if devices, err := engine.AudioDevices(); err == nil {
				runtime.EventsEmit(ctx, "devices", devices)
			}
		},
	})
}

func (a *App) shutdown(context.Context) {
	if a.ctl != nil {
		a.ctl.Close()
	}
	if a.engine != nil {
		a.engine.Close()
	}
}

func controllerOptions(s store.Settings) player.Options {
	return player.Options{
		AutoReconnect:  s.AutoReconnect,
		AutoCatchUp:    s.AutoCatchUp,
		CatchUpAboveMs: s.CatchUpAboveMs,
	}
}

func (a *App) ready() error {
	if a.ctl == nil {
		return errors.New(a.engineErr)
	}
	return nil
}

// Bootstrap returns the initial UI data.
func (a *App) Bootstrap() Bootstrap {
	b := Bootstrap{
		Streams:    a.store.Streams(),
		Settings:   a.store.Settings(),
		State:      player.State{Status: player.Idle, BufferMs: -1},
		Devices:    []player.AudioDevice{},
		EngineErr:  a.engineErr,
		Notice:     a.notice,
		ConfigPath: a.store.Path(),
	}
	if a.ctl == nil {
		b.EngineHelp = player.LibraryHelp()
		return b
	}
	b.State = a.ctl.State()
	b.Engine = a.engine.Describe()
	if devices, err := a.engine.AudioDevices(); err == nil {
		b.Devices = devices
	}
	return b
}

// SaveStream creates (empty ID) or updates a saved stream.
func (a *App) SaveStream(in store.Stream) (store.Stream, error) {
	return a.store.SaveStream(in)
}

// DeleteStream removes a saved stream and returns the remaining list.
func (a *App) DeleteStream(id string) ([]store.Stream, error) {
	err := a.store.DeleteStream(id)
	return a.store.Streams(), err
}

// MoveStream repositions a saved stream and returns the reordered list.
func (a *App) MoveStream(id string, to int) ([]store.Stream, error) {
	err := a.store.MoveStream(id, to)
	return a.store.Streams(), err
}

// Play switches playback to a saved stream.
func (a *App) Play(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	st, ok := a.store.Stream(id)
	if !ok {
		return errors.New("that stream no longer exists")
	}
	src, err := vrcdn.Parse(st.Source)
	if err != nil {
		return err
	}
	target := player.Target{
		StreamID: st.ID,
		Label:    st.Label,
		URL:      src.URL(st.Transport),
	}
	target.RTSPTransports = src.RTSPTransports(st.Transport)
	a.ctl.Play(target)
	return nil
}

// Stop ends playback.
func (a *App) Stop() {
	if a.ctl != nil {
		a.ctl.Stop()
	}
}

// Resync drops buffered audio to get back to live.
func (a *App) Resync() {
	if a.ctl != nil {
		a.ctl.Resync()
	}
}

// SetVolume applies a volume (0-100) immediately without saving it; the UI
// calls this while the slider is being dragged and UpdateSettings on release.
func (a *App) SetVolume(volume float64) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.engine.SetVolume(volume)
}

// UpdateSettings saves the settings and applies them to the player.
func (a *App) UpdateSettings(in store.Settings) (store.Settings, error) {
	saved, err := a.store.SetSettings(in)
	if a.ctl != nil {
		a.ctl.SetOptions(controllerOptions(saved))
		if e := a.engine.SetVolume(saved.Volume); e != nil && err == nil {
			err = e
		}
		if e := a.engine.SetAudioDevice(saved.AudioDevice); e != nil && err == nil {
			err = e
		}
	}
	return saved, err
}
