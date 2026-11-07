package fifotest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAReaderThatOpensThePipeIsSeenAndGetsEndOfFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe")
	opened := Plant(t, path)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 0 {
		t.Fatalf("read %q from the pipe, want nothing", body)
	}
	if !opened() {
		t.Fatal("the read was not seen")
	}
}

func TestAPipeNothingOpensIsNotSeen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe")
	opened := Plant(t, path)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("mode %v is not a named pipe", info.Mode())
	}
	if opened() {
		t.Fatal("a pipe nothing opened was seen as opened")
	}
}
