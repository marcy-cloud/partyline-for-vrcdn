package sender

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"partyline/internal/meter"
)

// fakeWorker runs RunWorker's protocol by hand over in-memory pipes.
type fakeWorker struct {
	inR  *io.PipeReader
	inW  *io.PipeWriter
	outR *io.PipeReader
	outW *io.PipeWriter
}

func newFakeWorker() *fakeWorker {
	f := &fakeWorker{}
	f.inR, f.inW = io.Pipe()
	f.outR, f.outW = io.Pipe()
	return f
}
func (f *fakeWorker) stdin() io.WriteCloser { return f.inW }
func (f *fakeWorker) stdout() io.Reader     { return f.outR }
func (f *fakeWorker) finish()               { f.outW.Close() }
func (f *fakeWorker) say(m message)         { json.NewEncoder(f.outW).Encode(m) }

func TestProxyRelaysTheWorker(t *testing.T) {
	states := make(chan State, 16)
	levels := make(chan meter.Levels, 16)
	p := NewProxy(Hooks{State: func(s State) { states <- s }, Levels: func(l meter.Levels) { levels <- l }})
	w := newFakeWorker()
	p.spawn = func() (worker, error) { return w, nil }

	got := make(chan string, 1)
	go func() {
		line, _ := io.ReadAll(io.LimitReader(w.inR, 400))
		got <- string(line)
	}()
	if err := p.Start(good); err != nil {
		t.Fatal(err)
	}
	if st := <-states; st.Status != Connecting {
		t.Fatalf("first state %+v", st)
	}

	w.say(message{State: &State{Status: Live, SinceMs: 5}})
	if st := <-states; st.Status != Live || st.SinceMs != 5 {
		t.Errorf("relayed state %+v", st)
	}
	w.say(message{Levels: &meter.Levels{Left: -6, Right: -7}})
	if l := <-levels; l.Left != -6 || l.Right != -7 {
		t.Errorf("relayed levels %+v", l)
	}

	// Stopping closes the worker's stdin, which is how it is told to stop.
	p.Stop()
	if p.State().Status != Off {
		t.Errorf("after stop: %+v", p.State())
	}
	select {
	case line := <-got:
		var wc workerConfig
		if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &wc); err != nil || Config(wc) != good {
			t.Errorf("worker was sent %q (%v)", line, err)
		}
	case <-time.After(2 * time.Second):
		t.Error("the worker's stdin was never closed")
	}
}

func TestProxyReportsAWorkerThatDies(t *testing.T) {
	states := make(chan State, 16)
	p := NewProxy(Hooks{State: func(s State) { states <- s }})
	w := newFakeWorker()
	go io.Copy(io.Discard, w.inR)
	p.spawn = func() (worker, error) { return w, nil }
	p.Start(good)
	<-states // connecting

	w.outW.Close() // the process is gone
	select {
	case st := <-states:
		if st.Status != Failed || !strings.Contains(st.Message, "unexpectedly") {
			t.Errorf("state %+v", st)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a dead worker went unnoticed")
	}
}

// The worker end of the pipe, with a config that is refused before any
// connection is made.
func TestRunWorkerRefusesABadConfig(t *testing.T) {
	bad := good
	bad.Key = ""
	line, _ := json.Marshal(workerConfig(bad))
	var out bytes.Buffer
	RunWorker(bytes.NewReader(append(line, '\n')), &out)
	var m message
	if err := json.Unmarshal(out.Bytes(), &m); err != nil || m.State == nil || m.State.Status != Failed {
		t.Fatalf("worker said %q", out.String())
	}
	if strings.Contains(out.String(), good.Key) {
		t.Error("the key leaked into the worker's output")
	}
}
