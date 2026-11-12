package sender

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Proxy is the app's handle on the sender. It has the same shape as Sender,
// but the broadcast runs in a worker process (see worker.go).
type Proxy struct {
	hooks Hooks
	// spawn starts a worker; replaced in tests.
	spawn func() (worker, error)

	mu    sync.Mutex
	state State
	w     worker
	done  chan struct{} // closed when the current worker's output has ended
}

// worker is a running worker process.
type worker interface {
	// stdin takes the config line; closing it asks the worker to stop.
	stdin() io.WriteCloser
	stdout() io.Reader
	// finish waits for the process to exit, killing it if it lingers.
	finish()
}

// NewProxy returns an idle sender whose broadcasts run in worker processes.
func NewProxy(hooks Hooks) *Proxy {
	return &Proxy{hooks: hooks, spawn: spawnWorker, state: State{Status: Off}}
}

// State returns the current snapshot.
func (p *Proxy) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

func (p *Proxy) set(next State) {
	p.mu.Lock()
	changed := p.state != next
	p.state = next
	p.mu.Unlock()
	if changed && p.hooks.State != nil {
		p.hooks.State(next)
	}
}

// Start begins broadcasting; progress arrives through the State hook.
func (p *Proxy) Start(cfg Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	p.Stop()

	w, err := p.spawn()
	if err != nil {
		return err
	}
	line, _ := json.Marshal(workerConfig(cfg))
	if _, err := w.stdin().Write(append(line, '\n')); err != nil {
		w.finish()
		return errors.New("the sender could not be started")
	}

	done := make(chan struct{})
	p.mu.Lock()
	p.w, p.done = w, done
	p.mu.Unlock()
	p.set(State{Status: Connecting})

	go func() {
		defer close(done)
		p.pump(w)
	}()
	return nil
}

// pump relays a worker's messages until its output ends.
func (p *Proxy) pump(w worker) {
	lines := bufio.NewScanner(w.stdout())
	for lines.Scan() {
		var m message
		if json.Unmarshal(lines.Bytes(), &m) != nil {
			continue
		}
		p.mu.Lock()
		current := p.w == w
		p.mu.Unlock()
		if !current {
			continue // being stopped: nothing it says matters any more
		}
		switch {
		case m.State != nil:
			p.set(*m.State)
		case m.Levels != nil && p.hooks.Levels != nil:
			p.hooks.Levels(*m.Levels)
		}
	}
	// The output ended. If nobody asked for that, the worker died.
	p.mu.Lock()
	unexpected := p.w == w
	if unexpected {
		p.w, p.done = nil, nil
	}
	p.mu.Unlock()
	if unexpected {
		w.finish()
		if st := p.State(); st.Status != Failed {
			p.set(State{Status: Failed, Message: "The sender stopped unexpectedly"})
		}
	}
}

// Stop ends the broadcast, if there is one, and waits for it to finish.
func (p *Proxy) Stop() {
	p.mu.Lock()
	w, done := p.w, p.done
	p.w, p.done = nil, nil
	p.mu.Unlock()
	if w == nil {
		return
	}
	_ = w.stdin().Close()
	w.finish()
	<-done
	p.set(State{Status: Off})
}

// process is a worker that is a real child process.
type process struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out io.Reader
}

func (p *process) stdin() io.WriteCloser { return p.in }
func (p *process) stdout() io.Reader     { return p.out }

func (p *process) finish() {
	exited := make(chan struct{})
	go func() { _ = p.cmd.Wait(); close(exited) }()
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		_ = p.cmd.Process.Kill()
		<-exited
	}
}

// self returns a command that runs this program again with one argument,
// with no window of its own.
func self(arg string) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, arg)
	hideWindow(cmd)
	return cmd, nil
}

func spawnWorker() (worker, error) {
	cmd, err := self(WorkerArg)
	if err != nil {
		return nil, err
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &process{cmd: cmd, in: in, out: out}, nil
}

// ListDevices lists the inputs by asking a short-lived copy of this program
// (see worker.go for why it cannot be done in-process).
func ListDevices() []Device {
	fallback := []Device{TestTone}
	cmd, err := self(ListArg)
	if err != nil {
		return fallback
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Start(); err != nil {
		return fallback
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		return fallback
	}
	var devices []Device
	if json.Unmarshal(out.Bytes(), &devices) != nil || len(devices) == 0 {
		return fallback
	}
	return devices
}
