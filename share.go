package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"partyline/internal/store"
)

// Sharing the saved streams: a file one person exports and another imports.
// The file format and what goes in it are the store's (see ExportStreams).

const (
	listFilename = "partyline-streams.json"
	listFilter   = "PartyLine stream list"
)

// Exported says what ExportStreams wrote.
type Exported struct {
	// Path is the file written; "" when the dialog was cancelled.
	Path string `json:"path"`
	// Count is how many streams are in the file.
	Count int `json:"count"`
	// Private is how many private streams were left out.
	Private int `json:"private"`
}

// ExportStreams asks where to save the stream list and writes it there.
func (a *App) ExportStreams() (Exported, error) {
	data, count, private, err := a.store.ExportStreams()
	if err != nil {
		return Exported{}, err
	}
	if count == 0 {
		if private > 0 {
			return Exported{}, errors.New("there is nothing to export: private streams are never put in a list")
		}
		return Exported{}, errors.New("there are no streams to export yet")
	}
	path, err := application.Get().Dialog.SaveFile().
		SetFilename(listFilename).
		AddFilter(listFilter, "*.json").
		PromptForSingleSelection()
	if err != nil || path == "" {
		return Exported{}, err
	}
	if !strings.EqualFold(filepath.Ext(path), ".json") {
		path += ".json"
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return Exported{}, err
	}
	return Exported{Path: path, Count: count, Private: private}, nil
}

// Imported says what ImportStreams loaded.
type Imported struct {
	store.Imported
	// Cancelled is set when no file was chosen.
	Cancelled bool `json:"cancelled"`
	// Streams is the saved list afterwards.
	Streams []store.Stream `json:"streams"`
}

// ImportStreams asks for a stream list file and adds its streams to the
// saved ones.
func (a *App) ImportStreams() (Imported, error) {
	path, err := application.Get().Dialog.OpenFile().
		SetTitle("Import a stream list").
		AddFilter(listFilter, "*.json").
		PromptForSingleSelection()
	if err != nil {
		return Imported{}, err
	}
	if path == "" {
		return Imported{Cancelled: true}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Imported{}, err
	}
	res, err := a.store.ImportStreams(data)
	return Imported{Imported: res, Streams: publicAll(a.store.Streams())}, err
}
