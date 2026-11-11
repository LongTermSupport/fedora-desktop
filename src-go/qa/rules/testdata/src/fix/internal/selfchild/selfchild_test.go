package selfchild

import (
	"os"
	"testing"
)

func self(t *testing.T) string {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// The originating shapes: a child's stderr, a field of its result, required to be empty.
func TestQuiet(t *testing.T) {
	_ = self(t)
	var r Run
	if r.Exit != 0 || r.Stderr != "" { // want `stderr of a child of the test binary compared with empty`
		t.Fatal(r)
	}
	var res Result
	if string(res.Stderr) != "" { // want `stderr of a child of the test binary compared with empty`
		t.Fatal(res)
	}
	if len(res.Stderr) == 0 && res.Code == 0 {
		t.Log("len is not judged")
	}
}

// Read through a helper, compared with a text, or a different field: none is judged.
func TestRead(t *testing.T) {
	var r Run
	if Clean(r.Stderr) != "" {
		t.Fatal(r)
	}
	if r.Stderr != "boom\n" {
		t.Fatal(r)
	}
	var stdout string
	if stdout != "" {
		t.Fatal(stdout)
	}
}
