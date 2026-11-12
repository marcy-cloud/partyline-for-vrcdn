//go:build windows

// The package encodes AAC with Apple's encoder, the one OBS calls
// "CoreAudio AAC". Apple does not let it be redistributed: it is used only
// if Apple Application Support (installed with iTunes) is already on the PC.
package coreaudio

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Four-character codes from Apple's AudioToolbox headers.
const (
	formatLPCM   = 0x6C70636D // 'lpcm'
	formatAAC    = 0x61616320 // 'aac ': MPEG-4 AAC-LC
	flagsS16     = 0x4 | 0x8  // signed integer, packed
	propBitRate  = 0x62726174 // 'brat'
	propRateMode = 0x61636266 // 'acbf': bit rate control mode
	propQuality  = 0x63647175 // 'cdqu'
	propMaxOut   = 0x786F7073 // 'xops': maximum output packet size
	modeCBR      = 0
	qualityMax   = 0x7F
	// noMoreInput is what the input callback returns when it has no PCM
	// to give right now. Any non-zero value works; the converter hands it
	// back from FillComplexBuffer.
	noMoreInput = 0x706C6E65 // 'plne'
)

// FrameSize is how many samples per channel go into one AAC packet.
const FrameSize = 1024

type streamDescription struct {
	sampleRate       float64
	formatID         uint32
	formatFlags      uint32
	bytesPerPacket   uint32
	framesPerPacket  uint32
	bytesPerFrame    uint32
	channelsPerFrame uint32
	bitsPerChannel   uint32
	reserved         uint32
}

type audioBufferList struct {
	numberBuffers  uint32
	_              uint32
	numberChannels uint32
	dataByteSize   uint32
	data           unsafe.Pointer
}

var (
	loadOnce sync.Once
	loadErr  error
	dllPath  string

	procNew, procDispose, procSetProperty, procGetProperty, procFill *windows.LazyProc
)

// ErrNotInstalled means Apple's encoder is not on this PC.
var ErrNotInstalled = errors.New("Apple's AAC encoder is not installed (it comes with iTunes)")

// Find returns the path of CoreAudioToolbox.dll, or "" if it isn't installed.
func Find() string {
	// Apple Application Support records where it lives.
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Apple Inc.\Apple Application Support`, registry.QUERY_VALUE); err == nil {
		defer k.Close()
		if dir, _, err := k.GetStringValue("InstallDir"); err == nil {
			if p := filepath.Join(dir, "CoreAudioToolbox.dll"); exists(p) {
				return p
			}
		}
	}
	return ""
}

func exists(path string) bool {
	_, err := windows.GetFileAttributes(windows.StringToUTF16Ptr(path))
	return err == nil
}

// Available reports whether the encoder can be used, loading it if need be.
func Available() error {
	loadOnce.Do(func() { loadErr = load() })
	return loadErr
}

func load() error {
	dllPath = Find()
	if dllPath == "" {
		return ErrNotInstalled
	}
	// The DLL leans on others in its own folder; this flag makes Windows
	// look there for them.
	h, err := windows.LoadLibraryEx(dllPath, 0, windows.LOAD_WITH_ALTERED_SEARCH_PATH)
	if err != nil {
		return fmt.Errorf("loading %s: %w", dllPath, err)
	}
	dll := &windows.LazyDLL{Name: dllPath}
	_ = h
	procNew = dll.NewProc("AudioConverterNew")
	procDispose = dll.NewProc("AudioConverterDispose")
	procSetProperty = dll.NewProc("AudioConverterSetProperty")
	procGetProperty = dll.NewProc("AudioConverterGetProperty")
	procFill = dll.NewProc("AudioConverterFillComplexBuffer")
	for _, p := range []*windows.LazyProc{procNew, procDispose, procSetProperty, procGetProperty, procFill} {
		if err := p.Find(); err != nil {
			return fmt.Errorf("%s is not usable: %w", dllPath, err)
		}
	}
	return nil
}

// Path returns where the encoder was loaded from.
func Path() string { return dllPath }

// Encoder turns 16-bit PCM into AAC-LC packets.
type Encoder struct {
	conv     uintptr
	channels int
	out      []byte
	callback uintptr

	// The PCM for the packet being encoded, handed over in the callback.
	pending []byte
	pinner  runtime.Pinner
}

// NewEncoder returns an AAC-LC encoder for interleaved signed 16-bit PCM.
func NewEncoder(sampleRate, channels, bitrateKbps int) (*Encoder, error) {
	if err := Available(); err != nil {
		return nil, err
	}
	in := streamDescription{
		sampleRate:       float64(sampleRate),
		formatID:         formatLPCM,
		formatFlags:      flagsS16,
		bytesPerPacket:   uint32(2 * channels),
		framesPerPacket:  1,
		bytesPerFrame:    uint32(2 * channels),
		channelsPerFrame: uint32(channels),
		bitsPerChannel:   16,
	}
	out := streamDescription{
		sampleRate:       float64(sampleRate),
		formatID:         formatAAC,
		framesPerPacket:  FrameSize,
		channelsPerFrame: uint32(channels),
	}
	e := &Encoder{channels: channels}
	if st, _, _ := procNew.Call(uintptr(unsafe.Pointer(&in)), uintptr(unsafe.Pointer(&out)), uintptr(unsafe.Pointer(&e.conv))); st != 0 {
		return nil, status("AudioConverterNew", st)
	}

	set := func(name string, id uint32, value uint32) error {
		if st, _, _ := procSetProperty.Call(e.conv, uintptr(id), 4, uintptr(unsafe.Pointer(&value))); st != 0 {
			return status("setting "+name, st)
		}
		return nil
	}
	// Constant bitrate, as VRCDN's limits are on the bitrate; then the
	// bitrate itself, which the mode has to be set before.
	for _, p := range []struct {
		name string
		id   uint32
		v    uint32
	}{
		{"the rate control mode", propRateMode, modeCBR},
		{"the bitrate", propBitRate, uint32(bitrateKbps * 1000)},
		{"the quality", propQuality, qualityMax},
	} {
		if err := set(p.name, p.id, p.v); err != nil {
			e.Close()
			return nil, err
		}
	}

	var maxOut, size uint32 = 0, 4
	if st, _, _ := procGetProperty.Call(e.conv, propMaxOut, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&maxOut))); st != 0 || maxOut == 0 {
		maxOut = 8192
	}
	e.out = make([]byte, maxOut)
	e.callback = syscall.NewCallback(e.supply)
	return e, nil
}

// supply is called by the converter when it wants PCM.
func (e *Encoder) supply(conv uintptr, ioPackets *uint32, ioData *audioBufferList, outDesc, user uintptr) uintptr {
	if len(e.pending) == 0 {
		*ioPackets = 0
		return noMoreInput
	}
	frames := uint32(len(e.pending) / (2 * e.channels))
	if *ioPackets < frames {
		frames = *ioPackets
	}
	n := int(frames) * 2 * e.channels
	ioData.numberBuffers = 1
	ioData.numberChannels = uint32(e.channels)
	ioData.dataByteSize = uint32(n)
	ioData.data = unsafe.Pointer(&e.pending[0])
	*ioPackets = frames
	e.pending = e.pending[n:]
	return 0
}

// Encode takes one frame of PCM (FrameSize samples per channel, interleaved
// signed 16-bit little-endian) and returns one AAC packet, or nil while the
// encoder is still filling its look-ahead. The packet is only valid until
// the next call.
func (e *Encoder) Encode(pcm []byte) ([]byte, error) {
	if len(pcm) != FrameSize*2*e.channels {
		return nil, fmt.Errorf("Encode wants %d bytes of PCM, got %d", FrameSize*2*e.channels, len(pcm))
	}
	// The converter reads the PCM during the call below, from C.
	e.pinner.Pin(&pcm[0])
	defer e.pinner.Unpin()
	e.pending = pcm

	list := audioBufferList{numberBuffers: 1, numberChannels: uint32(e.channels), dataByteSize: uint32(len(e.out)), data: unsafe.Pointer(&e.out[0])}
	packets := uint32(1)
	st, _, _ := procFill.Call(e.conv, e.callback, 0, uintptr(unsafe.Pointer(&packets)), uintptr(unsafe.Pointer(&list)), 0)
	e.pending = nil
	if st != 0 && st != noMoreInput {
		return nil, status("encoding", st)
	}
	if packets == 0 {
		return nil, nil
	}
	return e.out[:list.dataByteSize], nil
}

// Close releases the encoder.
func (e *Encoder) Close() {
	if e.conv != 0 {
		procDispose.Call(e.conv)
		e.conv = 0
	}
}

// status formats an OSStatus, which is often a four-character code.
func status(what string, st uintptr) error {
	code := uint32(st)
	b := []byte{byte(code >> 24), byte(code >> 16), byte(code >> 8), byte(code)}
	for _, c := range b {
		if c < 32 || c > 126 {
			return fmt.Errorf("%s failed: CoreAudio error %d", what, int32(code))
		}
	}
	return fmt.Errorf("%s failed: CoreAudio error '%s'", what, b)
}
