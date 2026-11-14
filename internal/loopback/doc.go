// Package loopback captures the sound one application is playing: a DJ
// program on the same PC, say, without whatever else the PC plays.
//
// It uses the Windows feature OBS calls "Application Audio Capture": WASAPI
// process loopback, there since Windows 10 version 2004. Nothing has to be
// installed and nothing about the application or the PC's sound settings
// changes.
package loopback
