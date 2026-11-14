package sender

import (
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"partyline/internal/loopback"
)

// AppInput is the prefix of a device ID that names a program instead of an
// input device: "app:" followed by the program's file name, e.g.
// "app:rekordbox.exe". What is sent is the sound that program is playing.
const AppInput = "app:"

// appSource is the sound of a running program, as a pcmSource.
type appSource struct {
	cap   *loopback.Capture
	ended chan string
	quit  chan struct{}
	once  sync.Once
	ln    net.Listener
}

func openApp(id string) (*appSource, error) {
	program, err := loopback.Find(strings.TrimPrefix(id, AppInput))
	if err != nil {
		return nil, err
	}
	c, err := loopback.Open(program.PID)
	if err != nil {
		return nil, err
	}
	a := &appSource{cap: c, ended: make(chan string, 1), quit: make(chan struct{})}
	// Windows goes on handing over silence for a program that has gone, so
	// its going has to be watched for.
	go func() {
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-a.quit:
				return
			case <-tick.C:
				if !loopback.Running(program.PID) {
					a.ended <- program.Name + " was closed"
					return
				}
			}
		}
	}()
	return a, nil
}

func (a *appSource) audio() (io.ReadCloser, error) { return a, nil }
func (a *appSource) gone() <-chan string           { return a.ended }

// Read returns the program's sound as it is captured. If Windows delivers
// nothing for a while, silence stands in for it: an encoder left waiting
// would let the stream stall.
func (a *appSource) Read(p []byte) (int, error) {
	const wait = 100 * time.Millisecond
	select {
	case <-a.quit:
		return 0, io.EOF
	default:
	}
	n, err := a.cap.Read(p, wait)
	if err != nil {
		return 0, io.EOF
	}
	if n == 0 {
		n = min(len(p), int(wait*loopback.Rate/time.Second)*loopback.FrameBytes)
		clear(p[:n])
	}
	return n, nil
}

func (a *appSource) Close() error { return nil }

// serve offers the sound on a local TCP port, for libmpv to read as a
// PCMInput, and returns the address.
func (a *appSource) serve() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	a.ln = ln
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 8192)
		for {
			n, err := a.Read(buf)
			if err != nil {
				return
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return
			}
		}
	}()
	return ln.Addr().String(), nil
}

func (a *appSource) close() {
	a.once.Do(func() {
		close(a.quit)
		if a.ln != nil {
			a.ln.Close()
		}
		a.cap.Close()
	})
}

// programs lists the running programs that have sound open, as devices.
func programs() []Device {
	list, _ := loopback.Programs()
	out := make([]Device, 0, len(list))
	for _, p := range list {
		out = append(out, Device{ID: AppInput + p.Exe, Name: p.Name, Program: true})
	}
	return out
}
