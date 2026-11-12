package store

import (
	"os"
	"path/filepath"
	"testing"

	"partyline/internal/vrcdn"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func ids(streams []Stream) []string {
	out := make([]string, len(streams))
	for i, s := range streams {
		out[i] = s.Label
	}
	return out
}

func TestDefaultsOnFirstRun(t *testing.T) {
	s, _ := open(t)
	if len(s.Streams()) != 0 {
		t.Error("expected no streams")
	}
	if got := s.Settings(); got != DefaultSettings() {
		t.Errorf("settings = %+v", got)
	}
}

func TestSaveNormalizesAndPersists(t *testing.T) {
	s, path := open(t)

	a, err := s.SaveStream(Stream{Label: "  DJ A ", Source: "https://stream.vrcdn.live/live/dja.live.flv"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.Label != "DJ A" || a.Source != "dja" || a.Transport != vrcdn.RTSP {
		t.Errorf("saved = %+v", a)
	}

	// Blank label falls back to the stream name.
	b, err := s.SaveStream(Stream{Source: "djb", Transport: vrcdn.FLV})
	if err != nil {
		t.Fatal(err)
	}
	if b.Label != "djb" || b.Transport != vrcdn.FLV {
		t.Errorf("saved = %+v", b)
	}

	if _, err := s.SaveStream(Stream{Source: "not valid!"}); err == nil {
		t.Error("expected validation error")
	}

	// Update in place.
	a.Label = "DJ Alpha"
	if _, err := s.SaveStream(a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveStream(Stream{ID: "missing", Source: "x"}); err == nil {
		t.Error("expected error updating unknown id")
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Streams()
	if len(got) != 2 || got[0].Label != "DJ Alpha" || got[1].Source != "djb" {
		t.Errorf("reloaded = %+v", got)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file left behind")
	}
}

func TestDeleteAndMove(t *testing.T) {
	s, _ := open(t)
	var saved []Stream
	for _, n := range []string{"a", "b", "c", "d"} {
		st, err := s.SaveStream(Stream{Source: n})
		if err != nil {
			t.Fatal(err)
		}
		saved = append(saved, st)
	}

	check := func(want ...string) {
		t.Helper()
		got := ids(s.Streams())
		if len(got) != len(want) {
			t.Fatalf("order = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("order = %v, want %v", got, want)
			}
		}
	}

	if err := s.MoveStream(saved[0].ID, 2); err != nil {
		t.Fatal(err)
	}
	check("b", "c", "a", "d")

	if err := s.MoveStream(saved[3].ID, 0); err != nil {
		t.Fatal(err)
	}
	check("d", "b", "c", "a")

	if err := s.MoveStream(saved[1].ID, 99); err != nil {
		t.Fatal(err)
	}
	check("d", "c", "a", "b")

	if err := s.MoveStream("missing", 0); err == nil {
		t.Error("expected error moving unknown id")
	}

	if err := s.DeleteStream(saved[2].ID); err != nil {
		t.Fatal(err)
	}
	check("d", "a", "b")
	if err := s.DeleteStream("missing"); err != nil {
		t.Errorf("deleting unknown id should be a no-op, got %v", err)
	}
}

func TestSettingsClamped(t *testing.T) {
	s, path := open(t)
	got, err := s.SetSettings(Settings{Volume: 250, CatchUpAboveMs: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got.Volume != 100 || got.AudioDevice != "auto" || got.CatchUpAboveMs != 100 {
		t.Errorf("settings = %+v", got)
	}
	reopened, _ := Open(path)
	if reopened.Settings() != got {
		t.Errorf("reloaded = %+v", reopened.Settings())
	}
}

func TestCorruptConfigIsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err == nil {
		t.Fatal("expected error for corrupt config")
	}
	if s == nil || s.Settings() != DefaultSettings() {
		t.Error("store should still be usable with defaults")
	}
	if _, statErr := os.Stat(path + ".bad"); statErr != nil {
		t.Errorf("corrupt file not preserved: %v", statErr)
	}
}

// Version 1 configs stored RTSP over TCP only because it was the default;
// they move to the UDP-first default. A TCP choice made since is kept.
func TestVersion1TCPBecomesUDPFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	v1 := `{"version":1,"streams":[
		{"id":"a","label":"A","source":"dja","transport":"rtsp-tcp"},
		{"id":"b","label":"B","source":"djb","transport":"flv"}]}`
	if err := os.WriteFile(path, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := s.Streams()
	if got[0].Transport != vrcdn.RTSP || got[1].Transport != vrcdn.FLV {
		t.Fatalf("after migration: %q, %q", got[0].Transport, got[1].Transport)
	}

	// Choosing TCP only now is a real choice and must survive a reload.
	pinned := got[0]
	pinned.Transport = vrcdn.RTSPTCP
	if _, err := s.SaveStream(pinned); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if tr := again.Streams()[0].Transport; tr != vrcdn.RTSPTCP {
		t.Errorf("TCP-only choice reloaded as %q", tr)
	}
}

func TestAdoptLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "old", "config.json")
	path := filepath.Join(dir, "new", "config.json")

	adoptLegacyConfig(path, legacy) // nothing to adopt
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a config appeared from nowhere (%v)", err)
	}

	os.MkdirAll(filepath.Dir(legacy), 0o755)
	os.WriteFile(legacy, []byte(`{"version":2,"streams":[{"id":"a","label":"A","source":"dja","transport":"rtsp"}]}`), 0o644)
	adoptLegacyConfig(path, legacy)
	s, err := Open(path)
	if err != nil || len(s.Streams()) != 1 {
		t.Fatalf("legacy streams were not carried over: %v, %v", s.Streams(), err)
	}

	// An existing config is never overwritten.
	os.WriteFile(legacy, []byte(`{"version":2}`), 0o644)
	adoptLegacyConfig(path, legacy)
	if again, _ := Open(path); len(again.Streams()) != 1 {
		t.Error("the new config was overwritten by the legacy one")
	}
}

func TestSendSettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	s, _ := Open(path)
	want := Send{Ingest: "rtmp://uk.ingest.vrcdn.live/live", Device: "dev-1", BitrateKbps: 256, Key: "cHJvdGVjdGVk"}
	if err := s.SetSend(want); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil || again.Send() != want {
		t.Fatalf("reloaded %+v, %v; want %+v", again.Send(), err, want)
	}
}
