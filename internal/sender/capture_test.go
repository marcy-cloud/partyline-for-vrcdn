package sender

import (
	"io"
	"sort"
	"testing"
	"time"

	"partyline/internal/libmpv"
)

// The capture must hand audio over in small pieces as it comes, not saved
// up: whatever it holds back is delay for the listener. This runs the real
// libmpv on the test tone and is skipped where libmpv isn't available.
//
//	go test -tags bundled_libmpv ./internal/sender/
func TestCaptureDeliversAudioAsItComes(t *testing.T) {
	if err := libmpv.Load(); err != nil {
		t.Skipf("libmpv is not available: %v", err)
	}
	c, err := openCapture(TestTone.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()

	type result struct {
		gaps  []time.Duration
		bytes int
		err   error
	}
	done := make(chan result, 1)
	go func() {
		var res result
		r, err := c.audio()
		if err != nil {
			done <- result{err: err}
			return
		}
		// libmpv takes over a second to start playing, then hands over what
		// it captured meanwhile all at once. Only what follows is judged.
		const settle = 2500 * time.Millisecond
		// One AAC frame at a time, as the encoder's pump reads it.
		frame := make([]byte, 4096)
		begin := time.Now()
		last := begin
		for time.Since(begin) < settle+2*time.Second {
			if _, err := io.ReadFull(r, frame); err != nil {
				res.err = err
				break
			}
			now := time.Now()
			if now.Sub(begin) > settle {
				res.gaps = append(res.gaps, now.Sub(last))
				res.bytes += len(frame)
			}
			last = now
		}
		done <- res
	}()

	var res result
	select {
	case res = <-done:
	case reason := <-c.gone():
		t.Fatalf("the capture ended: %s", reason)
	case <-time.After(15 * time.Second):
		t.Fatal("no audio came out of the capture")
	}
	if res.err != nil {
		t.Fatal(res.err)
	}
	if len(res.gaps) < 50 {
		t.Fatalf("only %d frames in two seconds", len(res.gaps))
	}
	sort.Slice(res.gaps, func(i, j int) bool { return res.gaps[i] < res.gaps[j] })
	longest := res.gaps[len(res.gaps)-1]
	t.Logf("%d frames in 2 s (%.0f%% of real time), longest wait for one %v", len(res.gaps), float64(res.bytes)/(2*48000*4)*100, longest.Round(time.Millisecond))
	// A frame lasts 21 ms. libmpv's encoding mode made this 341 ms.
	if longest > 100*time.Millisecond {
		t.Errorf("a frame took %v to arrive: audio is being saved up", longest)
	}

	// Closing must end a reader that is waiting, promptly.
	closed := make(chan struct{})
	go func() { c.close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("close did not return")
	}
}
