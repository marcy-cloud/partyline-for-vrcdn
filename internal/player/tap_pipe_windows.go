package player

import (
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"golang.org/x/sys/windows"
)

// pipe is a named pipe that mpv writes decoded audio into as if it were a
// file.
type pipe struct {
	name string
	h    windows.Handle
	mu   sync.Mutex
	file *os.File
	shut bool
}

var pipeSeq atomic.Int64

func listenPipe() (*pipe, error) {
	name := fmt.Sprintf(`\\.\pipe\partyline-tap-%d-%d`, os.Getpid(), pipeSeq.Add(1))
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateNamedPipe(ptr, windows.PIPE_ACCESS_INBOUND, windows.PIPE_TYPE_BYTE|windows.PIPE_WAIT, 1, 0, 1<<16, 0, nil)
	if err != nil {
		return nil, fmt.Errorf("creating a pipe for the audio: %w", err)
	}
	return &pipe{name: name, h: h}, nil
}

func (p *pipe) Path() string { return p.name }

// Accept waits for the writer to open the pipe.
func (p *pipe) Accept() (io.ReadCloser, error) {
	if err := windows.ConnectNamedPipe(p.h, nil); err != nil && err != windows.ERROR_PIPE_CONNECTED {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.shut {
		return nil, io.ErrClosedPipe
	}
	p.file = os.NewFile(uintptr(p.h), p.name)
	return p.file, nil
}

// Close ends the pipe, whether or not anyone has opened it.
func (p *pipe) Close() {
	p.mu.Lock()
	p.shut = true
	file := p.file
	p.mu.Unlock()
	if file != nil {
		file.Close()
		return
	}
	// Accept may be waiting for a writer that will never come: be one.
	if w, err := os.OpenFile(p.name, os.O_WRONLY, 0); err == nil {
		w.Close()
	}
	windows.CloseHandle(p.h)
}
