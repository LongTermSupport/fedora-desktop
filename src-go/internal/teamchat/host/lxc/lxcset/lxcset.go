// Package lxcset is the LXC set's status record, paths.LXCSetStatusFile (Plan 00266 C.3): the
// containers the record of the last shutdown names, each as the boot unit found it once its start
// and reconcile were done, and each running container that record does not name. Root writes it
// 0640 to the owner group, so ccy-sessions verify-restore reads it as the user, with no sudo; the
// boot id it carries tells a reader whether it was written in this boot.
package lxcset

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// Mode is the record's mode: root's, the owner group's to read.
const (
	Mode      fs.FileMode = 0o640
	MaxStatus             = 1 << 20
)

// ErrInvalid is wrapped by every refusal of a status.
var ErrInvalid = errors.New("invalid LXC set status")

// Kind is what was found of one container.
type Kind string

// A recorded container running (Up) or not (Down, its state word or Absent as Why); a running one
// the record does not name (Unrecorded); and the record itself never written (NoRecord, its path
// as Why and no container).
const (
	Up         Kind = "UP"
	Down       Kind = "DOWN"
	Unrecorded Kind = "UNRECORDED"
	NoRecord   Kind = "NO-RECORD"
)

// Absent is a Down container's Why when the host has no container of its name.
const Absent = "ABSENT"

// Line is one container's finding.
type Line struct {
	Kind      Kind   `json:"kind"`
	Container string `json:"container"`
	Why       string `json:"why"`
}

// Status is the set as found at WrittenMS (Unix ms) in the boot BootID names.
type Status struct {
	V         int    `json:"v"`
	BootID    string `json:"boot_id"`
	WrittenMS int64  `json:"written_ms"`
	Lines     []Line `json:"lines"`
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// check is a line in its kind's form: one word each, a container but for NoRecord, a Why only
// for Down and NoRecord.
func (l Line) check() error {
	if strings.ContainsAny(l.Container+l.Why, " \t\n") {
		return invalid("line %+v holds a space", l)
	}
	ok := false
	switch l.Kind {
	case Up, Unrecorded:
		ok = l.Container != "" && l.Why == ""
	case Down:
		ok = l.Container != "" && l.Why != ""
	case NoRecord:
		ok = l.Container == "" && l.Why != ""
	}
	if !ok {
		return invalid("line %+v is in no kind's form", l)
	}
	return nil
}

func (s Status) check() error {
	if s.V != 1 {
		return invalid("version %d, not 1", s.V)
	}
	if s.BootID == "" || strings.ContainsAny(s.BootID, " \t\n") {
		return invalid("boot id %q is not one word", s.BootID)
	}
	if s.WrittenMS <= 0 {
		return invalid("written_ms %d is not a time", s.WrittenMS)
	}
	for _, l := range s.Lines {
		if err := l.check(); err != nil {
			return err
		}
	}
	return nil
}

// Write replaces the record under root ("" on a host) with s, Mode and group gid (-1 keeps the
// writer's), atomically. A status Read would refuse is refused, and nothing is written.
func Write(root string, s Status, gid int) error {
	s.V = 1
	if s.Lines == nil {
		s.Lines = []Line{}
	}
	if err := s.check(); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return state.WriteFile(root+paths.LXCSetStatusFile, data, Mode, gid)
}

// Read is the record under root; found is false when there is none. A file that is not one
// Write would write (an unknown key, another version, a line in no kind's form) is refused.
func Read(root string) (Status, bool, error) {
	path := root + paths.LXCSetStatusFile
	data, found, err := state.ReadFile(path, MaxStatus)
	if err != nil || !found {
		return Status{}, false, err
	}
	var s Status
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Status{}, false, fmt.Errorf("%s: %w: %w", path, ErrInvalid, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Status{}, false, fmt.Errorf("%s: %w", path, invalid("content after the value"))
	}
	if err := s.check(); err != nil {
		return Status{}, false, fmt.Errorf("%s: %w", path, err)
	}
	return s, true, nil
}
