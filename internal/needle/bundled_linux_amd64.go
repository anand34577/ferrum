//go:build linux && amd64

package needle

import _ "embed"

//go:embed bundled/linux-amd64/needle
var bundledBinary []byte

const bundledName = "needle"
