//go:build windows

package sidecar

import _ "embed"

// binary is the Windows engine helper, committed opaquely and regenerated on a core
// bump via tools/build-sidecar.sh.
//
//go:embed bin/welock-sidecar_windows.exe
var binary []byte

const binName = "welock-sidecar.exe"
