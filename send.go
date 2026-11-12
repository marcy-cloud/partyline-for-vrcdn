package main

import (
	"errors"
	"slices"
	"strings"

	"partyline/internal/meter"
	"partyline/internal/secret"
	"partyline/internal/sender"
	"partyline/internal/store"
	"partyline/internal/vrcdn"
)

// Events sent to the UI by the sender.
const (
	eventSend       = "send"       // sender.State, on every change
	eventSendLevels = "sendLevels" // meter.Levels, about 25 times a second while live
)

// SendSettings are the sender's choices as the UI sees them. The stream key
// is not among them: it goes in through SetSendKey and never comes back out.
type SendSettings struct {
	Ingest      string `json:"ingest"`
	Device      string `json:"device"`
	BitrateKbps int    `json:"bitrateKbps"`
}

// SendInfo is everything the Send panel needs to draw itself.
type SendInfo struct {
	// Supported is false where this build cannot send (see sender.Supported).
	Supported bool            `json:"supported"`
	Settings  SendSettings    `json:"settings"`
	HasKey    bool            `json:"hasKey"`
	State     sender.State    `json:"state"`
	Devices   []sender.Device `json:"devices"`
	Ingests   []vrcdn.Ingest  `json:"ingests"`
	Bitrates  []int           `json:"bitrates"`
}

func (a *App) startSender() {
	a.send = sender.NewProxy(sender.Hooks{
		State:  func(s sender.State) { emit(eventSend, s) },
		Levels: func(l meter.Levels) { emit(eventSendLevels, l) },
	})
}

func (a *App) sendSupported() bool { return a.send != nil && a.engineErr == "" && sender.Supported() }

// sendSettings returns the saved choices with anything missing or no longer
// valid replaced by its default.
func sendSettings(saved store.Send) SendSettings {
	out := SendSettings{Ingest: saved.Ingest, Device: saved.Device, BitrateKbps: saved.BitrateKbps}
	if !slices.ContainsFunc(vrcdn.Ingests, func(i vrcdn.Ingest) bool { return i.URL == out.Ingest }) {
		out.Ingest = vrcdn.Ingests[0].URL
	}
	if !slices.Contains(sender.Bitrates, out.BitrateKbps) {
		out.BitrateKbps = sender.Bitrates[0]
	}
	return out
}

// SendInfo returns the sender's settings, state and choices.
func (a *App) SendInfo() SendInfo {
	saved := a.store.Send()
	info := SendInfo{
		Supported: a.sendSupported(),
		Settings:  sendSettings(saved),
		HasKey:    saved.Key != "",
		State:     sender.State{Status: sender.Off},
		Devices:   []sender.Device{},
		Ingests:   vrcdn.Ingests,
		Bitrates:  sender.Bitrates,
	}
	if info.Supported {
		info.State = a.send.State()
		info.Devices = a.SendDevices()
	}
	return info
}

// SendDevices lists the inputs that can be sent right now.
func (a *App) SendDevices() []sender.Device {
	if !a.sendSupported() {
		return []sender.Device{}
	}
	return sender.ListDevices()
}

// SaveSendSettings saves the sender's choices. They take effect the next
// time a broadcast starts.
func (a *App) SaveSendSettings(in SendSettings) (SendSettings, error) {
	saved := a.store.Send()
	saved.Ingest, saved.Device, saved.BitrateKbps = in.Ingest, in.Device, in.BitrateKbps
	out := sendSettings(saved)
	saved.Ingest, saved.BitrateKbps = out.Ingest, out.BitrateKbps
	return out, a.store.SetSend(saved)
}

// SetSendKey saves the stream key, protected so that only this Windows
// account can read it back. An empty key forgets the saved one.
func (a *App) SetSendKey(key string) error {
	key = strings.TrimSpace(key)
	saved := a.store.Send()
	if key == "" {
		saved.Key = ""
		return a.store.SetSend(saved)
	}
	if strings.ContainsAny(key, " \t\r\n/") {
		return errors.New("that doesn't look like a stream key: it has spaces or slashes in it")
	}
	protected, err := secret.Protect(key)
	if err != nil {
		return err
	}
	saved.Key = protected
	return a.store.SetSend(saved)
}

// StartSend goes live with the saved settings and key.
func (a *App) StartSend() error {
	if !a.sendSupported() {
		return errors.New("sending is not available in this build")
	}
	saved := a.store.Send()
	if saved.Key == "" {
		return errors.New("enter your stream key first")
	}
	key, err := secret.Reveal(saved.Key)
	if err != nil || key == "" {
		return errors.New("the saved stream key can't be read on this PC; enter it again")
	}
	settings := sendSettings(saved)
	return a.send.Start(sender.Config{
		Ingest:      settings.Ingest,
		Key:         key,
		Device:      settings.Device,
		BitrateKbps: settings.BitrateKbps,
	})
}

// StopSend ends the broadcast.
func (a *App) StopSend() {
	if a.send != nil {
		a.send.Stop()
	}
}
