//go:build !((windows && amd64) || (linux && amd64) || (linux && arm64) || (darwin && arm64))

// This file backs every platform Ferrum doesn't ship a bundled Needle 2
// binary for (32-bit, RISC-V, Windows/ARM64, ...) — see the README "Built-in
// LLM (Needle 2)" section for the full platform list Needle itself supports.
// An operator on one of those can still point FERRUM_NEEDLE_BIN at a binary
// they downloaded themselves; only the auto-bundled fallback is unavailable.
package needle

var bundledBinary []byte

const bundledName = ""
