package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccystart"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunStartsTheSessionInTheImagePaths(t *testing.T) {
	fake := &execx.Fake{}
	s := session([]string{"claude", "--x"})
	if s.Paths != ccystart.ImagePaths() {
		t.Fatalf("paths %+v", s.Paths)
	}
	s.Env = func(k string) (string, bool) {
		v, ok := map[string]string{"PINGBUS_MEMBERSHIP": "off", "CCY_NO_SUPERVISOR": "1"}[k]
		return v, ok
	}
	s.Execer = fake
	if code := run(s); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if execs := fake.Execs(); len(execs) != 1 || execs[0].Line() != "claude --x" {
		t.Fatalf("execs %v", execs)
	}
}
