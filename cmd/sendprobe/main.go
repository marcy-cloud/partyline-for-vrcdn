// Command sendprobe drives the sender without the GUI: it lists the inputs
// that can be sent and, given a stream key, sends one to an RTMP ingest
// while printing state changes and levels.
//
//	go run ./cmd/sendprobe -list
//	go run ./cmd/sendprobe -key-file key.txt -for 30s             # test tone
//	go run ./cmd/sendprobe -key-file key.txt -device "Line In"    # an input, by part of its name
//
// The stream key is read from a file so it never appears on a command line
// or in shell history, and it is never printed.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"partyline/internal/meter"
	"partyline/internal/sender"
	"partyline/internal/vrcdn"
)

func main() {
	list := flag.Bool("list", false, "list the inputs and exit")
	device := flag.String("device", sender.TestTone.Name, "input to send: part of its name")
	ingest := flag.String("ingest", vrcdn.Ingests[0].URL, "RTMP ingest address")
	keyFile := flag.String("key-file", "", "file holding the stream key")
	bitrate := flag.Int("bitrate", sender.Bitrates[0], "AAC bitrate in kbps")
	duration := flag.Duration("for", 0, "stop after this long (default: until Ctrl+C)")
	flag.Parse()

	devices, err := sender.Devices()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not list input devices:", err)
	}
	if *list {
		for _, d := range devices {
			fmt.Println(d.Name)
		}
		return
	}

	var input *sender.Device
	for i, d := range devices {
		if strings.Contains(strings.ToLower(d.Name), strings.ToLower(*device)) {
			input = &devices[i]
			break
		}
	}
	if input == nil {
		fail(fmt.Sprintf("no input matches %q; see -list", *device))
	}
	if *keyFile == "" {
		fail("give -key-file")
	}
	raw, err := os.ReadFile(*keyFile)
	if err != nil {
		fail(err.Error())
	}

	began := time.Now()
	stamp := func() string { return fmt.Sprintf("%5.1fs", time.Since(began).Seconds()) }
	readings := 0
	s := sender.New(sender.DefaultTuning(), sender.Hooks{
		State: func(st sender.State) {
			line := fmt.Sprintf("%s %-12s", stamp(), st.Status)
			if st.Message != "" {
				line += fmt.Sprintf(" %s (attempt %d)", st.Message, st.Attempt)
			}
			fmt.Println(line)
		},
		Levels: func(l meter.Levels) {
			// About 25 readings a second; show one every two seconds.
			if readings++; readings%50 == 0 {
				fmt.Printf("%s level        L %6.1f dB  R %6.1f dB\n", stamp(), l.Left, l.Right)
			}
		},
	})

	fmt.Printf("sending %s\n     to %s/<key> (AAC %d kbps, 48 kHz stereo, no video)\n", input.Name, strings.TrimRight(*ingest, "/"), *bitrate)
	err = s.Start(sender.Config{Ingest: *ingest, Key: strings.TrimSpace(string(raw)), Device: input.ID, BitrateKbps: *bitrate})
	if err != nil {
		fail(err.Error())
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	var timeUp <-chan time.Time
	if *duration > 0 {
		timeUp = time.After(*duration)
	}
	gaveUp := time.NewTicker(200 * time.Millisecond)
	defer gaveUp.Stop()
wait:
	for {
		select {
		case <-interrupt:
			break wait
		case <-timeUp:
			break wait
		case <-gaveUp.C:
			if s.State().Status == sender.Failed {
				os.Exit(1)
			}
		}
	}
	s.Stop()
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "error:", msg)
	os.Exit(1)
}
