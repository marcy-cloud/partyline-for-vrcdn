package store

import (
	"os"
	"path/filepath"
	"strings"
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

func TestSendTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	s, _ := Open(path)

	if _, err := s.SaveSendTarget(SendTarget{Label: "No key"}); err == nil {
		t.Fatal("a new target needs a key")
	}
	a, err := s.SaveSendTarget(SendTarget{Label: " Club X ", Ingest: "rtmp://uk.ingest.vrcdn.live/live", Key: "cHJvdGVjdGVk"})
	if err != nil || a.ID == "" || a.Label != "Club X" {
		t.Fatalf("saved %+v, %v", a, err)
	}
	s.SetSend(Send{Device: "dev-1", BitrateKbps: 256, TargetID: a.ID})

	// Renaming without retyping the key keeps the key.
	renamed, err := s.SaveSendTarget(SendTarget{ID: a.ID, Label: "Club X main"})
	if err != nil || renamed.Key != "cHJvdGVjdGVk" || renamed.Label != "Club X main" {
		t.Fatalf("renamed %+v, %v", renamed, err)
	}

	again, _ := Open(path)
	if got := again.SendTargets(); len(got) != 1 || got[0] != renamed {
		t.Fatalf("reloaded %+v", got)
	}
	if again.Send().TargetID != a.ID || again.Send().Device != "dev-1" {
		t.Errorf("send settings %+v", again.Send())
	}

	again.DeleteSendTarget(a.ID)
	if len(again.SendTargets()) != 0 || again.Send().TargetID != "" {
		t.Errorf("after delete: %+v, selected %q", again.SendTargets(), again.Send().TargetID)
	}
}

// A config from before send targets had one key and one server.
func TestOldSendKeyBecomesATarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	old := `{"version":2,"send":{"ingest":"rtmp://de.ingest.vrcdn.live/live","device":"d","bitrateKbps":320,"key":"cHJvdGVjdGVk"}}`
	os.WriteFile(path, []byte(old), 0o644)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	targets := s.SendTargets()
	if len(targets) != 1 || targets[0].Key != "cHJvdGVjdGVk" || targets[0].Ingest != "rtmp://de.ingest.vrcdn.live/live" {
		t.Fatalf("targets %+v", targets)
	}
	if s.Send().TargetID != targets[0].ID || s.Send().Key != "" {
		t.Errorf("send %+v", s.Send())
	}
}

func TestPrivateStream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	s, _ := Open(path)

	if _, err := s.SaveStream(Stream{Private: true, Source: "would-be-plain"}); err == nil {
		t.Fatal("a private stream without a protected key must be refused")
	}
	saved, err := s.SaveStream(Stream{Private: true, Source: "typed-key-must-not-be-kept", Key: "cHJvdGVjdGVk"})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Source != "" || saved.Key != "cHJvdGVjdGVk" || saved.Label != "Private stream" {
		t.Errorf("saved %+v", saved)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "typed-key-must-not-be-kept") {
		t.Error("the plain key reached the config file")
	}

	// Making it an ordinary stream again drops the key.
	saved.Private, saved.Source = false, "dja"
	public, err := s.SaveStream(saved)
	if err != nil || public.Key != "" || public.Source != "dja" {
		t.Errorf("made public: %+v, %v", public, err)
	}
}

func TestExportImportStreams(t *testing.T) {
	from, _ := open(t)
	for _, st := range []Stream{
		{Label: "Marcy", Source: "marcymarcy"},
		{Label: "Demo", Source: "vrcdn", Transport: vrcdn.MPEGTS},
		{Label: "Secret", Private: true, Key: "protected-key"},
	} {
		if _, err := from.SaveStream(st); err != nil {
			t.Fatal(err)
		}
	}
	data, count, private, err := from.ExportStreams()
	if err != nil || count != 2 || private != 1 {
		t.Fatalf("export: count %d, private %d, err %v", count, private, err)
	}
	if strings.Contains(string(data), "protected-key") || strings.Contains(string(data), "Secret") {
		t.Fatalf("private stream leaked into the list:\n%s", data)
	}

	to, path := open(t)
	if _, err := to.SaveStream(Stream{Label: "My name for her", Source: "MarcyMarcy", Transport: vrcdn.RTSPTCP}); err != nil {
		t.Fatal(err)
	}
	res, err := to.ImportStreams(data)
	if err != nil || res != (Imported{Added: 1, Known: 1}) {
		t.Fatalf("import: %+v, %v", res, err)
	}
	// Importing again changes nothing.
	if res, err = to.ImportStreams(data); err != nil || res != (Imported{Known: 2}) {
		t.Fatalf("second import: %+v, %v", res, err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Streams()
	if len(got) != 2 {
		t.Fatalf("streams after import: %v", ids(got))
	}
	// The stream already saved keeps the listener's own label and connection.
	if got[0].Label != "My name for her" || got[0].Transport != vrcdn.RTSPTCP {
		t.Errorf("existing stream was changed: %+v", got[0])
	}
	if got[1].Label != "Demo" || got[1].Source != "vrcdn" || got[1].Transport != vrcdn.MPEGTS || got[1].ID == "" {
		t.Errorf("imported stream: %+v", got[1])
	}
}

func TestImportStreamsRejectsOtherFiles(t *testing.T) {
	s, _ := open(t)
	for name, data := range map[string]string{
		"not json":      "hello",
		"other json":    `{"streams":[{"label":"x","source":"x"}]}`,
		"the config":    `{"version":2,"streams":[],"settings":{}}`,
		"newer version": `{"app":"partyline","version":99,"streams":[]}`,
	} {
		if _, err := s.ImportStreams([]byte(data)); err == nil {
			t.Errorf("%s: imported without an error", name)
		}
	}
	// Bad entries are counted and skipped; good ones still load, with defaults.
	res, err := s.ImportStreams([]byte(`{"app":"partyline","version":1,"streams":[
		{"label":"","source":""},
		{"label":"  ","source":"somedj","transport":"nonsense"},
		{"label":"Twice","source":"SomeDJ"}]}`))
	if err != nil || res != (Imported{Added: 1, Known: 1, Invalid: 1}) {
		t.Fatalf("import: %+v, %v", res, err)
	}
	got := s.Streams()
	if len(got) != 1 || got[0].Label != "somedj" || got[0].Transport != vrcdn.RTSP {
		t.Errorf("imported: %+v", got)
	}
}
