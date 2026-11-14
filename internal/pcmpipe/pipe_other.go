//go:build !windows

package pcmpipe

import (
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// Pipe is a FIFO that libmpv writes audio into as if it were a file.
type Pipe struct {
	dir, name string
}

// Listen creates the pipe. Nothing is read until Accept.
func Listen() (*Pipe, error) {
	dir, err := os.MkdirTemp("", "partyline-pcm-")
	if err != nil {
		return nil, err
	}
	name := filepath.Join(dir, "audio.pcm")
	if err := syscall.Mkfifo(name, 0o600); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return &Pipe{dir: dir, name: name}, nil
}

// Path is what to give libmpv as the file to write.
func (p *Pipe) Path() string { return p.name }

// Accept opens the reading end. Opened for reading and writing it doesn't
// wait for the writer, and never sees end-of-file when the writer leaves.
func (p *Pipe) Accept() (io.ReadCloser, error) { return os.OpenFile(p.name, os.O_RDWR, 0) }

func (p *Pipe) Close() { os.RemoveAll(p.dir) }
