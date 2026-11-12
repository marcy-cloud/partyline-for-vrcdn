// Package meter measures the level of audio passing through a libmpv core,
// for the level meters of both the player and the sender.
package meter

import (
	"strconv"

	"partyline/internal/libmpv"
)

// Filter is the value for mpv's "af" option that makes Read work. It is
// ffmpeg's astats filter, which tags each audio frame with its per-channel
// peak. It only measures: the audio passes through untouched and unbuffered,
// ahead of any volume control.
const Filter = "@levels:lavfi=[astats=metadata=1:reset=1:measure_perchannel=Peak_level:measure_overall=none]"

const property = "af-metadata/levels"

// Silence is the level reported when there is no signal, in dBFS.
const Silence = -100.0

// Levels is the peak level of the audio per channel, in dBFS (0 is full
// scale, Silence is nothing). A mono signal reports the same value for both.
type Levels struct {
	Left  float64 `json:"left"`
	Right float64 `json:"right"`
}

// Read returns the most recent frame's peak levels. It reports false when
// there is no reading, for example before audio has started.
func Read(m *libmpv.Client) (Levels, bool) {
	node, err := m.GetNode(property)
	if err != nil {
		return Levels{}, false
	}
	meta, _ := node.(map[string]any)
	left, okL := peak(meta, 1)
	right, okR := peak(meta, 2)
	switch {
	case okL && okR:
		return Levels{Left: left, Right: right}, true
	case okL: // mono
		return Levels{Left: left, Right: left}, true
	}
	return Levels{}, false
}

// peak picks one channel's peak (dBFS) out of astats' frame metadata, where
// it is a string such as "-12.3" or "-inf".
func peak(meta map[string]any, channel int) (float64, bool) {
	text, ok := meta["lavfi.astats."+strconv.Itoa(channel)+".Peak_level"].(string)
	if !ok {
		return 0, false
	}
	db, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false
	}
	// -inf and NaN can't go into JSON; neither is louder than Silence.
	if !(db > Silence) {
		db = Silence
	}
	return min(db, 0), true
}
