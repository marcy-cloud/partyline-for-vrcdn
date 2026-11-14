package player

import (
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"partyline/internal/libmpv"
)

// Tap opens a stream the way the player does but, instead of sending it to
// an audio device, hands over the decoded audio the moment it is decoded,
// as 48 kHz stereo 16-bit little-endian PCM. It is for measuring how late a
// stream is: what comes out has been through the same network and decoder
// settings as playback, and nothing after them (no queue, no audio device).
type Tap struct {
	m    *libmpv.Client
	pipe *pipe
	quit chan struct{}
	done chan struct{}
	once sync.Once

	chunks chan []byte
	rest   []byte

	mu      sync.Mutex
	errs    []string
	endedAs string
	ended   chan struct{}
}

// OpenTap starts reading url. rtspTransport is "udp", "tcp" or "" as for
// Engine.Load. extra mpv options are applied last.
func OpenTap(url, rtspTransport string, extra ...[2]string) (*Tap, error) {
	p, err := listenPipe()
	if err != nil {
		return nil, err
	}
	m, err := libmpv.New()
	if err != nil {
		p.Close()
		return nil, err
	}
	t := &Tap{m: m, pipe: p, quit: make(chan struct{}), done: make(chan struct{}), chunks: make(chan []byte, 1024), ended: make(chan struct{})}

	output := [][2]string{
		{"config", "no"}, {"terminal", "no"}, {"ytdl", "no"}, {"load-scripts", "no"},
		// An audio "device" that writes what it is given straight to our
		// pipe, as fast as the stream delivers it.
		{"ao", "pcm"},
		{"ao-pcm-file", p.Path()},
		{"ao-pcm-waveheader", "no"},
		{"audio-format", "s16"},
		{"audio-samplerate", "48000"},
		{"audio-channels", "stereo"},
	}
	var transport [][2]string
	if rtspTransport != "" {
		transport = [][2]string{{"rtsp-transport", rtspTransport}}
	}
	for _, group := range [][][2]string{lowLatencyOptions, output, transport, extra} {
		for _, kv := range group {
			_ = m.SetOption(kv[0], kv[1])
		}
	}
	if err := m.Initialize(); err != nil {
		m.Destroy()
		p.Close()
		return nil, err
	}
	_ = m.Command("apply-profile", "low-latency")
	_ = m.RequestLogMessages("error")
	if err := m.Command("loadfile", url); err != nil {
		m.Destroy()
		p.Close()
		return nil, err
	}

	go t.pump()
	go t.watch()
	return t, nil
}

// pump moves audio from the pipe to Read, in the pieces it arrives in.
func (t *Tap) pump() {
	r, err := t.pipe.Accept()
	if err != nil {
		return
	}
	defer r.Close()
	buf := make([]byte, 64<<10)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			select {
			case t.chunks <- append([]byte(nil), buf[:n]...):
			case <-t.quit:
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				return
			}
			return
		}
	}
}

func (t *Tap) watch() {
	defer close(t.done)
	for {
		select {
		case <-t.quit:
			return
		default:
		}
		ev := t.m.WaitEvent(0.1)
		switch ev.ID {
		case libmpv.EventLogMessage:
			t.mu.Lock()
			if t.errs = append(t.errs, strings.TrimSpace(ev.LogText)); len(t.errs) > 6 {
				t.errs = t.errs[len(t.errs)-6:]
			}
			t.mu.Unlock()
		case libmpv.EventEndFile, libmpv.EventShutdown:
			t.mu.Lock()
			if t.endedAs = strings.Join(t.errs, " · "); t.endedAs == "" {
				t.endedAs = ev.EndErr
			}
			if t.endedAs == "" {
				t.endedAs = "the stream ended"
			}
			t.mu.Unlock()
			close(t.ended)
			return
		}
	}
}

// Read returns decoded audio: one piece as it arrived, or the rest of one
// that didn't fit. It fails if none arrives within the timeout, with the
// engine's reason if the stream ended.
func (t *Tap) Read(p []byte, timeout time.Duration) (int, error) {
	if len(t.rest) == 0 {
		select {
		case t.rest = <-t.chunks:
		case <-t.ended:
			t.mu.Lock()
			defer t.mu.Unlock()
			return 0, errors.New(t.endedAs)
		case <-time.After(timeout):
			return 0, errors.New("no audio arrived")
		}
	}
	n := copy(p, t.rest)
	t.rest = t.rest[n:]
	return n, nil
}

// Close stops reading.
func (t *Tap) Close() {
	t.once.Do(func() {
		close(t.quit)
		<-t.done
		t.pipe.Close() // also lets go of a pump still waiting for mpv
		t.m.Destroy()
	})
}
