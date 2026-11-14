// Command loopprobe tries out capturing one application's sound (package
// loopback) without the GUI.
//
//	loopprobe -tone 20s                 # be an application playing a very quiet tone
//	loopprobe -exe loopprobe.exe        # capture that, by program name
//	loopprobe -pid 1234 -for 10s        # or by process ID
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"partyline/internal/libmpv"
	"partyline/internal/loopback"
)

func main() {
	tone := flag.Duration("tone", 0, "play a 440 Hz tone at -60 dBFS for this long, and exit")
	exe := flag.String("exe", "", "capture the program with this file name")
	pid := flag.Uint("pid", 0, "capture the process with this ID")
	duration := flag.Duration("for", 8*time.Second, "how long to capture")
	flag.Parse()

	if *tone > 0 {
		playTone(*tone)
		return
	}
	target := uint32(*pid)
	if target == 0 {
		if *exe == "" {
			fail("give -exe or -pid")
		}
		var err error
		if target, err = findProcess(*exe); err != nil {
			fail(err.Error())
		}
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

// findProcess returns the oldest process running the named program other
// than this one: for a program with helper processes, that is their parent.
func findProcess(name string) (uint32, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	byPID := map[uint32]windows.ProcessEntry32{}
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		byPID[e.ProcessID] = e
	}
	self := uint32(os.Getpid())
	for pid, e := range byPID {
		if pid == self || !strings.EqualFold(filepath.Base(windows.UTF16ToString(e.ExeFile[:])), name) {
			continue
		}
		parent, ok := byPID[e.ParentProcessID]
		if ok && e.ParentProcessID != self && strings.EqualFold(windows.UTF16ToString(parent.ExeFile[:]), name) {
			continue // a helper; its parent is the one to capture
		}
		return pid, nil
	}
	return 0, fmt.Errorf("no program called %s is running", name)
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "error:", msg)
	os.Exit(1)
}
