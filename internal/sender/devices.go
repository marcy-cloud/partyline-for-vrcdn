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

// Supported reports whether this build can capture audio to send. Capture
// uses DirectShow, so for now that means Windows.
func Supported() bool { return runtime.GOOS == "windows" }

// lavfi's sine is -18 dB and loses 3 dB going from mono to stereo; arealtime
// paces it, as lavfi would otherwise run as fast as the encoder allows.
const testToneURL = "av://lavfi:sine=frequency=440:sample_rate=48000,volume=9dB,aformat=channel_layouts=stereo,arealtime"

// inputURL returns what to hand libmpv for a device, and the libavformat
// options that go with it.
func inputURL(id string) (url, options string, err error) {
	if id == TestTone.ID {
		return testToneURL, "", nil
	}
	if !Supported() {
		return "", "", errors.New("sending from an input device is only available on Windows for now")
	}
	// DirectShow buffers half a second of audio unless told otherwise.
	return "av://dshow:audio=" + id, "audio_buffer_size=20,fflags=+nobuffer", nil
}

var (
	deviceLine = regexp.MustCompile(`"(.+)" \(audio\)`)
	altLine    = regexp.MustCompile(`Alternative name "(.+)"`)
)

// Devices lists the inputs that can be sent, the test tone last.
func Devices() ([]Device, error) {
	if !Supported() {
		return []Device{TestTone}, nil
	}
	lines, err := dshowListing()
	if err != nil {
		return []Device{TestTone}, err
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
