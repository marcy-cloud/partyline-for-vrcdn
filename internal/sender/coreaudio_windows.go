//go:build windows

package sender

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bluenviron/gortmplib"
	"github.com/bluenviron/gortmplib/pkg/amf0"
	rtmpmsg "github.com/bluenviron/gortmplib/pkg/message"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"

	"partyline/internal/coreaudio"
	"partyline/internal/meter"
)

// coreAudioInstalled reports whether Apple's encoder is on this PC.
func coreAudioInstalled() bool { return coreaudio.Find() != "" }

const (
	caRate     = 48000
	caChannels = 2
	// One AAC frame of 16-bit stereo PCM, and how long it lasts.
	caFrameBytes = coreaudio.FrameSize * caChannels * 2
	caFrameTime  = time.Duration(coreaudio.FrameSize) * time.Second / caRate
	rtmpStreamID = 0x1000000
)

// coreAudioSession sends with Apple's encoder. libmpv cannot send audio it
// did not encode, so here it only captures (see capture), and this code
// encodes each frame with Apple's encoder and publishes it over RTMP
// itself.
type coreAudioSession struct {
	events chan sessionEvent
	quit   chan struct{}
	wg     sync.WaitGroup
	pumped chan struct{} // closed when pump has finished

	cap  *capture
	rtmp *gortmplib.Client
	enc  *coreaudio.Encoder
	key  string

	// writing is true while the pump waits for the server to take a frame.
	writing atomic.Bool

	mu      sync.Mutex
	started bool // the goroutines are running
	sent    int
	peak    meter.Levels
	fresh   bool // a peak has been measured since levels was last read
}

func openCoreAudio(cfg Config) (session, error) {
	enc, err := coreaudio.NewEncoder(caRate, caChannels, cfg.BitrateKbps)
	if err != nil {
		return nil, err
	}
	s := &coreAudioSession{enc: enc, key: cfg.Key, events: make(chan sessionEvent, 8), quit: make(chan struct{}), pumped: make(chan struct{})}
	fail := func(err error) (session, error) {
		s.close()
		return nil, err
	}

	// The server first: a wrong key or address should fail before any
	// audio device is opened.
	if s.rtmp, err = publish(cfg); err != nil {
		return fail(err)
	}
	if s.cap, err = openCapture(cfg.Device); err != nil {
		return fail(err)
	}

	s.started = true
	s.wg.Add(2)
	go s.watchCapture()
	go s.watchServer()
	go s.pump()
	return s, nil
}

// publish connects to the ingest and announces an audio-only AAC stream.
func publish(cfg Config) (*gortmplib.Client, error) {
	u, err := url.Parse(strings.TrimRight(cfg.Ingest, "/"))
	if err != nil {
		return nil, errors.New("the server address is not valid")
	}
	// This library takes the stream name from the fragment. As a path it
	// would become part of the app name, and the stream would be published
	// with no name, which VRCDN drops after 30 seconds.
	u.Fragment = cfg.Key

	c := &gortmplib.Client{URL: u, Publish: true}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}

	// Written by hand, not through the library's Writer, whose metadata
	// announces a video codec and data rates of 0. This is how ffmpeg words
	// it for an audio-only stream.
	err = c.Write(&rtmpmsg.DataAMF0{ChunkStreamID: 4, MessageStreamID: rtmpStreamID, Payload: []any{
		"@setDataFrame", "onMetaData", amf0.Object{
			{Key: "duration", Value: float64(0)},
			{Key: "audiodatarate", Value: float64(cfg.BitrateKbps)},
			{Key: "audiosamplerate", Value: float64(caRate)},
			{Key: "audiosamplesize", Value: float64(16)},
			{Key: "stereo", Value: true},
			{Key: "audiocodecid", Value: float64(10)},
			{Key: "encoder", Value: "PartyLine (CoreAudio AAC)"},
		},
	}})
	if err == nil {
		err = c.Write(aacMessage(rtmpmsg.Audio{
			AACType:   rtmpmsg.AudioAACTypeConfig,
			AACConfig: &mpeg4audio.AudioSpecificConfig{Type: mpeg4audio.ObjectTypeAACLC, SampleRate: caRate, ChannelCount: caChannels},
		}))
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// unpublish tells the server the stream is over, as ffmpeg does. Just
// hanging up leaves VRCDN holding the key as "locked", refusing the next
// publish, for some time afterwards.
func unpublish(c *gortmplib.Client, key string) {
	_ = c.NetConn().SetWriteDeadline(time.Now().Add(2 * time.Second))
	_ = c.Write(&rtmpmsg.CommandAMF0{ChunkStreamID: 3, Name: "FCUnpublish", CommandID: 6, Arguments: []any{nil, key}})
	_ = c.Write(&rtmpmsg.CommandAMF0{ChunkStreamID: 3, Name: "deleteStream", CommandID: 7, Arguments: []any{nil, float64(1)}})
	// Give the server a moment to take it in before the socket closes.
	time.Sleep(300 * time.Millisecond)
}

// aacMessage fills in the fields every AAC message shares. (FLV always
// labels AAC as 44.1 kHz stereo; the real format is in the configuration.)
func aacMessage(m rtmpmsg.Audio) *rtmpmsg.Audio {
	m.ChunkStreamID, m.MessageStreamID = rtmpmsg.AudioChunkStreamID, rtmpStreamID
	m.Codec, m.Rate, m.Depth, m.IsStereo = rtmpmsg.CodecMPEG4Audio, rtmpmsg.AudioRate44100, rtmpmsg.AudioDepth16, true
	return &m
}

func (s *coreAudioSession) end(detail string) {
	select {
	case s.events <- sessionEvent{kind: sessionEnded, detail: detail}:
	default: // an end is already queued; one is enough
	}
}

func (s *coreAudioSession) stopping() bool {
	select {
	case <-s.quit:
		return true
	default:
		return false
	}
}

// watchCapture reports libmpv giving up on the input.
func (s *coreAudioSession) watchCapture() {
	defer s.wg.Done()
	select {
	case detail := <-s.cap.ended:
		s.end(detail)
	case <-s.quit:
	}
}

// watchServer reads what the server sends, which the protocol requires
// (the library answers its pings here), and reports it hanging up.
func (s *coreAudioSession) watchServer() {
	defer s.wg.Done()
	for {
		if _, err := s.rtmp.Read(); err != nil {
			if !s.stopping() {
				s.end("") // the connection to the server was lost
			}
			return
		}
	}
}

// pump moves audio from libmpv's PCM to the server, a frame at a time.
func (s *coreAudioSession) pump() {
	defer close(s.pumped)
	conn, err := s.cap.audio()
	if err != nil {
		if !s.stopping() {
			s.end("the audio capture did not start")
		}
		return
	}

	frame := make([]byte, caFrameBytes)
	for {
		if _, err := io.ReadFull(conn, frame); err != nil {
			if !s.stopping() {
				s.end("the input stopped delivering audio")
			}
			return
		}
		s.measure(frame)

		packet, err := s.enc.Encode(frame)
		if err != nil {
			s.end(err.Error())
			return
		}
		if packet == nil {
			continue
		}
		s.mu.Lock()
		n := s.sent
		s.sent++
		s.mu.Unlock()
		s.writing.Store(true)
		err = s.rtmp.Write(aacMessage(rtmpmsg.Audio{AACType: rtmpmsg.AudioAACTypeAU, AU: packet, DTS: time.Duration(n) * caFrameTime}))
		s.writing.Store(false)
		if err != nil {
			if !s.stopping() {
				s.end("")
			}
			return
		}
		if n == 0 {
			select {
			case s.events <- sessionEvent{kind: sessionStarted}:
			case <-s.quit:
				return
			}
		}
	}
}

// measure keeps the loudest sample per channel since levels was last read.
func (s *coreAudioSession) measure(frame []byte) {
	var peak [caChannels]int
	for i := 0; i+1 < len(frame); i += 2 {
		v := int(int16(binary.LittleEndian.Uint16(frame[i:])))
		if v < 0 {
			v = -v
		}
		if c := (i / 2) % caChannels; v > peak[c] {
			peak[c] = v
		}
	}
	db := func(v int) float64 {
		if v == 0 {
			return meter.Silence
		}
		return math.Max(20*math.Log10(float64(v)/32768), meter.Silence)
	}
	l := meter.Levels{Left: db(peak[0]), Right: db(peak[1])}
	s.mu.Lock()
	if !s.fresh {
		s.peak, s.fresh = l, true
	} else {
		s.peak.Left, s.peak.Right = math.Max(s.peak.Left, l.Left), math.Max(s.peak.Right, l.Right)
	}
	s.mu.Unlock()
}

func (s *coreAudioSession) wait(timeout time.Duration) sessionEvent {
	select {
	case ev := <-s.events:
		return ev
	case <-time.After(timeout):
		return sessionEvent{}
	}
}

// stalledOn says what the pump is waiting for: the server to take a frame,
// or the input to deliver one.
func (s *coreAudioSession) stalledOn() stallCause {
	if s.writing.Load() {
		return stallNetwork
	}
	return stallInput
}

func (s *coreAudioSession) position() (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return (time.Duration(s.sent) * caFrameTime).Seconds(), true
}

func (s *coreAudioSession) levels() (meter.Levels, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.fresh {
		return meter.Levels{}, false
	}
	s.fresh = false
	return s.peak, true
}

func (s *coreAudioSession) close() {
	close(s.quit)
	// The audio first: ending the capture stops the pump, which must be out
	// of the way before anything else is written to the server on the same
	// connection.
	if s.rtmp != nil {
		// Unblocks a pump that is in the middle of a write to a dead server.
		_ = s.rtmp.NetConn().SetWriteDeadline(time.Now())
	}
	if s.cap != nil {
		s.cap.close()
	}
	if s.started {
		<-s.pumped
	}
	if s.rtmp != nil {
		unpublish(s.rtmp, s.key)
		s.rtmp.Close()
	}
	s.wg.Wait()
	s.enc.Close()
}
