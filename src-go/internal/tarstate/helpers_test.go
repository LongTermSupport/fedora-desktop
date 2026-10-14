package tarstate

import (
	"bytes"
	"testing"
)

func stateOf(t *testing.T, raw []byte) *State {
	t.Helper()
	s, err := Read(bytes.NewReader(raw), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// fileData is the content of a file member; the member must exist.
func fileData(t *testing.T, s *State, name string) string {
	t.Helper()
	data, ok := s.File(name)
	if !ok {
		t.Fatalf("no file member %q in %q", name, s.Names())
	}
	return string(data)
}
