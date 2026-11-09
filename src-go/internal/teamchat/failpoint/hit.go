//go:build !busfailpoints

package failpoint

// Hit is a failpoint, compiled to nothing without the busfailpoints tag.
func Hit(Name) {}
