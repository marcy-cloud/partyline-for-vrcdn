// Command coreaudioprobe is an experiment on the coreaudio branch: can we
// call Apple's AAC encoder ourselves? It encodes a few seconds of test tone
// and writes the result as an ADTS file any player can open.
//
//	go run ./cmd/coreaudioprobe -o tone.aac
//
// With -key-file it sends the tone to an RTMP ingest instead, in real time,
// using a Go RTMP client: libmpv, which the sender is built on, cannot pass
// through audio that is already encoded.
//
//	go run ./cmd/coreaudioprobe -key-file key.txt -seconds 60
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bluenviron/gortmplib"
	"github.com/bluenviron/gortmplib/pkg/amf0"
	"github.com/bluenviron/gortmplib/pkg/message"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"

	"partyline/internal/coreaudio"
)

const (
	rate     = 48000
	channels = 2
)

func main() {
	out := flag.String("o", "tone.aac", "ADTS file to write")
	seconds := flag.Float64("seconds", 4, "how much tone to encode")
	bitrate := flag.Int("bitrate", 320, "kbps")
	keyFile := flag.String("key-file", "", "send to RTMP with the stream key in this file, instead of writing a file")
	ingest := flag.String("ingest", "rtmp://ingest.vrcdn.live/live", "RTMP ingest address")
	flag.Parse()

	if err := coreaudio.Available(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println("encoder:", coreaudio.Path())
	enc, err := coreaudio.NewEncoder(rate, channels, *bitrate)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer enc.Close()

	if *keyFile != "" {
		if err := send(enc, *ingest, *keyFile, *seconds, *bitrate); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer f.Close()

	frames := int(*seconds * rate / coreaudio.FrameSize)
	pcm := make([]byte, coreaudio.FrameSize*channels*2)
	packets, bytes := 0, 0
	began := time.Now()
	for i := 0; i < frames; i++ {
		tone(pcm, i)
		packet, err := enc.Encode(pcm)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		if packet == nil {
			continue
		}
		f.Write(adtsHeader(len(packet)))
		f.Write(packet)
		packets++
		bytes += len(packet)
	}
	took := time.Since(began)
	audio := float64(packets*coreaudio.FrameSize) / rate
	fmt.Printf("%d frames in -> %d AAC packets out, %d bytes (%.0f kbps), encoded %.1fs of audio in %d ms\n",
		frames, packets, bytes, float64(bytes*8)/audio/1000, audio, took.Milliseconds())
	fmt.Println("wrote", *out)
}

// tone fills pcm with frame i of a 440 Hz tone at -12 dBFS.
func tone(pcm []byte, i int) {
	for s := 0; s < coreaudio.FrameSize; s++ {
		n := i*coreaudio.FrameSize + s
		v := uint16(int16(0.25 * 32767 * math.Sin(2*math.Pi*440*float64(n)/rate)))
		for c := 0; c < channels; c++ {
			binary.LittleEndian.PutUint16(pcm[(s*channels+c)*2:], v)
		}
	}
}

// send encodes the tone in real time and publishes it over RTMP.
func send(enc *coreaudio.Encoder, ingest, keyFile string, seconds float64, bitrateKbps int) error {
	raw, err := os.ReadFile(keyFile)
	if err != nil {
		return err
	}
	key := strings.TrimSpace(string(raw))
	scrub := func(err error) error {
		return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), key, "<key>"))
	}
	u, err := url.Parse(strings.TrimRight(ingest, "/") + "/" + key)
	if err != nil {
		return scrub(err)
	}

	c := &gortmplib.Client{URL: u, Publish: true}
	if err := c.Initialize(context.Background()); err != nil {
		return scrub(err)
	}
	defer c.Close()

	// The three kinds of message are written by hand, not through the
	// library's Writer, so the metadata can be worded the way ffmpeg's FLV
	// muxer words it for an audio-only stream. (That was one theory for why
	// VRCDN closes this connection after 30 seconds; it made no difference.
	// See docs/coreaudio.md.)
	readerErr := make(chan error, 1)
	go func() {
		for {
			msg, err := c.Read()
			if err != nil {
				readerErr <- err
				return
			}
			fmt.Printf("  server says: %T\n", msg)
		}
	}()
	const streamID = 0x1000000
	config := &mpeg4audio.AudioSpecificConfig{Type: mpeg4audio.ObjectTypeAACLC, SampleRate: rate, ChannelCount: channels}
	audio := func(m message.Audio) error {
		m.ChunkStreamID, m.MessageStreamID = message.AudioChunkStreamID, streamID
		m.Codec, m.Rate, m.Depth, m.IsStereo = message.CodecMPEG4Audio, message.AudioRate44100, message.AudioDepth16, true
		return c.Write(&m)
	}
	err = c.Write(&message.DataAMF0{ChunkStreamID: 4, MessageStreamID: streamID, Payload: []any{
		"@setDataFrame", "onMetaData", amf0.Object{
			{Key: "duration", Value: float64(0)},
			{Key: "audiodatarate", Value: float64(bitrateKbps)},
			{Key: "audiosamplerate", Value: float64(rate)},
			{Key: "audiosamplesize", Value: float64(16)},
			{Key: "stereo", Value: true},
			{Key: "audiocodecid", Value: float64(10)},
			{Key: "encoder", Value: "PartyLine"},
		},
	}})
	if err == nil {
		err = audio(message.Audio{AACType: message.AudioAACTypeConfig, AACConfig: config})
	}
	if err != nil {
		return scrub(err)
	}
	fmt.Printf("connected to %s/<key>; sending CoreAudio AAC, no video\n", strings.TrimRight(ingest, "/"))

	pcm := make([]byte, coreaudio.FrameSize*channels*2)
	frame := time.Duration(coreaudio.FrameSize) * time.Second / rate
	began := time.Now()
	sent := 0
	for i := 0; time.Since(began).Seconds() < seconds; i++ {
		// Real time: frame i is due i frames after the start.
		time.Sleep(time.Until(began.Add(time.Duration(i) * frame)))
		tone(pcm, i)
		packet, err := enc.Encode(pcm)
		if err != nil {
			return err
		}
		if packet == nil {
			continue
		}
		if err := audio(message.Audio{AACType: message.AudioAACTypeAU, AU: packet, DTS: time.Duration(sent) * frame}); err != nil {
			// The reader usually knows why the connection went.
			return scrub(fmt.Errorf("%w (reader: %v)", err, <-readerErr))
		}
		sent++
		if sent%235 == 0 {
			fmt.Printf("  %5.1fs elapsed, %5.1fs of audio sent\n", time.Since(began).Seconds(), (time.Duration(sent) * frame).Seconds())
		}
	}
	fmt.Println("stopped")
	return nil
}

// adtsHeader frames one raw AAC-LC packet so the file is playable.
func adtsHeader(payload int) []byte {
	const profile, freqIndex = 1, 3 // AAC-LC, 48 kHz
	n := payload + 7
	return []byte{
		0xFF, 0xF1,
		byte(profile<<6 | freqIndex<<2 | channels>>2),
		byte((channels&3)<<6 | n>>11),
		byte(n >> 3),
		byte((n&7)<<5 | 0x1F),
		0xFC,
	}
}
