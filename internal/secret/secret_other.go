//go:build !windows

package secret

func Protect(string) (string, error) { return "", ErrUnsupported }

func Reveal(string) (string, error) { return "", ErrUnsupported }

const supported = false
