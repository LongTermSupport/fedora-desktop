package lxcstate

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// The host's two boot records (Plan 00266 Task C.2): the seats `lxc start` started and `lxc stop`
// has not stopped (paths.LXCStartedFile), and the containers that ran at the last shutdown
// (paths.LXCRunningFile). Both are root's alone, outside every container and checkout, and decode
// strictly: a file must be what its write would write.
const (
	MaxStarted              = 1 << 20
	StartedMode fs.FileMode = 0o600
)

// StartedSeat is one started seat, by the container and key `lxc start --member` named.
type StartedSeat struct {
	Container paths.ContainerName `json:"container"`
	Key       paths.SeatKey       `json:"key"`
}

// String is the seat as `--member` names it.
func (s StartedSeat) String() string { return s.Container.String() + "/" + s.Key.String() }

func compareStarted(a, b StartedSeat) int {
	return cmp.Or(cmp.Compare(a.Container.String(), b.Container.String()), cmp.Compare(a.Key.String(), b.Key.String()))
}

// Started is the seats started and not since stopped on purpose; a container stopped by a
// shutdown or by maintenance leaves its seats here.
type Started struct {
	V     int           `json:"v"`
	Seats []StartedSeat `json:"seats"`
}

// Has is whether seat is recorded.
func (s Started) Has(seat StartedSeat) bool { return slices.Contains(s.Seats, seat) }

// Add returns s with seat recorded once.
func (s Started) Add(seat StartedSeat) Started {
	return Started{Seats: append(slices.Clone(s.Seats), seat)}.normal()
}

// Remove returns s without seat.
func (s Started) Remove(seat StartedSeat) Started {
	return Started{Seats: slices.DeleteFunc(slices.Clone(s.Seats), func(x StartedSeat) bool { return x == seat })}.normal()
}

// normal is the record as it is written: v 1, its seats sorted and each once.
func (s Started) normal() Started {
	seats := slices.Clone(s.Seats)
	if seats == nil {
		seats = []StartedSeat{}
	}
	slices.SortFunc(seats, compareStarted)
	return Started{V: 1, Seats: slices.Compact(seats)}
}

// ReadStarted is the started seats under the host root; no file is none.
func ReadStarted(root string) (Started, error) {
	var s Started
	found, err := readBoot(root+paths.LXCStartedFile, &s, func() any { return s.normal() })
	if err != nil || !found {
		return Started{}.normal(), err
	}
	return s, nil
}

// WriteStarted writes s root 0600 atomically. A file there that does not decode is refused, not
// replaced.
func WriteStarted(root string, s Started) error {
	if _, err := ReadStarted(root); err != nil {
		return err
	}
	return writeBoot(root+paths.LXCStartedFile, s.normal())
}

// running is the containers that ran at the last shutdown.
type running struct {
	V          int                   `json:"v"`
	Containers []paths.ContainerName `json:"containers"`
}

func (r running) normal() running {
	c := slices.Clone(r.Containers)
	if c == nil {
		c = []paths.ContainerName{}
	}
	slices.SortFunc(c, func(a, b paths.ContainerName) int { return cmp.Compare(a.String(), b.String()) })
	return running{V: 1, Containers: slices.Compact(c)}
}

// ReadRunning is the containers that ran at the last shutdown, sorted; no file is none.
func ReadRunning(root string) ([]paths.ContainerName, error) {
	var r running
	found, err := readBoot(root+paths.LXCRunningFile, &r, func() any { return r.normal() })
	if err != nil || !found {
		return nil, err
	}
	return r.Containers, nil
}

// WriteRunning writes the containers, sorted and each once, root 0600 atomically. A file there
// that does not decode is refused, not replaced.
func WriteRunning(root string, containers []paths.ContainerName) error {
	if _, err := ReadRunning(root); err != nil {
		return err
	}
	return writeBoot(root+paths.LXCRunningFile, running{Containers: containers}.normal())
}

// readBoot decodes the file at path into v strictly: v 1, and in its written form, which normal
// gives once v is read.
func readBoot(path string, v any, normal func() any) (bool, error) {
	data, found, err := state.ReadFile(path, MaxStarted)
	if err != nil || !found {
		return false, err
	}
	if err := decodeStrict(data, MaxStarted, v); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if err := sameWritten(data, normal()); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	return true, nil
}

func writeBoot(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return state.WriteFile(path, data, StartedMode, -1)
}
