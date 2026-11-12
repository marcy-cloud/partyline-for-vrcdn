//go:build !windows

package sender

import "errors"

func coreAudioInstalled() bool { return false }

func openCoreAudio(Config) (session, error) {
	return nil, errors.New("Apple's AAC encoder is only available on Windows")
}
