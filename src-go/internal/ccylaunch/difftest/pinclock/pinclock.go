// Package pinclock is the differential harness's pinned clock (Plan 00191 Task 4.5): the time
// the matrix's fake date gives the bash launcher, for a Go launcher under test. It is linked
// only into test builds: the difftest package's in-process front, and a claude-yolo built with
// -tags difftest (cmd/claude-yolo/clock_difftest.go). The production build never imports it,
// so no environment variable moves the production clock.
//
// The harness writes fakes.json into each scenario's sandbox, CCY_DIFFTEST_SANDBOX names the
// sandbox, and the clock reads the pinned epoch and the shift from it: Now is the epoch, and a
// file's modification time (stamped by the machine) is shifted back onto the pinned clock as
// the harness's fake stat shifts it.
package pinclock

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

// SandboxEnv names the scenario's sandbox in every program the harness starts.
const SandboxEnv = "CCY_DIFFTEST_SANDBOX"

// FakesFile is the file in the sandbox the harness's fakes read.
const FakesFile = "fakes.json"

// RunWindow bounds one run: a time the machine gave a file during it reads as the pinned now,
// so how long the run took never shows.
const RunWindow = 300

// Clock is the pinned clock: Epoch is the matrix's now, Shift how far the machine's clock is
// ahead of it.
type Clock struct {
	Epoch int64
	Shift int64
}

// Now is the pinned now, in the local zone.
func (c Clock) Now() time.Time { return time.Unix(c.Epoch, 0) }

// ModTime is a file's modification time on the pinned clock, in whole seconds.
func (c Clock) ModTime(info fs.FileInfo) time.Time {
	return time.Unix(c.Pinned(info.ModTime().Unix()), 0)
}

// Pinned is a machine time, in seconds, on the pinned clock.
func (c Clock) Pinned(machine int64) int64 {
	pinned := machine - c.Shift
	if pinned >= c.Epoch && pinned < c.Epoch+RunWindow {
		return c.Epoch
	}
	return pinned
}

// Read is the clock of the sandbox at dir.
func Read(dir string) (Clock, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Clock{}, fmt.Errorf("read the pinned clock: %w", err)
	}
	raw, err := root.ReadFile(FakesFile)
	err = errors.Join(err, root.Close())
	if err != nil {
		return Clock{}, fmt.Errorf("read the pinned clock: %w", err)
	}
	var cfg struct {
		Epoch *int64 `json:"epoch"`
		Shift int64  `json:"shift"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Clock{}, fmt.Errorf("read the pinned clock: %w", err)
	}
	if cfg.Epoch == nil {
		return Clock{}, errors.New("read the pinned clock: " + FakesFile + " has no epoch")
	}
	return Clock{Epoch: *cfg.Epoch, Shift: cfg.Shift}, nil
}

// FromEnv is the clock of the sandbox SandboxEnv names; an error outside the harness.
func FromEnv() (Clock, error) {
	dir := os.Getenv(SandboxEnv)
	if dir == "" {
		return Clock{}, errors.New("this is a difftest build of claude-yolo: it runs only inside the harness (" + SandboxEnv + " is unset)")
	}
	return Read(dir)
}
