// Package store persists saved streams and player settings as a JSON file in
// the user's config directory.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"partyline/internal/vrcdn"
)

// AppDir is the folder name used under the OS config directory.
const AppDir = "partyline"

// Stream is one saved connection.
type Stream struct {
	ID string `json:"id"`
	// Label is what the VJ sees in the list, e.g. the performer's name.
	Label string `json:"label"`
	// Source is a VRCDN stream name, or a full URL for non-VRCDN streams.
	// Empty for a private stream, whose name is a secret and lives in Key.
	Source    string          `json:"source"`
	Transport vrcdn.Transport `json:"transport"`
	// Private marks a VRCDN private stream. Its name doubles as the key
	// that lets anyone send to it, so it is never stored or shown in the
	// clear.
	Private bool `json:"private,omitempty"`
	// Key is a private stream's name in protected form (see package
	// secret). It never leaves the backend.
	Key string `json:"key,omitempty"`
}

// Settings are the player preferences.
type Settings struct {
	// AudioDevice is the mpv audio device name; "auto" follows the OS default.
	AudioDevice string `json:"audioDevice"`
	// Volume is 0-100.
	Volume float64 `json:"volume"`
	// AutoReconnect keeps retrying while a stream is offline or drops.
	AutoReconnect bool `json:"autoReconnect"`
	// AutoCatchUp plays slightly fast to drain latency that builds up after
	// network hiccups.
	AutoCatchUp bool `json:"autoCatchUp"`
	// CatchUpAboveMs is the buffered amount that triggers catch-up.
	CatchUpAboveMs int `json:"catchUpAboveMs"`
}

// DefaultSettings returns the settings used on first run.
func DefaultSettings() Settings {
	return Settings{
		AudioDevice:    "auto",
		Volume:         100,
		AutoReconnect:  true,
		AutoCatchUp:    true,
		CatchUpAboveMs: 250,
	}
}

func (s *Settings) normalize() {
	if s.AudioDevice == "" {
		s.AudioDevice = "auto"
	}
	s.Volume = min(max(s.Volume, 0), 100)
	if s.CatchUpAboveMs <= 0 {
		s.CatchUpAboveMs = DefaultSettings().CatchUpAboveMs
	}
	s.CatchUpAboveMs = min(max(s.CatchUpAboveMs, 100), 5000)
}

type file struct {
	Version  int      `json:"version"`
	Streams  []Stream `json:"streams"`
	Settings Settings `json:"settings"`
	Send     Send     `json:"send"`
	// SendTargets are the saved places to send to.
	SendTargets []SendTarget `json:"sendTargets"`
}

// SendTarget is a saved destination for the sender: a stream key and the
// server to send it to, under a name.
type SendTarget struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Ingest is the RTMP address to send to; "" means the default.
	Ingest string `json:"ingest"`
	// Key is the stream key in protected form (see package secret). It
	// never leaves the backend.
	Key string `json:"key,omitempty"`
}

// Send holds the sender's settings.
type Send struct {
	// Ingest is the RTMP address to send to; "" means the default.
	Ingest string `json:"ingest"`
	// Device is the ID of the input last used.
	Device string `json:"device"`
	// BitrateKbps is the AAC bitrate; 0 means the default.
	BitrateKbps int `json:"bitrateKbps"`
	// TargetID is the send target last chosen.
	TargetID string `json:"targetId"`
	// Key is where the single stream key lived before there were send
	// targets. Open turns it into a target; nothing writes it any more.
	Key string `json:"key,omitempty"`
}

// Store is a concurrency-safe, file-backed config.
type Store struct {
	mu   sync.Mutex
	path string
	data file
}

// DefaultPath returns the config file location for this OS.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, AppDir, "config.json")
	adoptLegacyConfig(path, filepath.Join(dir, legacyAppDir, "config.json"))
	return path, nil
}

// legacyAppDir is the config folder used before the app was renamed.
const legacyAppDir = "vrcdn-lowlatency-player"

// adoptLegacyConfig copies the config from the old folder the first time the
// renamed app runs, so saved streams survive the rename. The old file is
// left where it is.
func adoptLegacyConfig(path, legacy string) {
	if _, err := os.Stat(path); err == nil {
		return
	}
	data, err := os.ReadFile(legacy)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
		_ = os.WriteFile(path, data, 0o644)
	}
}

// version is the config format written by this build. 2 added UDP-first RTSP.
const version = 2

// Open loads the config at path, or starts from defaults if it doesn't exist.
// A corrupt file is preserved next to the original as *.bad rather than
// silently discarded.
func Open(path string) (*Store, error) {
	s := &Store{path: path, data: file{Version: version, Settings: DefaultSettings()}}

	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return s, err
	}

	loaded := file{Settings: DefaultSettings()}
	if err := json.Unmarshal(raw, &loaded); err != nil {
		_ = os.Rename(path, path+".bad")
		return s, fmt.Errorf("config was unreadable and has been moved to %s: %w", path+".bad", err)
	}
	for i := range loaded.Streams {
		t := &loaded.Streams[i].Transport
		// Version 1 had no UDP-first option and TCP was simply the default,
		// so it is carried over as the new default, not as a choice.
		if !t.Valid() || (loaded.Version < 2 && *t == vrcdn.RTSPTCP) {
			*t = vrcdn.RTSP
		}
	}
	// Before send targets there was one key and one server; keep them.
	if loaded.Send.Key != "" {
		if len(loaded.SendTargets) == 0 {
			t := SendTarget{ID: newID(), Label: "My stream", Ingest: loaded.Send.Ingest, Key: loaded.Send.Key}
			loaded.SendTargets = append(loaded.SendTargets, t)
			loaded.Send.TargetID = t.ID
		}
		loaded.Send.Key = ""
	}
	loaded.Version = version
	loaded.Settings.normalize()
	s.data = loaded
	return s, nil
}

// Path returns where the config is stored.
func (s *Store) Path() string { return s.path }

// Streams returns a copy of the saved streams in display order.
func (s *Store) Streams() []Stream {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Stream{}, s.data.Streams...)
}

// Stream looks up one saved stream.
func (s *Store) Stream(id string) (Stream, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.data.Streams {
		if st.ID == id {
			return st, true
		}
	}
	return Stream{}, false
}

// SaveStream validates and stores a stream. An empty ID creates a new entry;
// otherwise the existing entry is updated in place.
func (s *Store) SaveStream(in Stream) (Stream, error) {
	in.Label = strings.TrimSpace(in.Label)
	if in.Private {
		if in.Key == "" {
			return Stream{}, errors.New("enter the private stream key")
		}
		in.Source = ""
		if in.Label == "" {
			in.Label = "Private stream"
		}
	} else {
		src, err := vrcdn.Parse(in.Source)
		if err != nil {
			return Stream{}, err
		}
		in.Source, in.Key = src.Canonical(), ""
		if in.Label == "" {
			in.Label = src.Display()
		}
	}
	if !in.Transport.Valid() {
		in.Transport = vrcdn.RTSP
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if in.ID == "" {
		in.ID = newID()
		s.data.Streams = append(s.data.Streams, in)
		return in, s.saveLocked()
	}
	for i := range s.data.Streams {
		if s.data.Streams[i].ID == in.ID {
			s.data.Streams[i] = in
			return in, s.saveLocked()
		}
	}
	return Stream{}, fmt.Errorf("stream %q no longer exists", in.ID)
}

// DeleteStream removes a saved stream. Deleting an unknown ID is a no-op.
func (s *Store) DeleteStream(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, st := range s.data.Streams {
		if st.ID == id {
			s.data.Streams = append(s.data.Streams[:i], s.data.Streams[i+1:]...)
			return s.saveLocked()
		}
	}
	return nil
}

// MoveStream moves a stream to a new position in the list.
func (s *Store) MoveStream(id string, to int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	from := -1
	for i, st := range s.data.Streams {
		if st.ID == id {
			from = i
			break
		}
	}
	if from < 0 {
		return fmt.Errorf("stream %q no longer exists", id)
	}
	to = min(max(to, 0), len(s.data.Streams)-1)
	if from == to {
		return nil
	}
	st := s.data.Streams[from]
	rest := append(s.data.Streams[:from:from], s.data.Streams[from+1:]...)
	out := make([]Stream, 0, len(rest)+1)
	out = append(out, rest[:to]...)
	out = append(out, st)
	out = append(out, rest[to:]...)
	s.data.Streams = out
	return s.saveLocked()
}

// Settings returns the current settings.
func (s *Store) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Settings
}

// SetSettings replaces the settings.
func (s *Store) SetSettings(in Settings) (Settings, error) {
	in.normalize()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Settings = in
	return in, s.saveLocked()
}

// saveLocked writes the config atomically (temp file + rename) so a crash
// mid-write can't leave a truncated config.
// Send returns the sender's settings.
func (s *Store) Send() Send {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Send
}

// SetSend saves the sender's settings.
func (s *Store) SetSend(in Send) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Send = in
	return s.saveLocked()
}

// SendTargets returns the saved send targets in display order.
func (s *Store) SendTargets() []SendTarget {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SendTarget{}, s.data.SendTargets...)
}

// SendTarget returns one send target.
func (s *Store) SendTarget(id string) (SendTarget, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.data.SendTargets {
		if t.ID == id {
			return t, true
		}
	}
	return SendTarget{}, false
}

// SaveSendTarget creates (empty ID) or updates a send target. On an update,
// an empty Key keeps the saved one.
func (s *Store) SaveSendTarget(in SendTarget) (SendTarget, error) {
	in.Label = strings.TrimSpace(in.Label)
	if in.Label == "" {
		in.Label = "My stream"
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if in.ID == "" {
		if in.Key == "" {
			return SendTarget{}, errors.New("enter the stream key")
		}
		in.ID = newID()
		s.data.SendTargets = append(s.data.SendTargets, in)
		return in, s.saveLocked()
	}
	for i := range s.data.SendTargets {
		if s.data.SendTargets[i].ID == in.ID {
			if in.Key == "" {
				in.Key = s.data.SendTargets[i].Key
			}
			s.data.SendTargets[i] = in
			return in, s.saveLocked()
		}
	}
	return SendTarget{}, fmt.Errorf("send target %q no longer exists", in.ID)
}

// DeleteSendTarget removes a send target.
func (s *Store) DeleteSendTarget(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.SendTargets {
		if s.data.SendTargets[i].ID == id {
			s.data.SendTargets = append(s.data.SendTargets[:i], s.data.SendTargets[i+1:]...)
			if s.data.Send.TargetID == id {
				s.data.Send.TargetID = ""
			}
			return s.saveLocked()
		}
	}
	return nil
}

func (s *Store) saveLocked() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func newID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
