//go:build linux && arm64

package needle

import _ "embed"

//go:embed bundled/linux-arm64/needle
var bundledBinary []byte

const bundledName = "needle"
