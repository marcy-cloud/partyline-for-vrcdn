// Command sendprobe is an experiment: can libmpv, as bundled, encode audio
// only and push it to an RTMP ingest (or a file), with no video track?
//
//	go run ./cmd/sendprobe -to out.flv -for 5s              # test tone into a file
//	go run ./cmd/sendprobe -key-file key.txt -for 30s       # test tone to VRCDN
//	go run ./cmd/sendprobe -input "av://dshow:audio=Line In" -key-file key.txt
//
// The stream key is read from a file so it never appears on a command line
// or in shell history.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"partyline/internal/libmpv"
)

// A 440 Hz tone that peaks at about -12 dBFS on both channels, paced in real
// time (lavfi would otherwise produce it as fast as the encoder can take
// it). lavfi's sine is -18 dB and loses 3 dB going from mono to stereo.
const testTone = "av://lavfi:sine=frequency=440:sample_rate=48000,volume=9dB,aformat=channel_layouts=stereo,arealtime"

func main() {
	input := flag.String("input", testTone, "what to send: an mpv URL such as av://dshow:audio=<device>")
	to := flag.String("to", "", "output file or URL; overrides -ingest and -key-file")
	ingest := flag.String("ingest", "rtmp://ingest.vrcdn.live/live", "RTMP ingest address")
	keyFile := flag.String("key-file", "", "file holding the stream key")
	bitrate := flag.Int("bitrate", 320, "AAC bitrate in kbps")
	duration := flag.Duration("for", 0, "stop after this long (default: until Ctrl+C)")
	verbose := flag.Bool("v", false, "show mpv's informational log too")
	flag.Parse()

	target, shown := *to, *to
	if target == "" {
		if *keyFile == "" {
			fail("give -to or -key-file")
		}
		raw, err := os.ReadFile(*keyFile)
		if err != nil {
			fail(err.Error())
		}
		key := strings.TrimSpace(string(raw))
		if key == "" {
			fail("the key file is empty")
		}
		target = strings.TrimRight(*ingest, "/") + "/" + key
		shown = strings.TrimRight(*ingest, "/") + "/<key>"
	}

	m, err := libmpv.New()
	if err != nil {
		fail(err.Error())
	}
	options := [][2]string{
		{"config", "no"}, {"terminal", "no"}, {"ytdl", "no"}, {"load-scripts", "no"},
		{"vid", "no"}, {"sid", "no"},
		// Encode instead of play.
		{"o", target},
		{"of", "flv"},
		{"oac", "aac"},
		{"oacopts", fmt.Sprintf("b=%dk", *bitrate)},
		{"audio-samplerate", "48000"},
		{"audio-channels", "stereo"},
	}
	for _, kv := range options {
		if err := m.SetOption(kv[0], kv[1]); err != nil {
			fail(fmt.Sprintf("option %s: %v", kv[0], err))
		}
	}
	if err := m.Initialize(); err != nil {
		fail(err.Error())
	}
	level := "warn"
	if *verbose {
		level = "v"
	}
	_ = m.RequestLogMessages(level)

	version, _ := m.GetString("mpv-version")
	fmt.Printf("%s\nsending %s\n     to %s (AAC %d kbps, 48 kHz stereo, no video)\n", version, *input, shown, *bitrate)
	if err := m.Command("loadfile", *input); err != nil {
		fail(err.Error())
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	var timeUp <-chan time.Time
	if *duration > 0 {
		timeUp = time.After(*duration)
	}
	began := time.Now()
	lastReport := began

loop:
	for {
		select {
		case <-interrupt:
			break loop
		case <-timeUp:
			break loop
		default:
		}
		ev := m.WaitEvent(0.25)
		switch ev.ID {
		case libmpv.EventLogMessage:
			// Never echo the key, which mpv includes in the output URL.
			fmt.Println("mpv:", strings.ReplaceAll(strings.TrimSpace(ev.LogText), target, shown))
		case libmpv.EventEndFile:
			fmt.Printf("ended after %.1fs: reason %d %s\n", time.Since(began).Seconds(), ev.Reason, ev.EndErr)
			break loop
		case libmpv.EventShutdown:
			break loop
		}
		if time.Since(lastReport) >= 2*time.Second {
			lastReport = time.Now()
			pos, _ := m.GetDouble("time-pos")
			fmt.Printf("  %5.1fs elapsed, %5.1fs of audio sent\n", time.Since(began).Seconds(), pos)
		}
	}
	// Destroying the core finishes the output properly.
	m.Destroy()
	fmt.Println("stopped")
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "error:", msg)
	os.Exit(1)
}
