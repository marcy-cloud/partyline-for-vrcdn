package sender

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"partyline/internal/libmpv"
	"partyline/internal/pcmpipe"
)

// capture is a libmpv core used only to capture: it reads an input and
// hands the audio over as raw PCM, 48 kHz stereo 16-bit little-endian, a
// piece at a time as it comes (about 21 ms each). It is for encoders libmpv
// doesn't have.
type capture struct {
	m    *libmpv.Client
	pipe *pcmpipe.Pipe
	quit chan struct{}
	done chan struct{} // closed when watch has finished
	// ended gets the reason when libmpv gives up on the input.
	ended chan string
	once  sync.Once
}

func openCapture(device string) (*capture, error) {
	input, inputOptions, err := inputURL(device)
	if err != nil {
		return nil, err
	}
	pipe, err := pcmpipe.Listen()
	if err != nil {
		return nil, err
	}
	m, err := libmpv.New()
	if err != nil {
		pipe.Close()
		return nil, err
	}
	options := [][2]string{
		{"config", "no"}, {"terminal", "no"}, {"ytdl", "no"}, {"load-scripts", "no"},
		{"vid", "no"}, {"sid", "no"}, {"audio-display", "no"}, {"vo", "null"},
		// Nothing may sit between the input and whoever reads the pipe.
		{"cache", "no"},
		{"audio-buffer", "0"},
		// "Play" into the pipe. Not libmpv's encoding mode, which would hand
		// PCM over a third of a second at a time (see package pcmpipe).
		{"ao", "pcm"},
		{"ao-pcm-file", pipe.Path()},
		{"ao-pcm-waveheader", "no"},
		{"audio-format", "s16"},
		{"audio-samplerate", "48000"},
		{"audio-channels", "stereo"},
	}
	for _, kv := range append(options, inputOptions...) {
		if err := m.SetOption(kv[0], kv[1]); err != nil {
			m.Destroy()
			pipe.Close()
			return nil, fmt.Errorf("option %s: %w", kv[0], err)
		}
	}
	if err := m.Initialize(); err != nil {
		m.Destroy()
		pipe.Close()
		return nil, err
	}
	_ = m.RequestLogMessages("error")
	if err := m.Command("loadfile", input); err != nil {
		m.Destroy()
		pipe.Close()
		return nil, err
	}
	c := &capture{m: m, pipe: pipe, quit: make(chan struct{}), done: make(chan struct{}), ended: make(chan string, 1)}
	go c.watch()
	return c, nil
}

// watch reports libmpv giving up on the input.
func (c *capture) watch() {
	defer close(c.done)
	var errs []string
	for {
		select {
		case <-c.quit:
			return
		default:
		}
		ev := c.m.WaitEvent(0.2)
		switch ev.ID {
		case libmpv.EventLogMessage:
			if errs = append(errs, strings.TrimSpace(ev.LogText)); len(errs) > 6 {
				errs = errs[len(errs)-6:]
			}
		case libmpv.EventEndFile, libmpv.EventShutdown:
			detail := strings.Join(errs, " · ")
			if detail == "" {
				detail = "the input stopped delivering audio"
			}
			c.ended <- detail
			return
		}
	}
}

// audio waits for the capture to start and returns its PCM. Reads end when
// the capture is closed.
func (c *capture) audio() (io.ReadCloser, error) { return c.pipe.Accept() }

// close stops capturing. Anything reading the audio sees it end.
func (c *capture) close() {
	c.once.Do(func() {
		close(c.quit)
		c.m.Wakeup()
		<-c.done
		// Destroying the core closes its end of the pipe, which is what
		// ends a read that is waiting; closing ours alone would not.
		c.m.Destroy()
		c.pipe.Close()
	})
}
