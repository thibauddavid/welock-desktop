//go:build darwin

package sidecar

import _ "embed"

// binary is the macOS engine helper, committed opaquely (like a web client commits
// a prebuilt wasm module). A universal (arm64+amd64) build is regenerated on a core bump via
// tools/build-sidecar.sh; a local `go build` of the engine's cmd/sidecar suffices for dev.
//
//go:embed bin/welock-sidecar_darwin
var binary []byte

const binName = "welock-sidecar"
