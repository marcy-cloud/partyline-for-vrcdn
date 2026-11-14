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
	// Program marks a running program whose sound can be sent, rather than
	// an input device.
	Program bool `json:"program,omitempty"`
}

// TestTone is a built-in input: a steady 440 Hz tone at about -12 dBFS, for
// checking the link without any gear connected.
var TestTone = Device{ID: "test-tone", Name: "Test tone (440 Hz)"}

// PCMInput is the prefix of a device ID that isn't a device: "pcm:" followed
// by host:port reads raw 48 kHz stereo 16-bit PCM from a TCP server there.
// Measurement tools use it to feed the sender audio whose timing they know;
// it is never listed.
const PCMInput = "pcm:"

// Supported reports whether this build can capture audio to send. Capture
// uses DirectShow, so for now that means Windows.
func Supported() bool { return runtime.GOOS == "windows" }

// lavfi's sine is -18 dB and loses 3 dB going from mono to stereo; arealtime
// paces it, as lavfi would otherwise run as fast as the encoder allows.
const testToneURL = "av://lavfi:sine=frequency=440:sample_rate=48000,volume=9dB,aformat=channel_layouts=stereo,arealtime"

// pcmRateFix is an audio filter that must come first for a PCMInput: it
// relabels the audio as 48 kHz without touching the samples, and numbers
// the timestamps afresh to match. See inputURL.
const pcmRateFix = "lavfi=[asetrate=48000,asetpts=N/SR/TB]"

// filters returns the value of mpv's "af" option for a device, given the
// filters wanted after any the input itself needs.
func filters(id string, then ...string) string {
	if strings.HasPrefix(id, PCMInput) {
		then = append([]string{pcmRateFix}, then...)
	}
	return strings.Join(then, ",")
}

// inputURL returns what to hand libmpv for a device, and the mpv
// options that go with it.
func inputURL(id string) (url string, options [][2]string, err error) {
	if id == TestTone.ID {
		return testToneURL, nil, nil
	}
	if addr, ok := strings.CutPrefix(id, PCMInput); ok {
		// libavformat reads raw PCM a tenth of a second at a time, rounded
		// down to a power of two frames: 4096 at 48 kHz, 85 ms held back
		// before the encoder sees any of it. Told the audio is 6 kHz it
		// takes 512 frames, which at the real rate is 11 ms. pcmRateFix
		// then tells the truth again.
		return "tcp://" + addr, [][2]string{
			{"demuxer-lavf-format", "s16le"},
			{"demuxer-lavf-o", "sample_rate=6000,ch_layout=stereo,fflags=+nobuffer"},
		}, nil
	}
	if !Supported() {
		return "", nil, errors.New("sending from an input device is only available on Windows for now")
	}
	// DirectShow buffers half a second of audio unless told otherwise.
	return "av://dshow:audio=" + id, [][2]string{{"demuxer-lavf-o", "audio_buffer_size=20,fflags=+nobuffer"}}, nil
}

var (
	deviceLine = regexp.MustCompile(`"(.+)" \(audio\)`)
	altLine    = regexp.MustCompile(`Alternative name "(.+)"`)
)

// Devices lists what can be sent: input devices, then running programs
// that have sound open, and the test tone last.
func Devices() ([]Device, error) {
	if !Supported() {
		return []Device{TestTone}, nil
	}
	lines, err := dshowListing()
	if err != nil {
		return append(programs(), TestTone), err
	}
	return append(append(parseDevices(lines), programs()...), TestTone), nil
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

// dshowListing asks ffmpeg, through a throwaway libmpv core, to list
// DirectShow devices. The list only comes out as log lines.
func dshowListing() ([]string, error) {
	m, err := libmpv.New()
	if err != nil {
		return nil, err
	}
	defer m.Destroy()
	for _, kv := range [][2]string{
		{"config", "no"}, {"terminal", "no"}, {"ytdl", "no"}, {"load-scripts", "no"},
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
	if err := m.Command("loadfile", "av://dshow:dummy"); err != nil {
		return nil, err
	}

	var lines []string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ev := m.WaitEvent(0.2)
		switch ev.ID {
		case libmpv.EventLogMessage:
			if strings.Contains(ev.LogText, "dshow") {
				lines = append(lines, ev.LogText)
			}
		case libmpv.EventEndFile, libmpv.EventShutdown:
			return lines, nil
		}
	}
	return lines, nil
}
