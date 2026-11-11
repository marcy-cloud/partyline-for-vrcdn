//go:build windows && amd64 && bundled_libmpv

package libmpv

import _ "embed"

// The library itself, compiled into the program so there is nothing to
// install. Both files are produced by tools/fetch-libmpv.

//go:embed bundled/libmpv-2.dll.gz
var bundledGz []byte

//go:embed bundled/manifest.json
var bundledManifest []byte
