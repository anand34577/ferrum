//go:build darwin && arm64

package needle

import _ "embed"

//go:embed bundled/darwin-arm64/needle
var bundledBinary []byte

const bundledName = "needle"
