//go:build !(windows && amd64 && bundled_libmpv)

package libmpv

// No built-in library: libmpv comes from next to the program or from the
// system. (On Linux it is a normal package dependency.)
var bundledGz, bundledManifest []byte
