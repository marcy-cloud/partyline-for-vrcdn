package loopback

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The audio is always 48 kHz stereo 16-bit little-endian; Windows converts.
const (
	Rate       = 48000
	Channels   = 2
	FrameBytes = Channels * 2
)

var (
	mmdevapi                     = windows.NewLazySystemDLL("mmdevapi.dll")
	procActivateAudioInterface   = mmdevapi.NewProc("ActivateAudioInterfaceAsync")
	ole32                        = windows.NewLazySystemDLL("ole32.dll")
	procCoInitializeEx           = ole32.NewProc("CoInitializeEx")
	procCoUninitialize           = ole32.NewProc("CoUninitialize")
	iidIUnknown                  = windows.GUID{Data1: 0x00000000, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIAgileObject              = windows.GUID{Data1: 0x94EA2B94, Data2: 0xE9CC, Data3: 0x49E0, Data4: [8]byte{0xC0, 0xFF, 0xEE, 0x64, 0xCA, 0x8F, 0x5B, 0x90}}
	iidCompletionHandler         = windows.GUID{Data1: 0x41D949AB, Data2: 0x9862, Data3: 0x444A, Data4: [8]byte{0x80, 0xF6, 0xC2, 0x61, 0x33, 0x4D, 0xA5, 0xEB}}
	iidIAudioClient              = windows.GUID{Data1: 0x1CB9AD4C, Data2: 0xDBFA, Data3: 0x4C32, Data4: [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioCaptureClient       = windows.GUID{Data1: 0xC8ADBD64, Data2: 0xE71E, Data3: 0x48A0, Data4: [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
	virtualProcessLoopbackDevice = `VAD\Process_Loopback`
)

const (
	sOK          = 0
	eNoInterface = 0x80004002

	activationProcessLoopback = 1
	includeProcessTree        = 0
	vtBlob                    = 65

	streamLoopback      = 0x00020000
	streamEventCallback = 0x00040000
	streamAutoConvert   = 0x80000000
	streamSRCQuality    = 0x08000000
	bufferSilent        = 0x2

	// Method slots, after IUnknown's three.
	opGetActivateResult      = 3
	clientInitialize         = 3
	clientStart              = 10
	clientStop               = 11
	clientSetEventHandle     = 13
	clientGetService         = 14
	captureGetBuffer         = 3
	captureReleaseBuffer     = 4
	captureGetNextPacketSize = 5
)

type activationParams struct {
	activationType uint32
	targetPID      uint32
	mode           uint32
}

// propVariantBlob is a PROPVARIANT holding a BLOB.
type propVariantBlob struct {
	vt       uint16
	reserved [3]uint16
	size     uint32
	data     unsafe.Pointer
}

type waveFormat struct {
	formatTag      uint16
	channels       uint16
	samplesPerSec  uint32
	avgBytesPerSec uint32
	blockAlign     uint16
	bitsPerSample  uint16
	extraSize      uint16
}

// handler is the COM object Windows calls when the capture is ready. It is
// never freed: there is one per capture and they are tiny.
type handler struct {
	vtable *[4]uintptr
	done   chan struct{}
}

var (
	handlerVtable = [4]uintptr{
		syscall.NewCallback(handlerQueryInterface),
		syscall.NewCallback(func(this uintptr) uintptr { return 1 }), // AddRef
		syscall.NewCallback(func(this uintptr) uintptr { return 1 }), // Release
		syscall.NewCallback(handlerActivateCompleted),
	}
	handlersMu sync.Mutex
	handlers   = map[uintptr]*handler{} // keeps them alive, and found by address
)

func handlerQueryInterface(this uintptr, iid *windows.GUID, out *uintptr) uintptr {
	// Being "agile" (callable from any thread) is required of this handler.
	if *iid == iidIUnknown || *iid == iidCompletionHandler || *iid == iidIAgileObject {
		*out = this
		return sOK
	}
	*out = 0
	return eNoInterface
}

func handlerActivateCompleted(this, operation uintptr) uintptr {
	handlersMu.Lock()
	h := handlers[this]
	handlersMu.Unlock()
	if h != nil {
		close(h.done)
	}
	return sOK
}

// call invokes method number slot of a COM object.
func call(obj unsafe.Pointer, slot int, args ...uintptr) error {
	vtable := *(**[32]uintptr)(obj)
	hr, _, _ := syscall.SyscallN(vtable[slot], append([]uintptr{uintptr(obj)}, args...)...)
	if int32(hr) < 0 {
		return fmt.Errorf("error 0x%08X", uint32(hr))
	}
	return nil
}

func release(obj unsafe.Pointer) {
	if obj != nil {
		vtable := *(**[32]uintptr)(obj)
		syscall.SyscallN(vtable[2], uintptr(obj))
	}
}

// Capture is the sound an application is playing.
type Capture struct {
	chunks chan []byte
	quit   chan struct{}
	done   chan struct{}
	once   sync.Once
	rest   []byte
}

// Open starts capturing what the process with this ID plays, along with any
// processes it started (a browser's or a DJ program's helpers).
//
// Windows doesn't say whether the process exists, or when it exits: the
// capture of a process that isn't there is simply silent.
func Open(pid uint32) (*Capture, error) {
	c := &Capture{chunks: make(chan []byte, 256), quit: make(chan struct{}), done: make(chan struct{})}
	ready := make(chan error, 1)
	go c.run(pid, ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return c, nil
}

// run does all the COM work on one thread, as COM wants.
func (c *Capture) run(pid uint32, ready chan<- error) {
	defer close(c.done)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	procCoInitializeEx.Call(0, 0) // multithreaded
	defer procCoUninitialize.Call()

	fail := func(what string, err error) {
		ready <- fmt.Errorf("capturing the application's sound: %s: %w", what, err)
	}
	if err := procActivateAudioInterface.Find(); err != nil {
		fail("this Windows is too old", err)
		return
	}

	params := activationParams{activationType: activationProcessLoopback, targetPID: pid, mode: includeProcessTree}
	variant := propVariantBlob{vt: vtBlob, size: uint32(unsafe.Sizeof(params)), data: unsafe.Pointer(&params)}
	h := &handler{vtable: &handlerVtable, done: make(chan struct{})}
	hAddr := uintptr(unsafe.Pointer(h))
	handlersMu.Lock()
	handlers[hAddr] = h
	handlersMu.Unlock()

	device, _ := windows.UTF16PtrFromString(virtualProcessLoopbackDevice)
	var operation unsafe.Pointer
	hr, _, _ := procActivateAudioInterface.Call(uintptr(unsafe.Pointer(device)), uintptr(unsafe.Pointer(&iidIAudioClient)),
		uintptr(unsafe.Pointer(&variant)), hAddr, uintptr(unsafe.Pointer(&operation)))
	if int32(hr) < 0 {
		fail("asking Windows for it", fmt.Errorf("error 0x%08X", uint32(hr)))
		return
	}
	defer release(operation)
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		fail("asking Windows for it", errors.New("no answer"))
		return
	}
	runtime.KeepAlive(&params)

	var result uint32
	var client unsafe.Pointer
	if err := call(operation, opGetActivateResult, uintptr(unsafe.Pointer(&result)), uintptr(unsafe.Pointer(&client))); err != nil {
		fail("reading Windows's answer", err)
		return
	}
	if int32(result) < 0 {
		fail("Windows refused", fmt.Errorf("error 0x%08X", result))
		return
	}
	defer release(client)

	format := waveFormat{formatTag: 1, channels: Channels, samplesPerSec: Rate, avgBytesPerSec: Rate * FrameBytes, blockAlign: FrameBytes, bitsPerSample: 16}
	flags := uintptr(streamLoopback | streamEventCallback | streamAutoConvert | streamSRCQuality)
	if err := call(client, clientInitialize, 0 /* shared */, flags, 0, 0, uintptr(unsafe.Pointer(&format)), 0); err != nil {
		fail("setting the format", err)
		return
	}
	event, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		fail("creating an event", err)
		return
	}
	defer windows.CloseHandle(event)
	if err := call(client, clientSetEventHandle, uintptr(event)); err != nil {
		fail("setting the event", err)
		return
	}
	var capture unsafe.Pointer
	if err := call(client, clientGetService, uintptr(unsafe.Pointer(&iidIAudioCaptureClient)), uintptr(unsafe.Pointer(&capture))); err != nil {
		fail("getting the capture service", err)
		return
	}
	defer release(capture)
	if err := call(client, clientStart); err != nil {
		fail("starting", err)
		return
	}
	defer call(client, clientStop)
	ready <- nil

	for {
		select {
		case <-c.quit:
			return
		default:
		}
		// Woken when audio is ready; the timeout is only to notice quit.
		if s, _ := windows.WaitForSingleObject(event, 100); s != windows.WAIT_OBJECT_0 {
			continue
		}
		for {
			var frames uint32
			if err := call(capture, captureGetNextPacketSize, uintptr(unsafe.Pointer(&frames))); err != nil || frames == 0 {
				break
			}
			var data unsafe.Pointer
			var bufFlags uint32
			if err := call(capture, captureGetBuffer, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&frames)), uintptr(unsafe.Pointer(&bufFlags)), 0, 0); err != nil {
				break
			}
			chunk := make([]byte, int(frames)*FrameBytes)
			if bufFlags&bufferSilent == 0 && data != nil {
				copy(chunk, unsafe.Slice((*byte)(data), len(chunk)))
			}
			call(capture, captureReleaseBuffer, uintptr(frames))
			select {
			case c.chunks <- chunk:
			default: // nobody is reading; drop rather than fall behind
			}
		}
	}
}

// Read returns captured audio, one piece (10 ms) as Windows delivered it.
// If nothing arrives within the timeout it returns 0, which is not an
// error. On the PC this was written on, Windows keeps delivering while the
// application is silent, and even for a process that doesn't exist: all
// but silence, never a gap or an error. Don't count on either.
func (c *Capture) Read(p []byte, timeout time.Duration) (int, error) {
	if len(c.rest) == 0 {
		select {
		case c.rest = <-c.chunks:
		case <-c.done:
			return 0, errors.New("the capture has ended")
		case <-time.After(timeout):
			return 0, nil
		}
	}
	n := copy(p, c.rest)
	c.rest = c.rest[n:]
	return n, nil
}

// Close stops capturing.
func (c *Capture) Close() {
	c.once.Do(func() { close(c.quit) })
	<-c.done
}
