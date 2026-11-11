// Package player plays one live audio stream at a time with as little
// buffering as possible, and keeps it that way: it reconnects when a stream
// drops and drains latency that builds up after network hiccups.
//
// The Controller holds the policy and is independent of any media library;
// the Engine interface is the seam where libmpv plugs in.
package player

// AudioDevice is an output device the engine can play to.
type AudioDevice struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// EventKind identifies an engine event.
type EventKind int

const (
	// EvStarted: audio from the entry has begun playing.
	EvStarted EventKind = iota
	// EvEnded: the entry stopped. Reason says why.
	EvEnded
	// EvTick: a periodic measurement while an entry is loaded.
	EvTick
	// EvDevices: the set of audio devices changed.
	EvDevices
	// EvLevels: a reading of the incoming signal level, several times a
	// second while an entry is playing.
	EvLevels
)

// Silence is the level reported when there is no signal, in dBFS.
const Silence = -100.0

// Levels is the peak level of the incoming audio per channel, in dBFS
// (0 is full scale, Silence is nothing). It is measured before the volume
// control, so it shows what the stream sends, not how loud it is played.
// A mono stream reports the same value for both.
type Levels struct {
	Left  float64 `json:"left"`
	Right float64 `json:"right"`
}

// EndReason says why playback of an entry ended.
type EndReason int

const (
	// EndStopped: we asked for it (stop, or replaced by another load).
	EndStopped EndReason = iota
	// EndEOF: the server closed the stream.
	EndEOF
	// EndError: connecting or reading failed.
	EndError
)

// Event is something the engine reports about playback.
type Event struct {
	Kind EventKind
	// Entry is the value Load returned for the stream this event is about.
	Entry int64

	// EvEnded
	Reason EndReason
	// Detail is the engine's own description of a failure, if any.
	Detail string

	// EvTick
	// BufferSec is how much audio is queued but not yet played: the part of
	// the latency this player is responsible for.
	BufferSec float64
	// PosSec is the playback position; it stops advancing when the stream
	// stalls. HavePos is false when the engine can't report it.
	PosSec  float64
	HavePos bool
	Info    StreamInfo

	// EvLevels
	Levels Levels
}

// StreamInfo describes the audio being played.
type StreamInfo struct {
	Codec      string `json:"codec"`
	SampleRate int    `json:"sampleRate"`
	Channels   int    `json:"channels"`
}

// Engine is the media backend. Implementations must be safe for concurrent
// use.
type Engine interface {
	// Load starts playing url, replacing whatever was playing, and returns
	// an identifier that later events for this stream carry. rtspTransport
	// is "tcp", "udp" or "" for non-RTSP URLs.
	Load(url, rtspTransport string) (entry int64, err error)
	// Stop ends playback.
	Stop() error
	// SetSpeed sets the playback rate; 1 is normal.
	SetSpeed(speed float64) error
	// DropBuffers discards queued audio so playback jumps to the newest
	// data received.
	DropBuffers() error
	// SetVolume takes 0-100.
	SetVolume(volume float64) error
	// SetAudioDevice selects an output by AudioDevice.Name.
	SetAudioDevice(name string) error
	AudioDevices() ([]AudioDevice, error)
	// Events delivers playback events in order. It is closed by Close.
	Events() <-chan Event
	// Describe returns a short version string for display.
	Describe() string
	Close()
}
