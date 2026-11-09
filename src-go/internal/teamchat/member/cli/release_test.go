//go:build teamchatrelease

package cli

import "testing"

// CL2: an installed binary never returns 1 on purpose, so before Task 3.3 installs it every verb
// is built. Task 3.3's build runs this package's tests with -tags teamchatrelease.
func TestNoVerbIsUnbuilt(t *testing.T) {
	for name, slice := range unbuilt {
		t.Errorf("teamchat %s is still unbuilt (slice %s)", name, slice)
	}
}
