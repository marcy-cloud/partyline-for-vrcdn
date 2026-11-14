// Command loopprobe tries out capturing one application's sound (package
// loopback) without the GUI.
//
//	loopprobe -list                     # programs that have sound open
//	loopprobe -tone 20s                 # be an application playing a very quiet tone
//	loopprobe -exe loopprobe.exe        # capture that, by program name
//	loopprobe -pid 1234 -for 10s        # or by process ID
package main

import (
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"sync"
	"time"

	"partyline/internal/libmpv"
	"partyline/internal/loopback"
	"partyline/internal/meter"
	"partyline/internal/player"
	"partyline/internal/sender"
)

func main() {
	tone := flag.Duration("tone", 0, "play a 440 Hz tone at -60 dBFS for this long, and exit")
	list := flag.Bool("list", false, "list the programs that have sound open")
	exe := flag.String("exe", "", "capture the program with this file name")
	pid := flag.Uint("pid", 0, "capture the process with this ID")
	send := flag.Bool("send", false, "with -exe: send it through the real sender (FFmpeg AAC) into a listener on this PC, and report what comes out")
	duration := flag.Duration("for", 8*time.Second, "how long to capture")
	flag.Parse()

	if *list {
		programs, err := loopback.Programs()
		if err != nil {
			fail(err.Error())
		}
		for _, p := range programs {
			state := "quiet"
			if p.Playing {
				state = "playing"
			}
			fmt.Printf("%-8s %-28s %s (process %d)\n", state, p.Exe, p.Name, p.PID)
		}
		return
	}
	if *tone > 0 {
		playTone(*tone)
		return
	}
	if *send {
		throughSender(*exe, *duration)
		return
	}
	target := uint32(*pid)
	if target == 0 {
		if *exe == "" {
			fail("give -exe or -pid")
		}
		p, err := loopback.Find(*exe)
		if err != nil {
			fail(err.Error())
		}
		target = p.PID
	}
	c, err := loopback.Open(target)
	if err != nil {
		fail(err.Error())
	}
	defer c.Close()
	fmt.Printf("capturing process %d\n", target)

	buf := make([]byte, 1<<16)
	begin := time.Now()
	var pieces, bytes, peak, quietReads int
	var longest time.Duration
	last, nextReport := begin, begin.Add(time.Second)
	for time.Since(begin) < *duration {
		n, err := c.Read(buf, 200*time.Millisecond)
		if err != nil {
			fail(err.Error())
		}
		now := time.Now()
		if n == 0 {
			quietReads++
		} else {
			if pieces > 0 {
				longest = max(longest, now.Sub(last))
			}
			last = now
			pieces++
			bytes += n
			for i := 0; i+1 < n; i += 2 {
				v := int(int16(uint16(buf[i]) | uint16(buf[i+1])<<8))
				peak = max(peak, max(v, -v))
			}
		}
		if now.After(nextReport) {
			level := "silence"
			if peak > 0 {
				level = fmt.Sprintf("%.1f dBFS", 20*math.Log10(float64(peak)/32768))
			}
			fmt.Printf("%4.1fs  %3d pieces, %5.0f ms of audio, peak %s, longest wait %d ms\n", now.Sub(begin).Seconds(), pieces,
				float64(bytes)/loopback.FrameBytes/loopback.Rate*1000, level, longest.Milliseconds())
			pieces, bytes, peak, longest = 0, 0, 0, 0
			nextReport = nextReport.Add(time.Second)
		}
	}
}

// throughSender sends a program's sound the way the app would, but to a
// listener on this PC instead of a server, and prints the level going in
// (the sender's meter) and the level of what the listener decodes.
func throughSender(exe string, d time.Duration) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fail(err.Error())
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	ingest := fmt.Sprintf("%s%d", sender.LocalIngest, port)
	tap, err := player.OpenTap(ingest+"?listen=1", "")
	if err != nil {
		fail(err.Error())
	}
	defer tap.Close()
	time.Sleep(500 * time.Millisecond)

	var mu sync.Mutex
	in := meter.Levels{Left: meter.Silence, Right: meter.Silence}
	s := sender.New(sender.DefaultTuning(), sender.Hooks{
		State: func(st sender.State) { fmt.Printf("sender: %s %s\n", st.Status, st.Message) },
		Levels: func(l meter.Levels) {
			mu.Lock()
			in.Left, in.Right = math.Max(in.Left, l.Left), math.Max(in.Right, l.Right)
			mu.Unlock()
		},
	})
	if err := s.Start(sender.Config{Ingest: ingest, Key: "probe", Device: sender.AppInput + exe, BitrateKbps: 320}); err != nil {
		fail(err.Error())
	}
	defer s.Stop()

	buf := make([]byte, 1<<16)
	begin := time.Now()
	next := begin.Add(time.Second)
	var peakL, peakR, bytes int
	for time.Since(begin) < d {
		n, err := tap.Read(buf, 10*time.Second)
		if err != nil {
			fmt.Println("listener:", err)
			return
		}
		bytes += n
		for i := 0; i+3 < n; i += 4 {
			l, r := int(int16(uint16(buf[i])|uint16(buf[i+1])<<8)), int(int16(uint16(buf[i+2])|uint16(buf[i+3])<<8))
			peakL, peakR = max(peakL, max(l, -l)), max(peakR, max(r, -r))
		}
		if now := time.Now(); now.After(next) {
			db := func(v int) float64 { return 20 * math.Log10(math.Max(float64(v), 1)/32768) }
			mu.Lock()
			fmt.Printf("%4.1fs  into the sender L %6.1f R %6.1f dB | out of the listener L %6.1f R %6.1f dB, %4.0f ms of audio\n",
				now.Sub(begin).Seconds(), in.Left, in.Right, db(peakL), db(peakR), float64(bytes)/4/48000*1000)
			in = meter.Levels{Left: meter.Silence, Right: meter.Silence}
			mu.Unlock()
			peakL, peakR, bytes = 0, 0, 0
			next = next.Add(time.Second)
		}
	}
}

// playTone makes this process an application that is playing something.
func playTone(d time.Duration) {
	m, err := libmpv.New()
	if err != nil {
		fail(err.Error())
	}
	defer m.Destroy()
	for _, kv := range [][2]string{{"config", "no"}, {"terminal", "no"}, {"vid", "no"}, {"audio-client-name", "loopprobe tone"}} {
		_ = m.SetOption(kv[0], kv[1])
	}
	if err := m.Initialize(); err != nil {
		fail(err.Error())
	}
	// lavfi's sine is -18 dBFS: 42 dB down from there is too quiet to bother anyone.
	if err := m.Command("loadfile", "av://lavfi:sine=frequency=440:sample_rate=48000,volume=-42dB,aformat=channel_layouts=stereo"); err != nil {
		fail(err.Error())
	}
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		m.WaitEvent(0.2)
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "error:", msg)
	os.Exit(1)
}
