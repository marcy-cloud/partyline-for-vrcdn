// Package pcmpipe gets raw audio out of libmpv as it is produced.
//
// libmpv has two ways of writing PCM somewhere other than an audio device.
// Its encoding mode, told to "encode" to PCM, hands the audio over 16384
// frames at a time: a third of a second, far too coarse for anything live.
// Its "pcm" audio output writes each piece as it comes, but only to a file.
// A Pipe is that file: a named pipe (a FIFO elsewhere) with this process
// reading the other end.
//
//	p, _ := pcmpipe.Listen()
//	// mpv options: ao=pcm, ao-pcm-file=p.Path(), ao-pcm-waveheader=no
//	r, _ := p.Accept() // once mpv has opened it
package pcmpipe
