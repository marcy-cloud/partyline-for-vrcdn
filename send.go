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

// SendSettings are the sender's choices as the UI sees them.
type SendSettings struct {
	// TargetID is the send target chosen.
	TargetID    string `json:"targetId"`
	Device      string `json:"device"`
	BitrateKbps int    `json:"bitrateKbps"`
	// Encoder is a sender.Encoder ID.
	Encoder string `json:"encoder"`
}

// SendTarget is a saved place to send to, as the UI sees it. Key only ever
// travels inwards: a newly typed key when saving, or "" to keep the saved
// one. It never comes back out.
type SendTarget struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Ingest string `json:"ingest"`
	Key    string `json:"key"`
}

// SendInfo is everything the Send side needs to draw itself.
type SendInfo struct {
	// Supported is false where this build cannot send (see sender.Supported).
	Supported bool            `json:"supported"`
	Settings  SendSettings    `json:"settings"`
	Targets   []SendTarget    `json:"targets"`
	State     sender.State    `json:"state"`
	Devices   []sender.Device `json:"devices"`
	Ingests   []vrcdn.Ingest  `json:"ingests"`
	Bitrates  []int           `json:"bitrates"`
	// Encoders are the AAC encoders usable on this PC, default first. More
	// than one only where Apple's encoder is installed.
	Encoders []sender.Encoder `json:"encoders"`
}

func (a *App) startSender() {
	a.send = sender.NewProxy(sender.Hooks{
		State:  func(s sender.State) { emit(eventSend, s) },
		Levels: func(l meter.Levels) { emit(eventSendLevels, l) },
	})
}

func (a *App) sendSupported() bool { return a.send != nil && a.engineErr == "" && sender.Supported() }

func knownIngest(url string) string {
	if slices.ContainsFunc(vrcdn.Ingests, func(i vrcdn.Ingest) bool { return i.URL == url }) {
		return url
	}
	return vrcdn.Ingests[0].URL
}

// sendSettings returns the saved choices with anything missing or no longer
// valid replaced by its default.
func (a *App) sendSettings() SendSettings {
	saved := a.store.Send()
	out := SendSettings{TargetID: saved.TargetID, Device: saved.Device, BitrateKbps: saved.BitrateKbps, Encoder: saved.Encoder}
	// An encoder that is gone (iTunes uninstalled) falls back to the default.
	encoders := sender.Encoders()
	if !slices.ContainsFunc(encoders, func(e sender.Encoder) bool { return e.ID == out.Encoder }) {
		out.Encoder = encoders[0].ID
	}
	if !slices.Contains(sender.Bitrates, out.BitrateKbps) {
		out.BitrateKbps = sender.Bitrates[0]
	}
	if _, ok := a.store.SendTarget(out.TargetID); !ok {
		out.TargetID = ""
		if targets := a.store.SendTargets(); len(targets) > 0 {
			out.TargetID = targets[0].ID
		}
	}
	return out
}

// publicTarget is a stored target without its key.
func publicTarget(t store.SendTarget) SendTarget {
	return SendTarget{ID: t.ID, Label: t.Label, Ingest: knownIngest(t.Ingest)}
}

func (a *App) publicTargets() []SendTarget {
	out := []SendTarget{}
	for _, t := range a.store.SendTargets() {
		out = append(out, publicTarget(t))
	}
	return out
}

// SendInfo returns the sender's settings, targets, state and choices.
func (a *App) SendInfo() SendInfo {
	info := SendInfo{
		Supported: a.sendSupported(),
		Settings:  a.sendSettings(),
		Targets:   a.publicTargets(),
		State:     sender.State{Status: sender.Off},
		Devices:   []sender.Device{},
		Ingests:   vrcdn.Ingests,
		Bitrates:  sender.Bitrates,
		Encoders:  sender.Encoders(),
	}
	if info.Supported {
		info.State = a.send.State()
		info.Devices = sender.ListDevices()
	}
	return info
}

// SaveSendSettings saves the sender's choices. They take effect the next
// time a broadcast starts.
func (a *App) SaveSendSettings(in SendSettings) (SendSettings, error) {
	saved := a.store.Send()
	saved.TargetID, saved.Device, saved.BitrateKbps, saved.Encoder = in.TargetID, in.Device, in.BitrateKbps, in.Encoder
	err := a.store.SetSend(saved)
	return a.sendSettings(), err
}

// SaveSendTarget creates (empty ID) or updates a send target. The key is
// protected so that only this Windows account can read it back.
func (a *App) SaveSendTarget(in SendTarget) (SendTarget, error) {
	t := store.SendTarget{ID: in.ID, Label: in.Label, Ingest: knownIngest(in.Ingest)}
	if key := strings.TrimSpace(in.Key); key != "" {
		if strings.ContainsAny(key, " \t\r\n/") {
			return SendTarget{}, errors.New("that doesn't look like a stream key: it has spaces or slashes in it")
		}
		protected, err := secret.Protect(key)
		if err != nil {
			return SendTarget{}, err
		}
		t.Key = protected
	}
	saved, err := a.store.SaveSendTarget(t)
	if err != nil {
		return SendTarget{}, err
	}
	return publicTarget(saved), nil
}

// DeleteSendTarget removes a send target and returns the remaining ones.
func (a *App) DeleteSendTarget(id string) ([]SendTarget, error) {
	err := a.store.DeleteSendTarget(id)
	return a.publicTargets(), err
}

// StartSend goes live to the chosen target with the saved settings.
func (a *App) StartSend() error {
	if !a.sendSupported() {
		return errors.New("sending is not available in this build")
	}
	settings := a.sendSettings()
	target, ok := a.store.SendTarget(settings.TargetID)
	if !ok {
		return errors.New("add somewhere to send to first")
	}
	key, err := secret.Reveal(target.Key)
	if err != nil || key == "" {
		return errors.New("the saved stream key can't be read on this PC; edit the send target and enter it again")
	}
	return a.send.Start(sender.Config{
		Ingest:      knownIngest(target.Ingest),
		Key:         key,
		Device:      settings.Device,
		BitrateKbps: settings.BitrateKbps,
		Encoder:     settings.Encoder,
	})
}

// StopSend ends the broadcast.
func (a *App) StopSend() {
	if a.send != nil {
		a.send.Stop()
	}
}
