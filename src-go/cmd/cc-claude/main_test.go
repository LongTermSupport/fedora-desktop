package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccclaude"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunDispatchesThroughTheApp(t *testing.T) {
	fake := &execx.Fake{}
	app := ccclaude.NewApp()
	app.Execer = fake
	if code := run(app, []string{"--resume", "x"}); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if execs := fake.Execs(); len(execs) != 1 || execs[0].Line() != "claude --resume x" {
		t.Fatalf("execs %v", execs)
	}
}
