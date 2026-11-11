// Package libmpv is a small, cgo-free binding to the parts of libmpv's client
// API this player needs.
//
// The shared library is loaded lazily by Load rather than at program start,
// so a missing libmpv is an ordinary error the UI can explain instead of a
// crash before the window opens. It also means the binary builds (and Wails
// can generate its bindings) on machines that have no libmpv installed.
package libmpv

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// mpv_format values.
const (
	formatNone      = 0
	formatString    = 1
	formatFlag      = 3
	formatInt64     = 4
	formatDouble    = 5
	formatNode      = 6
	formatNodeArray = 7
	formatNodeMap   = 8
)

// EventID is an mpv_event_id.
type EventID int32

// The events this player cares about.
const (
	EventNone            EventID = 0
	EventShutdown        EventID = 1
	EventLogMessage      EventID = 2
	EventStartFile       EventID = 6
	EventEndFile         EventID = 7
	EventFileLoaded      EventID = 8
	EventPlaybackRestart EventID = 21
	EventPropertyChange  EventID = 22
)

// EndReason is an mpv_end_file_reason.
type EndReason int32

// End-file reasons.
const (
	EndEOF      EndReason = 0
	EndStop     EndReason = 2
	EndQuit     EndReason = 3
	EndError    EndReason = 4
	EndRedirect EndReason = 5
)

// Event is a decoded mpv_event. Only the fields relevant to ID are set.
type Event struct {
	ID            EventID
	ReplyUserdata uint64

	// EventStartFile, EventEndFile
	EntryID int64
	// EventEndFile
	Reason EndReason
	EndErr string

	// EventLogMessage
	LogPrefix, LogLevel, LogText string

	// EventPropertyChange
	Property string
}

// C struct mirrors. Layouts follow mpv's client.h.
type (
	cEvent struct {
		id            int32
		err           int32
		replyUserdata uint64
		data          unsafe.Pointer
	}
	cEventStartFile struct {
		entryID int64
	}
	cEventEndFile struct {
		reason  int32
		err     int32
		entryID int64
	}
	cEventLogMessage struct {
		prefix, level, text unsafe.Pointer
	}
	cEventProperty struct {
		name   unsafe.Pointer
		format int32
		data   unsafe.Pointer
	}
	cNode struct {
		u      uint64 // union: value, or a pointer in its low bits
		format int32
	}
	cNodeList struct {
		num    int32
		values unsafe.Pointer // *cNode
		keys   unsafe.Pointer // **char
	}
)

var (
	loadOnce sync.Once
	loadErr  error
	loadedAs string

	mpvCreate             func() uintptr
	mpvInitialize         func(h uintptr) int32
	mpvTerminateDestroy   func(h uintptr)
	mpvSetOptionString    func(h uintptr, name, value string) int32
	mpvSetPropertyString  func(h uintptr, name, value string) int32
	mpvGetProperty        func(h uintptr, name string, format int32, data unsafe.Pointer) int32
	mpvCommand            func(h uintptr, args **byte) int32
	mpvCommandRet         func(h uintptr, args **byte, result unsafe.Pointer) int32
	mpvFreeNodeContents   func(node unsafe.Pointer)
	mpvFree               func(p unsafe.Pointer)
	mpvObserveProperty    func(h uintptr, replyUserdata uint64, name string, format int32) int32
	mpvRequestLogMessages func(h uintptr, level string) int32
	mpvWaitEvent          func(h uintptr, timeout float64) *cEvent
	mpvWakeup             func(h uintptr)
	mpvErrorString        func(code int32) string
)

// Load locates and loads libmpv. It is safe to call repeatedly; the result of
// the first attempt is remembered.
func Load() error {
	loadOnce.Do(func() { loadErr = load() })
	return loadErr
}

// LibraryPath returns the name or path libmpv was loaded from.
func LibraryPath() string { return loadedAs }

func load() (err error) {
	// RegisterLibFunc panics on a missing symbol; report that as an error.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("libmpv at %s is not usable: %v", loadedAs, r)
		}
	}()

	var tried []string
	var handle uintptr
	open := func(names []string) bool {
		for _, name := range names {
			if h, openErr := openLibrary(name); openErr == nil {
				handle, loadedAs = h, name
				return true
			}
			tried = append(tried, name)
		}
		return false
	}

	// A copy the user put in place wins over the built-in one, which wins
	// over whatever the system has.
	var bundleErr error
	if !open(explicitCandidates()) {
		var bundled string
		if bundled, bundleErr = unpackBundled(); bundled == "" || !open([]string{bundled}) {
			open(systemCandidates())
		}
	}
	if handle == 0 {
		return &NotFoundError{Tried: tried, BundleErr: bundleErr}
	}

	purego.RegisterLibFunc(&mpvCreate, handle, "mpv_create")
	purego.RegisterLibFunc(&mpvInitialize, handle, "mpv_initialize")
	purego.RegisterLibFunc(&mpvTerminateDestroy, handle, "mpv_terminate_destroy")
	purego.RegisterLibFunc(&mpvSetOptionString, handle, "mpv_set_option_string")
	purego.RegisterLibFunc(&mpvSetPropertyString, handle, "mpv_set_property_string")
	purego.RegisterLibFunc(&mpvGetProperty, handle, "mpv_get_property")
	purego.RegisterLibFunc(&mpvCommand, handle, "mpv_command")
	purego.RegisterLibFunc(&mpvCommandRet, handle, "mpv_command_ret")
	purego.RegisterLibFunc(&mpvFreeNodeContents, handle, "mpv_free_node_contents")
	purego.RegisterLibFunc(&mpvFree, handle, "mpv_free")
	purego.RegisterLibFunc(&mpvObserveProperty, handle, "mpv_observe_property")
	purego.RegisterLibFunc(&mpvRequestLogMessages, handle, "mpv_request_log_messages")
	purego.RegisterLibFunc(&mpvWaitEvent, handle, "mpv_wait_event")
	purego.RegisterLibFunc(&mpvWakeup, handle, "mpv_wakeup")
	purego.RegisterLibFunc(&mpvErrorString, handle, "mpv_error_string")

	prepareProcess(handle)
	return nil
}

// NotFoundError means no libmpv shared library could be loaded.
type NotFoundError struct {
	Tried []string
	// BundleErr is why the copy built into the program could not be used,
	// if there is one and it failed.
	BundleErr error
}

// Error names the library files that were looked for; Tried has the full
// list of paths.
func (e *NotFoundError) Error() string {
	var names []string
	seen := map[string]bool{}
	for _, p := range e.Tried {
		base := p[strings.LastIndexAny(p, `/\`)+1:]
		if !seen[base] {
			seen[base] = true
			names = append(names, base)
		}
	}
	msg := "libmpv was not found (looked for " + strings.Join(names, ", ") +
		" next to the program and on the system library path)"
	if e.BundleErr != nil {
		msg += "; the built-in copy could not be used: " + e.BundleErr.Error()
	}
	return msg
}

// Error is an mpv API error code.
type Error struct {
	Code int32
	Op   string
}

func (e *Error) Error() string {
	return fmt.Sprintf("mpv: %s: %s", e.Op, mpvErrorString(e.Code))
}

func check(code int32, op string) error {
	if code < 0 {
		return &Error{Code: code, Op: op}
	}
	return nil
}

// Client is an mpv core plus its client handle.
type Client struct {
	h uintptr
}

// New creates an uninitialized mpv instance. Set options, then call
// Initialize.
func New() (*Client, error) {
	if err := Load(); err != nil {
		return nil, err
	}
	// GUI toolkits switch the process locale; mpv refuses to start unless
	// LC_NUMERIC is "C".
	resetNumericLocale()
	h := mpvCreate()
	if h == 0 {
		return nil, errors.New("mpv: could not create player instance")
	}
	return &Client{h: h}, nil
}

// SetOption sets an option. Before Initialize this is the only way to
// configure the core.
func (c *Client) SetOption(name, value string) error {
	return check(mpvSetOptionString(c.h, name, value), "set option "+name)
}

// Initialize starts the core.
func (c *Client) Initialize() error {
	return check(mpvInitialize(c.h), "initialize")
}

// Destroy shuts the core down and frees the handle. No other call may be in
// flight on this client, including WaitEvent.
func (c *Client) Destroy() {
	if c.h != 0 {
		mpvTerminateDestroy(c.h)
		c.h = 0
	}
}

// SetProperty sets a property from its string form.
func (c *Client) SetProperty(name, value string) error {
	return check(mpvSetPropertyString(c.h, name, value), "set "+name)
}

// GetDouble reads a numeric property.
func (c *Client) GetDouble(name string) (float64, error) {
	var out float64
	if err := check(mpvGetProperty(c.h, name, formatDouble, unsafe.Pointer(&out)), "get "+name); err != nil {
		return 0, err
	}
	return out, nil
}

// GetString reads a property in its string form.
func (c *Client) GetString(name string) (string, error) {
	var out unsafe.Pointer
	if err := check(mpvGetProperty(c.h, name, formatString, unsafe.Pointer(&out)), "get "+name); err != nil {
		return "", err
	}
	defer mpvFree(out)
	return goString(out), nil
}

// GetNode reads a structured property as nested Go values: map[string]any,
// []any, string, bool, int64 or float64.
func (c *Client) GetNode(name string) (any, error) {
	var node cNode
	if err := check(mpvGetProperty(c.h, name, formatNode, unsafe.Pointer(&node)), "get "+name); err != nil {
		return nil, err
	}
	defer mpvFreeNodeContents(unsafe.Pointer(&node))
	return nodeToGo(&node), nil
}

// Command runs an mpv input command, e.g. Command("loadfile", url, "replace").
func (c *Client) Command(args ...string) error {
	argv, keep := cArgv(args)
	code := mpvCommand(c.h, unsafe.SliceData(argv))
	runtime.KeepAlive(keep)
	runtime.KeepAlive(argv)
	return check(code, args[0])
}

// CommandRet runs a command and returns its result decoded like GetNode.
func (c *Client) CommandRet(args ...string) (any, error) {
	argv, keep := cArgv(args)
	var node cNode
	code := mpvCommandRet(c.h, unsafe.SliceData(argv), unsafe.Pointer(&node))
	runtime.KeepAlive(keep)
	runtime.KeepAlive(argv)
	if err := check(code, args[0]); err != nil {
		return nil, err
	}
	defer mpvFreeNodeContents(unsafe.Pointer(&node))
	return nodeToGo(&node), nil
}

// ObserveProperty asks for an EventPropertyChange whenever the property
// changes. The value itself is not delivered; read it when notified.
func (c *Client) ObserveProperty(replyUserdata uint64, name string) error {
	return check(mpvObserveProperty(c.h, replyUserdata, name, formatNone), "observe "+name)
}

// RequestLogMessages subscribes to log output at the given level or above
// ("error", "warn", "info", "v", ... or "no").
func (c *Client) RequestLogMessages(level string) error {
	return check(mpvRequestLogMessages(c.h, level), "request log messages")
}

// WaitEvent blocks for up to timeout seconds for the next event. On timeout
// the returned event has ID EventNone.
func (c *Client) WaitEvent(timeout float64) Event {
	ce := mpvWaitEvent(c.h, timeout)
	ev := Event{ID: EventID(ce.id), ReplyUserdata: ce.replyUserdata}
	if ce.data == nil {
		return ev
	}
	// ce.data is only valid until the next WaitEvent, so copy out now.
	switch ev.ID {
	case EventStartFile:
		ev.EntryID = (*cEventStartFile)(ce.data).entryID
	case EventEndFile:
		d := (*cEventEndFile)(ce.data)
		ev.EntryID = d.entryID
		ev.Reason = EndReason(d.reason)
		if d.err < 0 {
			ev.EndErr = mpvErrorString(d.err)
		}
	case EventLogMessage:
		d := (*cEventLogMessage)(ce.data)
		ev.LogPrefix = goString(d.prefix)
		ev.LogLevel = goString(d.level)
		ev.LogText = strings.TrimRight(goString(d.text), "\r\n")
	case EventPropertyChange:
		ev.Property = goString((*cEventProperty)(ce.data).name)
	}
	return ev
}

// Wakeup makes a blocked WaitEvent return immediately.
func (c *Client) Wakeup() {
	mpvWakeup(c.h)
}

// cArgv builds a NULL-terminated char*[] for mpv_command. The second return
// value owns the string storage and must be kept alive across the call.
func cArgv(args []string) ([]*byte, [][]byte) {
	keep := make([][]byte, len(args))
	argv := make([]*byte, len(args)+1)
	for i, a := range args {
		keep[i] = append([]byte(a), 0)
		argv[i] = &keep[i][0]
	}
	return argv, keep
}

// goString copies a NUL-terminated C string.
func goString(p unsafe.Pointer) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(p, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(p), n))
}

// nodePtr reads the node's union as a pointer.
func nodePtr(n *cNode) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&n.u))
}

func nodeToGo(n *cNode) any {
	switch n.format {
	case formatString:
		return goString(nodePtr(n))
	case formatFlag:
		return uint32(n.u) != 0
	case formatInt64:
		return int64(n.u)
	case formatDouble:
		return math.Float64frombits(n.u)
	case formatNodeArray:
		list := (*cNodeList)(nodePtr(n))
		if list == nil {
			return []any{}
		}
		out := make([]any, list.num)
		if list.values != nil {
			nodes := unsafe.Slice((*cNode)(list.values), int(list.num))
			for i := range nodes {
				out[i] = nodeToGo(&nodes[i])
			}
		}
		return out
	case formatNodeMap:
		list := (*cNodeList)(nodePtr(n))
		out := map[string]any{}
		if list == nil || list.values == nil || list.keys == nil {
			return out
		}
		nodes := unsafe.Slice((*cNode)(list.values), int(list.num))
		keys := unsafe.Slice((*unsafe.Pointer)(list.keys), int(list.num))
		for i := range nodes {
			out[goString(keys[i])] = nodeToGo(&nodes[i])
		}
		return out
	}
	return nil
}
