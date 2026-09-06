//go:build windows && amd64

package needle

import _ "embed"

//go:embed bundled/windows-amd64/needle.exe
var bundledBinary []byte

const bundledName = "needle.exe"
