package store

import (
	"encoding/json"
	"errors"
	"strings"

	"partyline/internal/vrcdn"
)

// A stream list is a file of saved streams to hand to someone else. It holds
// what a listener types in by hand and nothing more: no settings, no send
// targets, and never a private stream, whose name is a key.

const (
	listApp     = "partyline"
	listVersion = 1
	// maxListSize bounds what Import will read; a list is a few KB.
	maxListSize = 1 << 20
)

type streamList struct {
	App     string         `json:"app"`
	Version int            `json:"version"`
	Streams []sharedStream `json:"streams"`
}

type sharedStream struct {
	Label     string          `json:"label"`
	Source    string          `json:"source"`
	Transport vrcdn.Transport `json:"transport,omitempty"`
}

// ExportStreams returns the saved streams as a stream list file. Private
// streams are left out; private is how many were.
func (s *Store) ExportStreams() (data []byte, count, private int, err error) {
	list := streamList{App: listApp, Version: listVersion, Streams: []sharedStream{}}
	for _, st := range s.Streams() {
		if st.Private {
			private++
			continue
		}
		list.Streams = append(list.Streams, sharedStream{Label: st.Label, Source: st.Source, Transport: st.Transport})
	}
	data, err = json.MarshalIndent(list, "", "  ")
	return append(data, '\n'), len(list.Streams), private, err
}

// Imported says what ImportStreams did with a list.
type Imported struct {
	// Added is how many streams were new and are now saved.
	Added int `json:"added"`
	// Known is how many were already in the list and left as they were.
	Known int `json:"known"`
	// Invalid is how many entries couldn't be read as a stream.
	Invalid int `json:"invalid"`
}

// ImportStreams adds the streams in a stream list file to the end of the
// saved ones. A stream already saved (the same name or URL) is left alone,
// so importing a list twice, or an updated list, never makes duplicates and
// never changes what the listener has set up.
func (s *Store) ImportStreams(data []byte) (Imported, error) {
	var res Imported
	if len(data) > maxListSize {
		return res, errors.New("that file is too big to be a stream list")
	}
	var list streamList
	if err := json.Unmarshal(data, &list); err != nil || list.App != listApp || list.Streams == nil {
		return res, errors.New("that file isn't a PartyLine stream list")
	}
	if list.Version > listVersion {
		return res, errors.New("that stream list was made by a newer PartyLine; update to load it")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	have := map[string]bool{}
	for _, st := range s.data.Streams {
		if !st.Private {
			have[strings.ToLower(st.Source)] = true
		}
	}
	for _, in := range list.Streams {
		src, err := vrcdn.Parse(in.Source)
		if err != nil {
			res.Invalid++
			continue
		}
		st := Stream{Label: strings.TrimSpace(in.Label), Source: src.Canonical(), Transport: in.Transport}
		if have[strings.ToLower(st.Source)] {
			res.Known++
			continue
		}
		if st.Label == "" {
			st.Label = src.Display()
		}
		if !st.Transport.Valid() {
			st.Transport = vrcdn.RTSP
		}
		st.ID = newID()
		have[strings.ToLower(st.Source)] = true
		s.data.Streams = append(s.data.Streams, st)
		res.Added++
	}
	if res.Added == 0 {
		return res, nil
	}
	return res, s.saveLocked()
}
