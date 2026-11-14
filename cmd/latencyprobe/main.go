// Command latencyprobe measures how late audio arrives at a listener: the
// delay from a sound going into PartyLine's sender to the same sound coming
// out of the player's decoder, on one PC with one clock.
//
// It feeds the sender a test signal whose timing it knows (see package
// latency), listens the way the player does, and times each beep.
//
//	go run ./cmd/latencyprobe -loopback                 # sender straight into the listener: no network, no RTMP
//	go run ./cmd/latencyprobe -key-file key.txt         # through VRCDN, each output in turn
//	go run ./cmd/latencyprobe -key-file key.txt -via rtsp-udp -encoder coreaudio
//
// -loopback is the delay PartyLine and the codec add by themselves; taking
// it from a VRCDN figure leaves the network and VRCDN.
//
// What is not in the figure: the capture device's buffer on the sending
// side (20 ms in the app) and the listener's queue and audio device (the
// player's queue is 0-21 ms over RTSP). The signal generator delivers 10 ms
// at a time in the capture device's place.
//
// The stream key is read from a file so it never appears on a command line
// or in shell history, and it is never printed. This goes on air on that
// stream: use a private one.
package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"partyline/internal/latency"
	"partyline/internal/player"
	"partyline/internal/sender"
	"partyline/internal/vrcdn"
)

func main() {
	loopback := flag.Bool("loopback", false, "send straight into the listener on this PC, without VRCDN")
	keyFile := flag.String("key-file", "", "file holding the stream key of a private stream")
	ingest := flag.String("ingest", vrcdn.Ingests[0].URL, "RTMP ingest address")
	via := flag.String("via", "rtsp-udp,rtsp-tcp,mpegts,flv", "outputs to listen on, in turn")
	encoder := flag.String("encoder", sender.EncoderFFmpeg.ID, "ffmpeg or coreaudio")
	bitrate := flag.Int("bitrate", sender.Bitrates[0], "AAC bitrate in kbps")
	beeps := flag.Int("beeps", 25, "beeps to time on each output (one every 1.3 s)")
	flag.Parse()

	key := "probe"
	if !*loopback {
		if *keyFile == "" {
			fail("give -key-file, or -loopback")
		}
		raw, err := os.ReadFile(*keyFile)
		if err != nil {
			fail(err.Error())
		}
		key = strings.TrimSpace(string(raw))
	}
	scrub := func(s string) string { return strings.ReplaceAll(s, key, "<key>") }

	src, err := startSource()
	if err != nil {
		fail(err.Error())
	}

	live := make(chan struct{}, 1)
	s := sender.New(sender.DefaultTuning(), sender.Hooks{State: func(st sender.State) {
		fmt.Printf("sender: %s %s\n", st.Status, st.Message)
		if st.Status == sender.Live {
			select {
			case live <- struct{}{}:
			default:
			}
		}
	}})
	cfg := sender.Config{Ingest: *ingest, Key: key, Device: sender.PCMInput + src.addr, BitrateKbps: *bitrate, Encoder: *encoder}
	fmt.Printf("%s AAC at %d kbps, %d beeps per measurement\n", *encoder, *bitrate, *beeps)

	if *loopback {
		// The listener is the server here, so it has to be up first.
		port, err := freePort()
		if err != nil {
			fail(err.Error())
		}
		cfg.Ingest = fmt.Sprintf("%s%d", sender.LocalIngest, port)
		tap, err := player.OpenTap(cfg.Ingest+"?listen=1", "")
		if err != nil {
			fail(err.Error())
		}
		defer tap.Close()
		time.Sleep(500 * time.Millisecond)
		if err := s.Start(cfg); err != nil {
			fail(err.Error())
		}
		defer s.Stop()
		report("loopback", measure(tap, src.start, *beeps, 30*time.Second), scrub)
		return
	}

	if err := s.Start(cfg); err != nil {
		fail(err.Error())
	}
	defer s.Stop()
	select {
	case <-live:
	case <-time.After(30 * time.Second):
		s.Stop()
		fail("the sender did not go live")
	}

	source := vrcdn.Source{Name: key}
	for _, name := range strings.Split(*via, ",") {
		t := vrcdn.Transport(strings.TrimSpace(name))
		if !t.Valid() || t == vrcdn.RTSP {
			fmt.Printf("%-9s skipped: not one of rtsp-udp, rtsp-tcp, mpegts, flv\n", name)
			continue
		}
		rtsp := ""
		if ways := t.RTSPTransports(); len(ways) > 0 {
			rtsp = ways[0]
		}
		// A stream takes a few seconds to appear after the sender goes live.
		var res result
		for attempt := 0; attempt < 5; attempt++ {
			tap, err := player.OpenTap(source.URL(t), rtsp)
			if err != nil {
				fail(scrub(err.Error()))
			}
			res = measure(tap, src.start, *beeps, 15*time.Second)
			tap.Close()
			if len(res.fresh) > 0 || s.State().Status != sender.Live {
				break
			}
			time.Sleep(2 * time.Second)
		}
		report(string(t), res, scrub)
	}
}

// source serves the test signal to the sender in real time: frame n is
// generated at start + n/48000 s, in 10 ms pieces, whether or not anyone
// is connected, so a reconnecting sender finds the clock where it should be.
type source struct {
	addr  string
	start time.Time
	mu    sync.Mutex
	conn  net.Conn
}

func startSource() (*source, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &source{addr: ln.Addr().String(), start: time.Now()}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			if s.conn != nil {
				s.conn.Close()
			}
			s.conn = c
			s.mu.Unlock()
		}
	}()
	go func() {
		var gen latency.Source
		piece := make([]byte, latency.Rate/100*latency.FrameBytes)
		for {
			gen.Fill(piece)
			// A piece is handed over once its last frame exists.
			time.Sleep(time.Until(s.start.Add(time.Duration(gen.Pos()) * time.Second / latency.Rate)))
			s.mu.Lock()
			if s.conn != nil {
				_ = s.conn.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
				if _, err := s.conn.Write(piece); err != nil {
					s.conn.Close()
					s.conn = nil
				}
			}
			s.mu.Unlock()
		}
	}()
	return s, nil
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

type result struct {
	// fresh is how late each beep was when it was decoded.
	fresh []time.Duration
	// stale is how late the oldest audio in each burst was when the burst
	// arrived: what a player has to wait out to play without gaps.
	stale []time.Duration
	// burst is how much audio each burst held.
	burst []time.Duration
	first time.Duration // from opening the stream to its first audio
	err   error
}

// measure times beeps on a tap until it has enough. The first few are left
// out: an output may start by sending audio it has been holding.
//
// Audio reaches the tap in bursts: several frames at once, then a pause.
// When a beep's burst arrived says little about when the beep itself was
// decoded, but the end of a burst is a moment that is known: its last
// sample has only just been decoded. A stream plays out at one second per
// second, so a beep was decoded as long before a burst's end as it sits
// ahead of that end. The earliest such estimate over the bursts that
// follow is taken as the beep's arrival ("fresh"). The other end of a
// burst is its oldest sample, which has waited for the whole burst to
// build up ("stale").
func measure(tap *player.Tap, start time.Time, want int, patience time.Duration) result {
	const settle = 3
	type pendingBeep struct {
		latency.Beep
		seen time.Time
	}
	type burst struct {
		at     time.Time
		pos, n int64
	}
	var (
		res     result
		det     latency.Detector
		pending []pendingBeep
		bursts  []burst
		lastAt  time.Time
		seen    int
	)
	opened := time.Now()
	buf := make([]byte, 64<<10)
	for len(res.fresh) < want {
		n, err := tap.Read(buf, patience)
		now := time.Now()
		if err != nil {
			res.err = err
			return res
		}
		if res.first == 0 {
			res.first = now.Sub(opened)
		}
		patience = 5 * time.Second

		before := det.Pos()
		for _, b := range det.Feed(buf[:n], now) {
			pending = append(pending, pendingBeep{b, now})
		}
		// Reads that follow each other at once are one burst.
		if len(bursts) == 0 || now.Sub(lastAt) > 5*time.Millisecond {
			bursts = append(bursts, burst{at: now, pos: before})
		}
		bursts[len(bursts)-1].n += det.Pos() - before
		lastAt = now

		keep := pending[:0]
		for _, b := range pending {
			if est := now.Add(-latency.Frames(det.Pos() - b.Pos)); est.Before(b.At) {
				b.At = est
			}
			if now.Sub(b.seen) < 450*time.Millisecond {
				keep = append(keep, b)
				continue
			}
			src, ok := latency.SourcePos(start, b.Beep)
			if seen++; seen <= settle || !ok {
				bursts = bursts[len(bursts)-1:]
				continue
			}
			res.fresh = append(res.fresh, b.At.Sub(start)-latency.Frames(src))
			// The beep ties positions in the received audio to positions in
			// the source, and so every burst since the last beep to a time.
			shift := src - b.Pos
			for _, u := range bursts[:len(bursts)-1] {
				res.stale = append(res.stale, u.at.Sub(start)-latency.Frames(u.pos+shift))
				res.burst = append(res.burst, latency.Frames(u.n))
			}
			bursts = bursts[len(bursts)-1:]
		}
		pending = keep
	}
	return res
}

func report(name string, res result, scrub func(string) string) {
	if len(res.fresh) == 0 {
		err := res.err
		if err == nil {
			err = errors.New("no beeps were heard")
		}
		fmt.Printf("%-9s failed: %s\n", name, scrub(err.Error()))
		return
	}
	fresh, stale, burst := summarise(res.fresh), summarise(res.stale), summarise(res.burst)
	fmt.Printf("%-9s freshest audio %3d ms (%d-%d) | oldest in a burst %3d ms, 90%% within %d, worst %d | bursts of %d ms | %d beeps, first audio after %.1f s\n",
		name, fresh.median, fresh.min, fresh.max, stale.median, stale.p90, stale.max, burst.median, len(res.fresh), res.first.Seconds())
	if res.err != nil {
		fmt.Printf("%-9s then stopped: %s\n", name, scrub(res.err.Error()))
	}
	if time.Duration(stale.max)*time.Millisecond > latency.Window*9/10 {
		fmt.Printf("%-9s warning: close to the %v this can measure; the real delay may be longer\n", name, latency.Window.Round(time.Second))
	}
}

type summary struct{ min, median, p90, max int64 }

func summarise(d []time.Duration) summary {
	if len(d) == 0 {
		return summary{}
	}
	d = append([]time.Duration(nil), d...)
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return summary{d[0].Milliseconds(), d[len(d)/2].Milliseconds(), d[(len(d)*9+9)/10-1].Milliseconds(), d[len(d)-1].Milliseconds()}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "error:", msg)
	os.Exit(1)
}
