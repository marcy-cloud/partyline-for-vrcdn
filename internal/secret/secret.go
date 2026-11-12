// Package secret keeps a small secret, such as a stream key, in a form that
// is safe to write to the settings file: encrypted so that only the same
// user account on the same PC can read it back.
package secret

import "errors"

// ErrUnsupported is returned where there is no way to protect a secret yet.
var ErrUnsupported = errors.New("storing a secret is not supported on this system yet")
