package keeper

import (
	"os"
	"strings"
	"testing"

	"fix/internal/teamchat/paths"
)

type world struct{ copy string }

// keeperLog is the harness's one reader, which the allowance names.
func (w *world) keeperLog(r paths.Run) string {
	data, err := os.ReadFile(r.KeeperLog())
	if err != nil {
		return w.copy
	}
	return string(data)
}

// turned is the copy the loop's turn-end leaves, which the allowance names.
func (w *world) turned(r paths.Run) {
	data, err := os.ReadFile(r.KeeperLog())
	if err == nil {
		w.copy = string(data)
	}
}

// The originating shape: a barrier that polls keeper.log itself.
func (w *world) logged(r paths.Run, sub string) bool {
	data, err := os.ReadFile(r.KeeperLog()) // want `keeper.log read outside the harness's barrier`
	return err == nil && strings.Contains(string(data), sub)
}

func TestReadsTheFile(t *testing.T) {
	var r paths.Run
	if data, err := os.ReadFile(r.KeeperLog()); err != nil || len(data) == 0 { // want `keeper.log read outside the harness's barrier`
		t.Fatal(err)
	}
	name := r.KeeperLog // want `keeper.log read outside the harness's barrier`
	_ = name
	at := paths.Run.KeeperLog // want `keeper.log read outside the harness's barrier`
	_ = at
	go func() { _ = r.KeeperLog() }() // want `keeper.log read outside the harness's barrier`
}

func TestThroughTheHarness(t *testing.T) {
	var r paths.Run
	w := &world{}
	if !strings.Contains(w.keeperLog(r), "ready") {
		t.Fatal("not ready")
	}
	if _, err := os.ReadFile(r.LinkLog()); err == nil {
		t.Log("the link's log is another file")
	}
}
