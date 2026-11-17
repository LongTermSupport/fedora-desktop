// Package fixture holds one struct for each of fieldalignment's two diagnostics. The test copies
// it into a module of its own and runs golangci-lint over it.
package fixture

// Wasteful pads a bool before an int64 and another after it: 24 bytes where 16 would do, the
// size diagnostic, which the configuration must report.
type Wasteful struct {
	First bool
	Count int64
	Last  bool
}

// PointerLate is as small as it can be, but its pointer comes after its int64: the pointer-ordering
// diagnostic, which the configuration must not report.
type PointerLate struct {
	Count int64
	Name  string
}
