package loopback

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Program is a program that can be captured.
type Program struct {
	// Exe is its file name, e.g. "rekordbox.exe": what it is known by from
	// one run to the next.
	Exe string
	// Name is what it calls itself, e.g. "rekordbox".
	Name string
	// PID is the process to capture: the program's first process, so that
	// its helpers are included.
	PID uint32
	// Playing is whether it is making sound right now.
	Playing bool
}

var (
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")

	clsidDeviceEnumerator = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidDeviceEnumerator   = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidSessionManager2    = windows.GUID{Data1: 0x77AA99A0, Data2: 0x1BD6, Data3: 0x484F, Data4: [8]byte{0x8B, 0xC7, 0x2C, 0x65, 0x4C, 0x9A, 0x9B, 0x6F}}
	iidSessionControl2    = windows.GUID{Data1: 0xBFB7FF88, Data2: 0x7239, Data3: 0x4FC9, Data4: [8]byte{0x8F, 0xA2, 0x07, 0xC9, 0x50, 0xBE, 0x9C, 0x6D}}
)

const (
	clsctxAll         = 23
	flowRender        = 0
	deviceStateActive = 1
	sessionActive     = 1
	sessionExpired    = 2

	enumeratorEnumEndpoints = 3
	collectionGetCount      = 3
	collectionItem          = 4
	deviceActivate          = 3
	managerGetSessionEnum   = 5
	sessionEnumGetCount     = 3
	sessionEnumGetSession   = 4
	controlGetState         = 3
	control2GetProcessID    = 14
	control2IsSystemSounds  = 15
)

// Programs lists the programs that have sound open on any output of this
// PC, those playing right now first. This program itself is left out.
func Programs() ([]Program, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	procCoInitializeEx.Call(0, 0)
	defer procCoUninitialize.Call()

	var enumerator unsafe.Pointer
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidDeviceEnumerator)), uintptr(unsafe.Pointer(&enumerator)))
	if int32(hr) < 0 {
		return nil, fmt.Errorf("listing sound outputs: error 0x%08X", uint32(hr))
	}
	defer release(enumerator)
	var outputs unsafe.Pointer
	if err := call(enumerator, enumeratorEnumEndpoints, flowRender, deviceStateActive, uintptr(unsafe.Pointer(&outputs))); err != nil {
		return nil, fmt.Errorf("listing sound outputs: %w", err)
	}
	defer release(outputs)

	// Which processes have sound open, and whether any of it is playing.
	playing := map[uint32]bool{}
	var count uint32
	_ = call(outputs, collectionGetCount, uintptr(unsafe.Pointer(&count)))
	for i := uint32(0); i < count; i++ {
		var device, manager, sessions unsafe.Pointer
		if call(outputs, collectionItem, uintptr(i), uintptr(unsafe.Pointer(&device))) != nil {
			continue
		}
		if call(device, deviceActivate, uintptr(unsafe.Pointer(&iidSessionManager2)), clsctxAll, 0, uintptr(unsafe.Pointer(&manager))) == nil {
			if call(manager, managerGetSessionEnum, uintptr(unsafe.Pointer(&sessions))) == nil {
				var n int32
				_ = call(sessions, sessionEnumGetCount, uintptr(unsafe.Pointer(&n)))
				for j := int32(0); j < n; j++ {
					var control, control2 unsafe.Pointer
					if call(sessions, sessionEnumGetSession, uintptr(j), uintptr(unsafe.Pointer(&control))) != nil {
						continue
					}
					if call(control, 0, uintptr(unsafe.Pointer(&iidSessionControl2)), uintptr(unsafe.Pointer(&control2))) == nil {
						var state, pid uint32
						_ = call(control2, controlGetState, uintptr(unsafe.Pointer(&state)))
						_ = call(control2, control2GetProcessID, uintptr(unsafe.Pointer(&pid)))
						// IsSystemSoundsSession answers 0 for yes, 1 for no.
						vtable := *(**[32]uintptr)(control2)
						system, _, _ := syscall.SyscallN(vtable[control2IsSystemSounds], uintptr(control2))
						if pid != 0 && system != 0 && state != sessionExpired {
							playing[pid] = playing[pid] || state == sessionActive
						}
						release(control2)
					}
					release(control)
				}
				release(sessions)
			}
			release(manager)
		}
		release(device)
	}

	procs := processes()
	self, _ := os.Executable()
	byExe := map[string]*Program{}
	for pid, active := range playing {
		root := procs.root(pid)
		path := imagePath(root)
		if path == "" || strings.EqualFold(path, self) {
			continue
		}
		key := strings.ToLower(filepath.Base(path))
		p := byExe[key]
		if p == nil {
			p = &Program{Exe: filepath.Base(path), Name: description(path), PID: root}
			byExe[key] = p
		}
		p.Playing = p.Playing || active
	}
	out := make([]Program, 0, len(byExe))
	for _, p := range byExe {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Playing != out[j].Playing {
			return out[i].Playing
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Find returns the process to capture for a program by its file name. A
// program that is running but has no sound open yet is found too.
func Find(exe string) (Program, error) {
	programs, _ := Programs()
	for _, p := range programs {
		if strings.EqualFold(p.Exe, exe) {
			return p, nil
		}
	}
	procs := processes()
	self := uint32(os.Getpid())
	for pid, e := range procs {
		if pid != self && strings.EqualFold(e.exe, exe) {
			root := procs.root(pid)
			path := imagePath(root)
			name := exe
			if path != "" {
				name = description(path)
			}
			return Program{Exe: e.exe, Name: name, PID: root}, nil
		}
	}
	return Program{}, fmt.Errorf("%s isn't running", strings.TrimSuffix(exe, filepath.Ext(exe)))
}

// Running reports whether a process is still there.
func Running(pid uint32) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	s, _ := windows.WaitForSingleObject(h, 0)
	return s == uint32(windows.WAIT_TIMEOUT)
}

type process struct {
	exe    string
	parent uint32
}

type processTable map[uint32]process

func processes() processTable {
	out := processTable{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		out[e.ProcessID] = process{exe: windows.UTF16ToString(e.ExeFile[:]), parent: e.ParentProcessID}
	}
	return out
}

// root climbs from a process to the first process of the same program: a
// browser's or a DJ program's helpers are started by it and share its name.
func (t processTable) root(pid uint32) uint32 {
	for i := 0; i < 16; i++ {
		p, ok := t[pid]
		if !ok {
			break
		}
		parent, ok := t[p.parent]
		if !ok || p.parent == pid || !strings.EqualFold(parent.exe, p.exe) {
			break
		}
		pid = p.parent
	}
	return pid
}

func imagePath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

// description is the name a program gives itself in its file details, or
// its file name without ".exe".
func description(path string) string {
	fallback := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	size, err := windows.GetFileVersionInfoSize(path, nil)
	if err != nil || size == 0 {
		return fallback
	}
	info := make([]byte, size)
	if windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&info[0])) != nil {
		return fallback
	}
	var langs unsafe.Pointer
	var n uint32
	if windows.VerQueryValue(unsafe.Pointer(&info[0]), `\VarFileInfo\Translation`, unsafe.Pointer(&langs), &n) != nil || n < 4 {
		return fallback
	}
	lang := *(*[2]uint16)(langs)
	var text unsafe.Pointer
	key := fmt.Sprintf(`\StringFileInfo\%04x%04x\FileDescription`, lang[0], lang[1])
	if windows.VerQueryValue(unsafe.Pointer(&info[0]), key, unsafe.Pointer(&text), &n) != nil || n == 0 {
		return fallback
	}
	if s := strings.TrimSpace(windows.UTF16PtrToString((*uint16)(text))); s != "" {
		return s
	}
	return fallback
}
