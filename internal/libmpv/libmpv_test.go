package libmpv

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"unsafe"
)

func cstr(s string) unsafe.Pointer {
	b := append([]byte(s), 0)
	return unsafe.Pointer(&b[0])
}

// pinned keeps test-built C structures reachable. A node stores its pointer
// in a uint64, which the garbage collector (rightly, for C memory) ignores.
var pinned []unsafe.Pointer

func ptrNode(format int32, p unsafe.Pointer) cNode {
	pinned = append(pinned, p)
	// A plain integer store, as C would do it. (Writing through a
	// *unsafe.Pointer alias of the uint64 is not something the compiler
	// promises to honour.)
	return cNode{u: uint64(uintptr(p)), format: format}
}

// Builds the mpv_node tree mpv returns for audio-device-list and for
// loadfile's result, and checks it decodes to plain Go values.
func TestNodeToGo(t *testing.T) {
	dev := []cNode{
		ptrNode(formatString, cstr("wasapi/{abc}")),
		ptrNode(formatString, cstr("Speakers")),
	}
	devKeys := []unsafe.Pointer{cstr("name"), cstr("description")}
	devList := cNodeList{num: 2, values: unsafe.Pointer(&dev[0]), keys: unsafe.Pointer(&devKeys[0])}

	items := []cNode{
		ptrNode(formatNodeMap, unsafe.Pointer(&devList)),
		{u: 7, format: formatInt64},
		{u: math.Float64bits(1.5), format: formatDouble},
		{u: 1, format: formatFlag},
		{format: formatNone},
	}
	arr := cNodeList{num: int32(len(items)), values: unsafe.Pointer(&items[0])}
	root := ptrNode(formatNodeArray, unsafe.Pointer(&arr))

	got := nodeToGo(&root)
	want := []any{
		map[string]any{"name": "wasapi/{abc}", "description": "Speakers"},
		int64(7),
		1.5,
		true,
		nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("nodeToGo = %#v\nwant %#v", got, want)
	}

	empty := ptrNode(formatNodeArray, nil)
	if got := nodeToGo(&empty); !reflect.DeepEqual(got, []any{}) {
		t.Errorf("empty array = %#v", got)
	}
}

func TestStructLayoutsMatchC(t *testing.T) {
	// Sizes on 64-bit targets per mpv's client.h.
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("layout assertions are for 64-bit targets")
	}
	sizes := map[string][2]uintptr{
		"mpv_event":             {unsafe.Sizeof(cEvent{}), 24},
		"mpv_node":              {unsafe.Sizeof(cNode{}), 16},
		"mpv_node_list":         {unsafe.Sizeof(cNodeList{}), 24},
		"mpv_event_property":    {unsafe.Sizeof(cEventProperty{}), 24},
		"mpv_event_log_message": {unsafe.Sizeof(cEventLogMessage{}), 24},
	}
	for name, s := range sizes {
		if s[0] != s[1] {
			t.Errorf("%s is %d bytes, want %d", name, s[0], s[1])
		}
	}
	if off := unsafe.Offsetof(cEventEndFile{}.entryID); off != 8 {
		t.Errorf("end_file.playlist_entry_id offset = %d, want 8", off)
	}
	if off := unsafe.Offsetof(cEventProperty{}.data); off != 16 {
		t.Errorf("event_property.data offset = %d, want 16", off)
	}
}

func TestCArgv(t *testing.T) {
	argv, keep := cArgv([]string{"loadfile", "rtsp://x/y", "replace"})
	if len(argv) != 4 || argv[3] != nil {
		t.Fatalf("argv not NULL-terminated: %v", argv)
	}
	for i, want := range []string{"loadfile", "rtsp://x/y", "replace"} {
		if got := goString(unsafe.Pointer(argv[i])); got != want {
			t.Errorf("argv[%d] = %q, want %q", i, got, want)
		}
	}
	if len(keep) != 3 {
		t.Errorf("keep = %d entries", len(keep))
	}
}

func TestGoString(t *testing.T) {
	if goString(nil) != "" {
		t.Error("nil should be empty")
	}
	if got := goString(cstr("héllo")); got != "héllo" {
		t.Errorf("got %q", got)
	}
}

// A missing library must be an ordinary error, never a panic, so the UI can
// explain what to install.
func TestLoadFailsGracefully(t *testing.T) {
	err := Load()
	if err == nil {
		t.Skipf("libmpv is installed here (%s)", LibraryPath())
	}
	var nf *NotFoundError
	if !errors.As(err, &nf) || len(nf.Tried) == 0 {
		t.Fatalf("err = %v, want *NotFoundError listing what was tried", err)
	}
	if _, newErr := New(); newErr == nil {
		t.Error("New should fail when libmpv is missing")
	}
}
