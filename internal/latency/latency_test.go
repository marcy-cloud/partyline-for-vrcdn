package latency

import (
	"testing"
	"time"
)

// The signal, delivered late and in awkward pieces, is timed correctly.
func TestDetectorFindsBeepsAndTheirDelay(t *testing.T) {
	const late = 730 * time.Millisecond
	start := time.Unix(1000, 0)
	var src Source
	var det Detector
	found := 0
	signal := make([]byte, 12*Rate*FrameBytes)
	src.Fill(signal)
	// Odd-sized pieces split frames; each piece "arrives" late after its end.
	sent := 0
	for _, size := range cycle(len(signal), 1999, 4096, 7) {
		piece := signal[sent : sent+size]
		sent += size
		at := start.Add(frames(int64(sent / FrameBytes))).Add(late)
		for _, b := range det.Feed(piece, at) {
			d, ok := Delay(start, b)
			if !ok {
				t.Fatalf("beep %+v can't be placed", b)
			}
			// Arrival is stamped per piece, so a beep reads up to one piece late.
			if d < late || d > late+25*time.Millisecond {
				t.Errorf("tone %d: delay %v, want about %v", b.Tone, d, late)
			}
			// The first beep is at the very start, before any silence.
			if want := (found + 1) % tones; b.Tone != want {
				t.Errorf("beep %d has tone %d, want %d", found, b.Tone, want)
			}
			found++
		}
	}
	if found < 8 {
		t.Fatalf("found %d beeps in 12 s, want at least 8", found)
	}
}

// A delay longer than the gap between beeps is still read correctly.
func TestDelayBeyondOneInterval(t *testing.T) {
	start := time.Unix(1000, 0)
	const late = 4200 * time.Millisecond
	for i := int64(0); i < 20; i++ {
		b := Beep{Tone: int(i % tones), At: start.Add(frames(i * interval)).Add(late)}
		if d, ok := Delay(start, b); !ok || d != late {
			t.Errorf("beep %d: %v %v, want %v", i, d, ok, late)
		}
	}
	if _, ok := Delay(start, Beep{Tone: 3, At: start.Add(time.Second)}); ok {
		t.Error("a beep that hasn't been generated yet was placed")
	}
}

// cycle returns sizes that repeat the given pattern until they add up to total.
func cycle(total int, pattern ...int) []int {
	var out []int
	for i := 0; total > 0; i++ {
		n := min(pattern[i%len(pattern)], total)
		out = append(out, n)
		total -= n
	}
	return out
}
