package player

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// Status is the coarse playback state shown in the UI.
type Status string

const (
	Idle         Status = "idle"
	Connecting   Status = "connecting"
	Playing      Status = "playing"
	Reconnecting Status = "reconnecting"
	Failed       Status = "failed"
)

// State is a snapshot of the player for the UI.
type State struct {
	Status   Status `json:"status"`
	StreamID string `json:"streamId"`
	Label    string `json:"label"`
	URL      string `json:"url"`
	// BufferMs is the queued-audio latency, or -1 when not known.
	BufferMs int `json:"bufferMs"`
	// CatchingUp is true while playing fast to drain the buffer.
	CatchingUp bool `json:"catchingUp"`
	// Message explains Reconnecting and Failed states, and a fallback in
	// progress while Connecting.
	Message string `json:"message"`
	// Attempt counts reconnect attempts since audio was last heard.
	Attempt int `json:"attempt"`
	// SinceMs is when audio started (Unix ms), or 0.
	SinceMs int64      `json:"sinceMs"`
	Info    StreamInfo `json:"info"`
	// Via is the RTSP transport in use ("udp" or "tcp"), or "" for streams
	// that aren't RTSP.
	Via string `json:"via"`
}

// Target is a stream to play.
type Target struct {
	StreamID string
	Label    string
	URL      string
	// RTSPTransports lists the RTSP transports to try, in order ("udp",
	// "tcp"). Empty for streams that aren't RTSP.
	RTSPTransports []string
}

// Options are the user-tunable policies.
type Options struct {
	// AutoReconnect retries forever while the stream is offline or drops.
	AutoReconnect bool
	// AutoCatchUp speeds playback up slightly when more than CatchUpAboveMs
	// of audio is queued, until the queue is short again.
	AutoCatchUp    bool
	CatchUpAboveMs int
}

// Tuning holds timings that are fixed in the app but shortened in tests.
type Tuning struct {
	// Backoff is the wait before reconnect attempt n (1-based).
	Backoff func(attempt int) time.Duration
	// ConnectTimeout is how long a load may take to produce audio.
	ConnectTimeout time.Duration
	// FallbackAfter is how long a load may take to produce audio before the
	// next RTSP transport is tried, when there is one. UDP that a router
	// drops connects fine and then stays silent, so this is the only tell.
	FallbackAfter time.Duration
	// StallTimeout is how long the position may stand still while playing.
	StallTimeout time.Duration
	// CatchUpSustain is how long the buffer must stay over the threshold
	// before catch-up starts, so a single burst doesn't trigger it.
	CatchUpSustain time.Duration
	// CatchUpSpeed is the playback rate used while catching up.
	CatchUpSpeed float64
}

// DefaultTuning returns the timings used by the app.
func DefaultTuning() Tuning {
	return Tuning{
		Backoff: func(attempt int) time.Duration {
			return time.Duration(min(attempt, 3)) * time.Second
		},
		ConnectTimeout: 12 * time.Second,
		FallbackAfter:  4 * time.Second,
		StallTimeout:   4 * time.Second,
		CatchUpSustain: time.Second,
		CatchUpSpeed:   1.1,
	}
}

// Controller drives an Engine. All policy runs on one goroutine, so the
// fields below it need no locking; the public methods run their work there.
// Hooks are called on that goroutine too and must not call back into the
// controller.
type Controller struct {
	engine Engine
	tuning Tuning
	hooks  Hooks

	cmds chan func()
	done chan struct{}
	wg   sync.WaitGroup

	mu       sync.Mutex
	snapshot State

	// Loop-owned.
	opts   Options
	state  State
	target *Target
	entry  int64
	// loading is true from Load until the stream starts or ends.
	loading      bool
	retry        <-chan time.Time
	connectLimit <-chan time.Time
	fallback     <-chan time.Time
	// transport indexes target.RTSPTransports: the one in use.
	transport int
	// learned is how far down that list a fallback has had to go. Whether
	// UDP gets through is a property of the network, not of one stream, so
	// later streams start there instead of waiting out the same failure.
	learned      int
	lastPos      float64
	lastProgress time.Time
	overSince    time.Time
	// fresh is true from a stream starting until its first buffer reading.
	fresh bool
}

// Hooks are optional callbacks, invoked from the controller goroutine.
type Hooks struct {
	// State is called whenever the state snapshot changes.
	State func(State)
	// Devices is called when the set of audio devices changes.
	Devices func()
}

// NewController starts a controller driving engine.
func NewController(engine Engine, opts Options, tuning Tuning, hooks Hooks) *Controller {
	c := &Controller{
		engine: engine,
		tuning: tuning,
		hooks:  hooks,
		cmds:   make(chan func()),
		done:   make(chan struct{}),
		opts:   opts,
		state:  State{Status: Idle, BufferMs: -1},
	}
	c.snapshot = c.state
	c.wg.Add(1)
	go c.loop()
	return c
}

// State returns the latest snapshot.
func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshot
}

// Play switches to a stream.
func (c *Controller) Play(t Target) { c.do(func() { c.play(t) }) }

// Stop ends playback and any pending reconnect.
func (c *Controller) Stop() { c.do(c.stop) }

// Resync drops queued audio to get back to live. While waiting to reconnect
// it retries immediately instead.
func (c *Controller) Resync() { c.do(c.resync) }

// SetOptions applies new policies.
func (c *Controller) SetOptions(o Options) { c.do(func() { c.setOptions(o) }) }

// Close stops the controller goroutine. It does not close the engine.
func (c *Controller) Close() {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	c.wg.Wait()
}

// do runs f on the controller goroutine and waits for it, so the effect of a
// call (and the State it produces) is visible as soon as the method returns.
func (c *Controller) do(f func()) {
	ack := make(chan struct{})
	run := func() {
		f()
		c.publish()
		close(ack)
	}
	select {
	case c.cmds <- run:
	case <-c.done:
		return
	}
	select {
	case <-ack:
	case <-c.done:
	}
}

func (c *Controller) loop() {
	defer c.wg.Done()
	events := c.engine.Events()
	for {
		select {
		case f := <-c.cmds:
			f()
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			c.handle(ev)
		case <-c.retry:
			c.retry = nil
			c.load()
		case <-c.connectLimit:
			c.connectLimit = nil
			if c.loading {
				_ = c.engine.Stop()
				c.lost("Timed out connecting")
			}
		case <-c.fallback:
			c.fallback = nil
			if c.loading {
				c.fallBack()
			}
		case <-c.done:
			return
		}
		c.publish()
	}
}

func (c *Controller) publish() {
	c.mu.Lock()
	changed := c.snapshot != c.state
	c.snapshot = c.state
	c.mu.Unlock()
	if changed && c.hooks.State != nil {
		c.hooks.State(c.state)
	}
}

func (c *Controller) play(t Target) {
	// Before the state is replaced, so the speed-up is actually undone.
	c.endCatchUp()
	c.target = &t
	c.state = State{
		Status:   Connecting,
		StreamID: t.StreamID,
		Label:    t.Label,
		URL:      t.URL,
		BufferMs: -1,
	}
	c.transport = max(min(c.learned, len(t.RTSPTransports)-1), 0)
	c.load()
}

// load (re)starts the current target.
func (c *Controller) load() {
	if c.target == nil {
		return
	}
	c.retry = nil
	c.endCatchUp()
	c.state.BufferMs = -1
	c.state.SinceMs = 0

	transports := c.target.RTSPTransports
	c.state.Via = ""
	if len(transports) > 0 {
		c.state.Via = transports[c.transport]
	}

	entry, err := c.engine.Load(c.target.URL, c.state.Via)
	if err != nil {
		c.loading = false
		c.lost(err.Error())
		return
	}
	c.entry = entry
	c.loading = true
	c.connectLimit = time.After(c.tuning.ConnectTimeout)
	c.fallback = nil
	if c.canFallBack() {
		c.fallback = time.After(c.tuning.FallbackAfter)
	}
}

func (c *Controller) canFallBack() bool {
	return c.target != nil && c.transport < len(c.target.RTSPTransports)-1
}

// fallBack restarts the current attempt on the next RTSP transport. It is
// not a reconnect: the attempt count and status stay as they are.
func (c *Controller) fallBack() {
	from := c.target.RTSPTransports[c.transport]
	c.transport++
	c.load()
	if c.state.Status == Connecting && c.loading {
		c.state.Message = fmt.Sprintf("No audio over %s, trying %s",
			strings.ToUpper(from), strings.ToUpper(c.state.Via))
	}
}

// transportRefused reports whether the server turned down the RTSP
// transport itself, as opposed to the stream being offline.
func transportRefused(detail string) bool {
	low := strings.ToLower(detail)
	return strings.Contains(low, "461") || strings.Contains(low, "unsupported transport")
}

func (c *Controller) stop() {
	c.target = nil
	c.loading = false
	c.retry = nil
	c.connectLimit = nil
	c.fallback = nil
	c.endCatchUp()
	_ = c.engine.Stop()
	c.state = State{Status: Idle, BufferMs: -1}
}

func (c *Controller) resync() {
	switch {
	case c.target == nil:
		return
	case c.state.Status == Playing:
		c.endCatchUp()
		_ = c.engine.DropBuffers()
	case !c.loading:
		// Waiting out a backoff: try again right now.
		c.load()
	}
}

func (c *Controller) setOptions(o Options) {
	c.opts = o
	if !o.AutoCatchUp {
		c.endCatchUp()
	}
	if !o.AutoReconnect && c.state.Status == Reconnecting && !c.loading {
		c.fail(c.state.Message)
	}
}

func (c *Controller) handle(ev Event) {
	if ev.Kind == EvDevices {
		if c.hooks.Devices != nil {
			c.hooks.Devices()
		}
		return
	}
	if c.target == nil || ev.Entry != c.entry {
		// Events for a stream we've since replaced or stopped.
		return
	}
	switch ev.Kind {
	case EvStarted:
		c.loading = false
		c.connectLimit = nil
		c.fallback = nil
		c.learned = max(c.learned, c.transport)
		if c.state.Status != Playing {
			c.state.SinceMs = time.Now().UnixMilli()
		}
		c.state.Status = Playing
		c.state.Message = ""
		c.state.Attempt = 0
		c.lastProgress = time.Now()
		c.lastPos = math.Inf(-1)
		c.fresh = true

	case EvEnded:
		if ev.Reason == EndStopped {
			return
		}
		if c.state.Status == Reconnecting && !c.loading {
			// Already handled (e.g. by the stall or connect watchdog).
			return
		}
		if c.loading && c.canFallBack() && transportRefused(ev.Detail) {
			c.fallBack()
			return
		}
		detail := ev.Detail
		if detail == "" && ev.Reason == EndEOF {
			detail = "Stream ended"
		}
		c.lost(detail)

	case EvTick:
		c.tick(ev)
	}
}

func (c *Controller) tick(ev Event) {
	if c.state.Status != Playing {
		return
	}
	now := time.Now()
	c.state.Info = ev.Info
	c.state.BufferMs = int(math.Round(ev.BufferSec * 1000))

	// A stream can stall without the connection closing; the position
	// standing still is the tell.
	if ev.HavePos {
		if ev.PosSec != c.lastPos {
			c.lastPos = ev.PosSec
			c.lastProgress = now
		} else if now.Sub(c.lastProgress) > c.tuning.StallTimeout {
			_ = c.engine.Stop()
			c.lost("Stream stalled")
			return
		}
	}

	fresh := c.fresh
	c.fresh = false
	c.catchUp(now, fresh)
}

// catchUp drains latency that accumulates when the network delivers a burst
// after a pause. Moderate excess is played off slightly fast; a large excess
// is simply dropped.
//
// fresh marks the first reading of a stream. Whatever is queued by then
// arrived while connecting (servers often open with a burst of recent
// audio); almost none of it has been heard, so it is dropped outright rather
// than played off over several seconds.
func (c *Controller) catchUp(now time.Time, fresh bool) {
	if !c.opts.AutoCatchUp {
		return
	}
	above := c.opts.CatchUpAboveMs
	buf := c.state.BufferMs

	if buf > max(4*above, 2000) || (fresh && buf > above) {
		c.endCatchUp()
		_ = c.engine.DropBuffers()
		return
	}

	if c.state.CatchingUp {
		if buf <= above/3 {
			c.endCatchUp()
		}
		return
	}

	if buf <= above {
		c.overSince = time.Time{}
		return
	}
	if c.overSince.IsZero() {
		c.overSince = now
	}
	if now.Sub(c.overSince) >= c.tuning.CatchUpSustain {
		if c.engine.SetSpeed(c.tuning.CatchUpSpeed) == nil {
			c.state.CatchingUp = true
		}
	}
}

func (c *Controller) endCatchUp() {
	c.overSince = time.Time{}
	if c.state.CatchingUp {
		_ = c.engine.SetSpeed(1)
		c.state.CatchingUp = false
	}
}

// lost handles the stream going away for any reason other than the user
// stopping it.
func (c *Controller) lost(detail string) {
	c.loading = false
	c.connectLimit = nil
	c.fallback = nil
	c.endCatchUp()
	c.state.BufferMs = -1
	c.state.SinceMs = 0
	c.state.Info = StreamInfo{}

	msg := explain(detail)
	if !c.opts.AutoReconnect {
		c.fail(msg)
		return
	}
	c.state.Status = Reconnecting
	c.state.Message = msg
	c.state.Attempt++
	c.retry = time.After(c.tuning.Backoff(c.state.Attempt))
}

func (c *Controller) fail(msg string) {
	c.target = nil
	c.retry = nil
	c.state.Status = Failed
	c.state.Message = msg
	c.state.Attempt = 0
}

// explain turns engine failure text into something a VJ can act on.
func explain(detail string) string {
	low := strings.ToLower(detail)
	switch {
	case detail == "":
		return "Connection lost"
	case strings.Contains(low, "401"), strings.Contains(low, "404"),
		strings.Contains(low, "not found"), strings.Contains(low, "unauthorized"):
		return "Stream is offline or doesn't exist"
	case strings.Contains(low, "connection refused"), strings.Contains(low, "network is unreachable"),
		strings.Contains(low, "failed to resolve"), strings.Contains(low, "name or service not known"):
		return "Can't reach the server"
	case low == "loading failed":
		return "Stream is offline or unreachable"
	}
	return detail
}
