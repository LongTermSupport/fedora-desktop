//go:build !busfailpoints

package engine

// hitPoint is the engine's failpoint, compiled to nothing without the busfailpoints tag.
func hitPoint(point) {}
