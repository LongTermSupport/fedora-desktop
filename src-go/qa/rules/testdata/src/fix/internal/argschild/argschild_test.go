package argschild

import (
	"os"
	"path/filepath"
	"testing"
)

type run struct{ Stderr string }

// TestMain runs the binary as another program when it is called by that name, as os.Args[0]
// says.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "tool" {
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestQuiet(t *testing.T) {
	var r run
	if r.Stderr == "" { // want `stderr of a child of the test binary compared with empty`
		t.Log(r)
	}
}
