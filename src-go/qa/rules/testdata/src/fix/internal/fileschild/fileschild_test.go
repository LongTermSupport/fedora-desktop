package fileschild

import (
	"testing"

	"fix/internal/testfiles"
)

type run struct{ Stderr string }

// The test binary is copied from testfiles.Executable and run as a child.
func TestQuiet(t *testing.T) {
	if testfiles.Executable(t) == "" {
		t.Fatal("no binary")
	}
	var r run
	if r.Stderr != "" { // want `stderr of a child of the test binary compared with empty`
		t.Fatal(r)
	}
}
