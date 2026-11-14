//go:build !windows

package loopback

import (
	"errors"
	"time"
)

const (
	Rate       = 48000
	Channels   = 2
	FrameBytes = Channels * 2
)

// Capture is the sound an application is playing.
type Capture struct{}

// Open is only available on Windows for now.
func Open(pid uint32) (*Capture, error) {
	return nil, errors.New("capturing an application's sound is only available on Windows for now")
}

func (c *Capture) Read(p []byte, timeout time.Duration) (int, error) {
	return 0, errors.New("not open")
}

func (c *Capture) Close() {}
