package sender

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"

	"partyline/internal/meter"
)

// The sender runs in a process of its own, started by the app as itself with
// one of the arguments below. Two reasons. libmpv hands every core's ffmpeg
// log lines to the first core created in a process, which in the app is the
// player's, so a sender sharing the process cannot see its own errors or the
// device listing. And a broadcast should not be at the mercy of the player,
// or the other way round.
const (
	// WorkerArg runs a broadcast: the Config arrives as one JSON line on
	// stdin, messages go out as JSON lines on stdout, and closing stdin
	// stops it. The stream key therefore never appears on a command line.
	WorkerArg = "--send-worker"
	// ListArg prints the input devices as JSON and exits.
	ListArg = "--list-inputs"
)

// message is one line from the worker.
type message struct {
	State  *State        `json:"state,omitempty"`
	Levels *meter.Levels `json:"levels,omitempty"`
}

// workerConfig is Config in the form it crosses the pipe.
type workerConfig struct {
	Ingest      string `json:"ingest"`
	Key         string `json:"key"`
	Device      string `json:"device"`
	BitrateKbps int    `json:"bitrateKbps"`
}

// RunWorker is the body of the worker process: it broadcasts what stdin
// asks for until stdin closes.
func RunWorker(stdin io.Reader, stdout io.Writer) {
	var mu sync.Mutex
	out := json.NewEncoder(stdout)
	send := func(m message) {
		mu.Lock()
		defer mu.Unlock()
		_ = out.Encode(m)
	}

	in := bufio.NewReader(stdin)
	line, err := in.ReadBytes('\n')
	var wc workerConfig
	if err != nil || json.Unmarshal(line, &wc) != nil {
		send(message{State: &State{Status: Failed, Message: "the sender was started without its settings"}})
		return
	}

	s := New(DefaultTuning(), Hooks{
		State:  func(st State) { send(message{State: &st}) },
		Levels: func(l meter.Levels) { send(message{Levels: &l}) },
	})
	if err := s.Start(Config(wc)); err != nil {
		send(message{State: &State{Status: Failed, Message: err.Error()}})
		return
	}
	// Anything further on stdin is ignored; its end is the signal to stop.
	_, _ = io.Copy(io.Discard, in)
	s.Stop()
}

// RunList is the body of the listing process.
func RunList(stdout io.Writer) {
	devices, _ := Devices() // on error this is still the test tone
	_ = json.NewEncoder(stdout).Encode(devices)
}
