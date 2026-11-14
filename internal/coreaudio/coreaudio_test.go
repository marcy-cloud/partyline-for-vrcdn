//go:build windows || darwin

package coreaudio

import (
	"encoding/binary"
	"math"
	"testing"
)

// A second of a 440 Hz tone should come out as AAC packets of about the
// size the bitrate implies, at each bitrate the sender offers.
func TestEncode(t *testing.T) {
	if err := Available(); err != nil {
		t.Skipf("Apple's encoder is not available: %v", err)
	}
	const rate, channels = 48000, 2
	for _, kbps := range []int{320, 256, 192} {
		enc, err := NewEncoder(rate, channels, kbps)
		if err != nil {
			t.Fatalf("%d kbps: %v", kbps, err)
		}
		pcm := make([]byte, FrameSize*channels*2)
		packets, bytes := 0, 0
		for frame := range rate / FrameSize {
			for i := range FrameSize {
				n := frame*FrameSize + i
				v := uint16(int16(8000 * math.Sin(2*math.Pi*440*float64(n)/rate)))
				binary.LittleEndian.PutUint16(pcm[i*4:], v)
				binary.LittleEndian.PutUint16(pcm[i*4+2:], v)
			}
			packet, err := enc.Encode(pcm)
			if err != nil {
				t.Fatalf("%d kbps, frame %d: %v", kbps, frame, err)
			}
			if packet != nil {
				packets++
				bytes += len(packet)
			}
		}
		enc.Close()
		if packets < 40 {
			t.Fatalf("%d kbps: got %d packets from %d frames", kbps, packets, rate/FrameSize)
		}
		// Constant bitrate: a packet is 1024 samples, 21.3 ms.
		want := kbps * 1000 / 8 * FrameSize / rate
		if mean := bytes / packets; mean < want*8/10 || mean > want*12/10 {
			t.Errorf("%d kbps: packets average %d bytes, want about %d", kbps, mean, want)
		}
	}
}

func TestEncodeWantsWholeFrames(t *testing.T) {
	if err := Available(); err != nil {
		t.Skipf("Apple's encoder is not available: %v", err)
	}
	enc, err := NewEncoder(48000, 2, 192)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	if _, err := enc.Encode(make([]byte, 100)); err == nil {
		t.Error("a short frame should be refused")
	}
}
