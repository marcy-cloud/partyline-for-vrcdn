// Package latency measures how long audio takes to get from one end of a
// link to the other. A Source makes a test signal whose timing is known: a
// short beep every 1.3 seconds, over quiet hiss. A Detector finds the beeps in
// what arrives at the far end, and Delay says how late each one is. Both
// ends must use the same clock, so both run on one PC.
//
// The beeps cycle through eight pitches, which is how a late beep is told
// from the next one: delays up to about 10 seconds are unambiguous.
//
// The hiss is there for the codec. Digital silence encodes to almost
// nothing, and anything on the way that waits for a number of bytes (a
// socket buffer, a proxy) then holds it far longer than it would hold
// music. Hiss can't be compressed, so the stream runs at its full bitrate.
package latency

import (
	"math"
	"time"
)

// The signal is 48 kHz stereo 16-bit little-endian PCM.
const (
	Rate       = 48000
	FrameBytes = 4

	interval  = 64171 // frames from one beep to the next; not a multiple of any codec frame
	beepLen   = Rate * 60 / 1000
	tones     = 8
	amplitude = 8192 // -12 dBFS
	hiss      = 256  // peak, -42 dBFS: well under the detector's idea of quiet

	// Detection.
	loud      = 3000             // a beep has begun
	quiet     = 1000             // silence, allowing for a lossy codec's noise
	quietFor  = Rate / 10        // silence needed before the next beep counts
	pitchFrom = Rate * 5 / 1000  // skip the codec's smeared onset
	pitchLen  = Rate * 40 / 1000 // then count zero crossings over this much
	pitchEnd  = pitchFrom + pitchLen
)

// Window is the longest delay Delay can tell apart from a shorter one.
const Window = time.Duration(tones*interval) * time.Second / Rate

func pitch(tone int) float64 { return 600 + 200*float64(tone) }

func frames(n int64) time.Duration { return time.Duration(n) * time.Second / Rate }

// Source generates the test signal.
type Source struct {
	pos  int64
	seed uint32
}

// Fill writes the next len(buf)/FrameBytes frames of the signal.
func (s *Source) Fill(buf []byte) {
	for i := 0; i+FrameBytes <= len(buf); i += FrameBytes {
		var v int16
		if off := s.pos % interval; off < beepLen {
			tone := int(s.pos / interval % tones)
			// A cosine, so the beep starts at full level: a sharp edge to time.
			v = int16(amplitude * math.Cos(2*math.Pi*pitch(tone)*float64(off)/Rate))
		}
		// Different hiss on each side, or a codec would send it only once.
		l, r := v+s.noise(), v+s.noise()
		buf[i], buf[i+1] = byte(l), byte(l>>8)
		buf[i+2], buf[i+3] = byte(r), byte(r>>8)
		s.pos++
	}
}

func (s *Source) noise() int16 {
	s.seed = s.seed*1664525 + 1013904223
	return int16(s.seed>>16) % hiss
}

// Pos is how many frames have been generated.
func (s *Source) Pos() int64 { return s.pos }

// Beep is a beep found in received audio.
type Beep struct {
	// Tone is which of the eight pitches it has.
	Tone int
	// At is when the audio holding its first sample arrived.
	At time.Time
	// Pos is its first sample's place in the received audio, in frames.
	Pos int64
}

// Detector finds beeps in received audio.
type Detector struct {
	rest    []byte // part of a frame left over from the last Feed
	pos     int64  // frames taken in so far
	onset   int64
	silence int // quiet frames in a row
	armed   bool

	inBeep    bool
	at        time.Time
	seen      int // frames since the onset
	crossings int
	last      int16
}

// Feed takes audio that arrived at the given time and returns the beeps
// that could be identified in it. A beep is reported once enough of it has
// arrived to tell its pitch, about 45 ms after its onset.
func (d *Detector) Feed(pcm []byte, at time.Time) []Beep {
	var out []Beep
	data := append(d.rest, pcm...)
	n := len(data) / FrameBytes * FrameBytes
	for i := 0; i < n; i += FrameBytes {
		v := int16(uint16(data[i]) | uint16(data[i+1])<<8)
		d.pos++
		abs := v
		if abs < 0 {
			abs = -abs
		}
		if d.inBeep {
			if d.seen >= pitchFrom && d.seen < pitchEnd && (v >= 0) != (d.last >= 0) {
				d.crossings++
			}
			d.last = v
			if d.seen++; d.seen == pitchEnd {
				d.inBeep, d.armed, d.silence = false, false, 0
				// Two zero crossings per cycle.
				hz := float64(d.crossings) / 2 * Rate / pitchLen
				tone := (hz - pitch(0)) / (pitch(1) - pitch(0))
				if r := math.Round(tone); r >= 0 && r < tones && math.Abs(tone-r) < 0.35 {
					out = append(out, Beep{Tone: int(r), At: d.at, Pos: d.onset})
				}
			}
			continue
		}
		if abs < quiet {
			if d.silence++; d.silence >= quietFor {
				d.armed = true
			}
			continue
		}
		d.silence = 0
		if d.armed && abs >= loud {
			d.inBeep, d.at, d.onset, d.seen, d.crossings, d.last = true, at, d.pos-1, 1, 0, v
		}
	}
	d.rest = append(d.rest[:0], data[n:]...)
	return out
}

// Pos is how many whole frames have been fed in.
func (d *Detector) Pos() int64 { return d.pos }

// Frames is how long n frames last.
func Frames(n int64) time.Duration { return frames(n) }

// Delay is how long after it was generated a beep arrived, given when the
// Source's first frame was generated. ok is false for a beep that can't
// have come from this Source yet.
func Delay(start time.Time, b Beep) (delay time.Duration, ok bool) {
	pos, ok := SourcePos(start, b)
	return b.At.Sub(start) - frames(pos), ok
}

// SourcePos is where in the Source's signal a received beep began, in
// frames: the latest beep of its pitch that had been generated by the time
// it arrived.
func SourcePos(start time.Time, b Beep) (pos int64, ok bool) {
	elapsed := b.At.Sub(start)
	if elapsed < 0 {
		return 0, false
	}
	i := int64(elapsed * Rate / time.Second / interval)
	for i >= 0 && int(i%tones) != b.Tone {
		i--
	}
	return i * interval, i >= 0
}
