package sender

import (
	"errors"
	"regexp"
	"runtime"
	"strings"
	"time"

	"partyline/internal/libmpv"
)

// Device is an audio input that can be sent.
type Device struct {
	// ID identifies the device to Start. It is stable across runs.
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TestTone is a built-in input: a steady 440 Hz tone at about -12 dBFS, for
// checking the link without any gear connected.
var TestTone = Device{ID: "test-tone", Name: "Test tone (440 Hz)"}

// PCMInput is the prefix of a device ID that isn't a device: "pcm:" followed
// by host:port reads raw 48 kHz stereo 16-bit PCM from a TCP server there.
// Measurement tools use it to feed the sender audio whose timing they know;
// it is never listed. The engine reads raw PCM 4096 frames (85 ms) at a time,
// where a capture device delivers about 20 ms; nothing here can change that.
const PCMInput = "pcm:"

// Supported reports whether this build can capture audio to send. Capture
// uses DirectShow on Windows and AVFoundation on macOS.
func Supported() bool { return captureFormat() != "" }

// captureFormat is the name of ffmpeg's input device for this system, or ""
// where there is none this program knows how to use.
func captureFormat() string {
	switch runtime.GOOS {
	case "windows":
		return "dshow"
	case "darwin":
		return "avfoundation"
	}
	return ""
}

// lavfi's sine is -18 dB and loses 3 dB going from mono to stereo; arealtime
// paces it, as lavfi would otherwise run as fast as the encoder allows.
const testToneURL = "av://lavfi:sine=frequency=440:sample_rate=48000,volume=9dB,aformat=channel_layouts=stereo,arealtime"

// inputURL returns what to hand libmpv for a device, and the mpv
// options that go with it.
func inputURL(id string) (url string, options [][2]string, err error) {
	if id == TestTone.ID {
		return testToneURL, nil, nil
	}
	if addr, ok := strings.CutPrefix(id, PCMInput); ok {
		return "tcp://" + addr, [][2]string{
			{"demuxer-lavf-format", "s16le"},
			{"demuxer-lavf-o", "sample_rate=48000,ch_layout=stereo,fflags=+nobuffer"},
		}, nil
	}
	switch captureFormat() {
	case "dshow":
		// DirectShow buffers half a second of audio unless told otherwise.
		return "av://dshow:audio=" + id, [][2]string{{"demuxer-lavf-o", "audio_buffer_size=20,fflags=+nobuffer"}}, nil
	case "avfoundation":
		// "video:audio", with no video. AVFoundation hands audio over as
		// it arrives and has no buffer to shorten.
		return "av://avfoundation:none:" + id, [][2]string{{"demuxer-lavf-o", "fflags=+nobuffer"}}, nil
	}
	return "", nil, errors.New("sending from an input device is only available on Windows and macOS for now")
}

var (
	deviceLine = regexp.MustCompile(`"(.+)" \(audio\)`)
	altLine    = regexp.MustCompile(`Alternative name "(.+)"`)
	// AVFoundation's listing: a heading for each kind, then "[0] Name".
	avfHeading = regexp.MustCompile(`AVFoundation (\w+) devices:`)
	avfLine    = regexp.MustCompile(`\[\d+\] (.+)$`)
)

// Devices lists the inputs that can be sent, the test tone last.
func Devices() ([]Device, error) {
	if !Supported() {
		return []Device{TestTone}, nil
	}
	format := captureFormat()
	lines, err := deviceListing(format)
	if err != nil {
		return []Device{TestTone}, err
	}
	if format == "avfoundation" {
		return append(parseAVFoundationDevices(lines), TestTone), nil
	}
	return append(parseDevices(lines), TestTone), nil
}

// parseDevices reads ffmpeg's DirectShow device listing, which names each
// device and then, on the next line, gives it a stable "alternative name".
// The alternative name is used as the ID: friendly names are not unique and
// can contain characters DirectShow's option syntax trips over.
func parseDevices(lines []string) []Device {
	var out []Device
	pending := ""
	for _, line := range lines {
		if m := altLine.FindStringSubmatch(line); m != nil {
			if pending != "" {
				out = append(out, Device{ID: m[1], Name: pending})
			}
			pending = ""
			continue
		}
		pending = ""
		if m := deviceLine.FindStringSubmatch(line); m != nil {
			pending = m[1]
		}
	}
	return out
}

// parseAVFoundationDevices reads ffmpeg's AVFoundation device listing:
//
//	AVFoundation video devices:
//	[0] FaceTime HD Camera
//	AVFoundation audio devices:
//	[0] MacBook Pro Microphone
//
// A device is opened by name, so the name is the ID as well. (The number is
// only its place in the list, which changes as devices come and go.)
func parseAVFoundationDevices(lines []string) []Device {
	var out []Device
	audio := false
	for _, line := range lines {
		if m := avfHeading.FindStringSubmatch(line); m != nil {
			audio = m[1] == "audio"
			continue
		}
		if !audio {
			continue
		}
		if m := avfLine.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			out = append(out, Device{ID: m[1], Name: m[1]})
		}
	}
	return out
}

// deviceListing asks ffmpeg, through a throwaway libmpv core, to list the
// devices of one of its input formats (dshow, avfoundation). The list only
// comes out as log lines.
func deviceListing(format string) ([]string, error) {
	m, err := libmpv.New()
	if err != nil {
		return nil, err
	}
	defer m.Destroy()
	bare(m)
	for _, kv := range [][2]string{
		{"vid", "no"}, {"ao", "null"}, {"idle", "yes"},
		{"demuxer-lavf-o", "list_devices=true"},
	} {
		_ = m.SetOption(kv[0], kv[1])
	}
	if err := m.Initialize(); err != nil {
		return nil, err
	}
	_ = m.RequestLogMessages("v")
	// The "open" always fails; listing is a side effect of trying.
	if err := m.Command("loadfile", "av://"+format+":dummy"); err != nil {
		return nil, err
	}

	var lines []string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ev := m.WaitEvent(0.2)
		switch ev.ID {
		case libmpv.EventLogMessage:
			// ffmpeg names the source of each line: "dshow: ..." or
			// "AVFoundation indev: ...".
			if strings.Contains(strings.ToLower(ev.LogText), format) {
				lines = append(lines, ev.LogText)
			}
		case libmpv.EventEndFile, libmpv.EventShutdown:
			return lines, nil
		}
	}
	return lines, nil
}
