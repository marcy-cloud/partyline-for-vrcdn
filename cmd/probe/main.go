// Command probe exercises the player without the GUI: it loads libmpv, lists
// audio devices and, given a stream, plays it while printing state changes.
//
//	go run ./cmd/probe                     # check libmpv + list devices
//	go run ./cmd/probe somedj              # play a VRCDN stream over RTSP (UDP, then TCP)
//	go run ./cmd/probe -transport flv somedj
//	go run ./cmd/probe -for 30s -volume 0 somedj   # a silent 30-second check
//	go run ./cmd/probe -device "wasapi/{...}" rtsp://host/live/x
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"vrcdn-lowlatency-player/internal/libmpv"
	"vrcdn-lowlatency-player/internal/player"
	"vrcdn-lowlatency-player/internal/vrcdn"
)

func main() {
	transport := flag.String("transport", string(vrcdn.RTSP), "rtsp (UDP, then TCP), rtsp-udp, rtsp-tcp, flv or mpegts")
	device := flag.String("device", "auto", "audio device name")
	volume := flag.Float64("volume", 100, "volume 0-100")
	duration := flag.Duration("for", 0, "stop after this long, e.g. 30s (default: until Ctrl+C)")
	flag.Parse()

	engine, err := player.NewMPVEngine("VRCDN Low Latency Player (probe)")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		fmt.Fprintln(os.Stderr, player.LibraryHelp())
		os.Exit(1)
	}
	defer engine.Close()

	fmt.Printf("%s (loaded from %s)\n\naudio devices:\n", engine.Describe(), libmpv.LibraryPath())
	devices, err := engine.AudioDevices()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error listing devices:", err)
	}
	for _, d := range devices {
		fmt.Printf("  %-50s %s\n", d.Name, d.Description)
	}

	if flag.NArg() == 0 {
		return
	}

	src, err := vrcdn.Parse(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	tr := vrcdn.Transport(*transport)
	if !tr.Valid() {
		fmt.Fprintf(os.Stderr, "error: unknown transport %q\n", *transport)
		os.Exit(2)
	}
	target := player.Target{Label: src.Display(), URL: src.URL(tr), RTSPTransports: src.RTSPTransports(tr)}

	_ = engine.SetAudioDevice(*device)
	_ = engine.SetVolume(*volume)

	levelCount := 0
	opts := player.Options{AutoReconnect: true, AutoCatchUp: true, CatchUpAboveMs: 250}
	ctl := player.NewController(engine, opts, player.DefaultTuning(), player.Hooks{
		State: func(s player.State) {
			line := fmt.Sprintf("%-12s", s.Status)
			if s.BufferMs >= 0 {
				line += fmt.Sprintf(" buffer %4d ms", s.BufferMs)
			}
			if s.CatchingUp {
				line += " (catching up)"
			}
			if s.Info.Codec != "" {
				line += fmt.Sprintf("  %s %d Hz %dch", s.Info.Codec, s.Info.SampleRate, s.Info.Channels)
			}
			if s.Via != "" {
				line += "  via " + s.Via
			}
			if s.Message != "" {
				line += fmt.Sprintf("  %s (attempt %d)", s.Message, s.Attempt)
			}
			fmt.Println(line)
		},
		Levels: func(l player.Levels) {
			// About 20 readings a second is too much to print; show one a second.
			if levelCount++; levelCount%20 == 0 {
				fmt.Printf("level        L %6.1f dB  R %6.1f dB\n", l.Left, l.Right)
			}
		},
	})
	defer ctl.Close()

	fmt.Printf("\nplaying %s — Ctrl+C to quit\n", target.URL)
	ctl.Play(target)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	var timeUp <-chan time.Time
	if *duration > 0 {
		timeUp = time.After(*duration)
	}
	select {
	case <-interrupt:
	case <-timeUp:
	}
	ctl.Stop()
}
