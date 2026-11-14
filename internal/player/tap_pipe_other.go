//go:build !windows

package player

import (
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// pipe is a FIFO that mpv writes decoded audio into as if it were a file.
type pipe struct {
	dir, name string
}

func listenPipe() (*pipe, error) {
	dir, err := os.MkdirTemp("", "partyline-tap-")
	if err != nil {
		return nil, err
	}
	name := filepath.Join(dir, "audio.pcm")
	if err := syscall.Mkfifo(name, 0o600); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return &pipe{dir: dir, name: name}, nil
}

func (p *pipe) Path() string { return p.name }

// Accept opens the reading end. Opened for reading and writing it doesn't
// wait for the writer, and never sees end-of-file when the writer leaves.
func (p *pipe) Accept() (io.ReadCloser, error) { return os.OpenFile(p.name, os.O_RDWR, 0) }

func (p *pipe) Close() { os.RemoveAll(p.dir) }
