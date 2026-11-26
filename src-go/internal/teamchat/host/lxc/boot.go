package lxc

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// The boot unit's two container operations (Plan 00266 C.2): paths.LXCRecordService runs
// `lxc record-running` as it stops at shutdown, while LXC's own service still runs, and
// paths.LXCBootService runs `lxc boot` at boot: StartRecorded, then Reconcile, which starts the
// recorded seats in them even when a container start was refused. A host whose own IaC starts
// its containers runs no container start: the play leaves it out of the boot unit
// (teamchat_lxc_seat_restore_starts_containers). StartRecorded's errors are `lxc boot`'s, the one
// verb that runs it.
const (
	recordRunningVerb = "lxc record-running"
	bootVerb          = "lxc boot"
	checkRunningVerb  = "lxc check-running"
	stepRunningFile   = "running"
)

// RecordRunning is `lxc record-running`: the containers running now, written as the record
// paths.LXCRunningFile holds, replacing it whole. A container list or a state that cannot be read
// fails it with the record left as it was, so a host half read is never written as the truth.
func RecordRunning(ctx context.Context, d Deps) ([]paths.ContainerName, error) {
	if _, err := lxcstate.ReadRunning(d.Root); err != nil {
		return nil, &host.Error{Verb: recordRunningVerb, Step: stepRunningFile, Err: err}
	}
	names, err := d.LXC.Containers(ctx)
	if err != nil {
		return nil, &host.Error{Verb: recordRunningVerb, Step: stepState, Err: err}
	}
	var running []paths.ContainerName
	for _, n := range names {
		st, err := d.LXC.State(ctx, n)
		if err != nil {
			return nil, &host.Error{Verb: recordRunningVerb, Step: stepState, Err: err}
		}
		if st != lxccmd.Running {
			continue
		}
		c, err := paths.ParseContainerName(n)
		if err != nil {
			return nil, &host.Error{Verb: recordRunningVerb, Step: stepState, Err: err}
		}
		running = append(running, c)
	}
	if err := lxcstate.WriteRunning(d.Root, running); err != nil {
		return nil, &host.Error{Verb: recordRunningVerb, Step: stepRunningFile, Err: err}
	}
	return lxcstate.ReadRunning(d.Root)
}

// RunningKind is a line's first field in `lxc check-running` (Plan 00266 C.3).
type RunningKind string

// What CheckRunning found of each container the record names, and of each running one it does
// not; NoRecord is the record itself never written.
const (
	RunningUp         RunningKind = "UP"
	RunningDown       RunningKind = "DOWN"
	RunningUnrecorded RunningKind = "UNRECORDED"
	RunningNoRecord   RunningKind = "NO-RECORD"
)

// runningAbsent is a DOWN container's Why when this host has no container of its name.
const runningAbsent = "ABSENT"

// RunningLine is `<kind>\t<container>\t<why>`: a DOWN line's why is the container's state word
// (or ABSENT), one word, and a NO-RECORD line names the record's path in place of a container.
type RunningLine struct {
	Kind      RunningKind
	Container string
	Why       string
}

func (l RunningLine) String() string { return string(l.Kind) + "\t" + l.Container + "\t" + l.Why }

// CheckRunning is `lxc check-running`: the containers the record of the last shutdown names,
// each UP or DOWN by its state now, in the record's order, then each running container the
// record does not name, UNRECORDED; a record never written is a NO-RECORD line first. It only
// reads. Could not look (the record, the list or a state) is an error, never a line.
func CheckRunning(ctx context.Context, d Deps) ([]RunningLine, error) {
	recorded, found, err := lxcstate.ReadRunningRecord(d.Root)
	if err != nil {
		return nil, &host.Error{Verb: checkRunningVerb, Step: stepRunningFile, Err: err}
	}
	names, err := d.LXC.Containers(ctx)
	if err != nil {
		return nil, &host.Error{Verb: checkRunningVerb, Step: stepState, Err: err}
	}
	states := make(map[string]lxccmd.State, len(names))
	for _, n := range names {
		if states[n], err = d.LXC.State(ctx, n); err != nil {
			return nil, &host.Error{Verb: checkRunningVerb, Step: stepState, Err: err}
		}
	}
	var lines []RunningLine
	if !found {
		lines = append(lines, RunningLine{Kind: RunningNoRecord, Why: paths.LXCRunningFile})
	}
	inRecord := map[string]bool{}
	for _, c := range recorded {
		inRecord[c.String()] = true
		st, listed := states[c.String()]
		switch {
		case !listed:
			lines = append(lines, RunningLine{Kind: RunningDown, Container: c.String(), Why: runningAbsent})
		case st == lxccmd.Running:
			lines = append(lines, RunningLine{Kind: RunningUp, Container: c.String()})
		default:
			lines = append(lines, RunningLine{Kind: RunningDown, Container: c.String(), Why: string(st)})
		}
	}
	for _, n := range names {
		if states[n] == lxccmd.Running && !inRecord[n] {
			lines = append(lines, RunningLine{Kind: RunningUnrecorded, Container: n})
		}
	}
	return lines, nil
}

// BootKind is a container line's first field.
type BootKind string

// What StartRecorded found of each recorded container.
const (
	BootStarted BootKind = "STARTED"
	BootRunning BootKind = "RUNNING"
	BootMissing BootKind = "MISSING"
	BootRefused BootKind = "REFUSED"
)

// BootLine is one recorded container's line: `<kind>\t<container>\t<why>`.
type BootLine struct {
	Kind      BootKind
	Container paths.ContainerName
	Why       string
}

func (l BootLine) String() string { return string(l.Kind) + "\t" + l.Container.String() + "\t" + l.Why }

// BootRefusals is how many of lines are refused starts: the verb fails when any is.
func BootRefusals(lines []BootLine) int {
	return len(slices.DeleteFunc(slices.Clone(lines), func(l BootLine) bool { return l.Kind != BootRefused }))
}

// StartRecorded is `lxc boot`'s container start: each container the record of the last shutdown holds is
// started, unless it runs already or is gone; no other container is ever started. One line a
// recorded container, in the record's order. A refused start is its container's line, and the
// others still start. An error is the host's own fault: the record unreadable, or no container
// list.
func StartRecorded(ctx context.Context, d Deps) ([]BootLine, error) {
	recorded, err := lxcstate.ReadRunning(d.Root)
	if err != nil {
		return nil, &host.Error{Verb: bootVerb, Step: stepRunningFile, Err: err}
	}
	if len(recorded) == 0 {
		return nil, nil
	}
	names, err := d.LXC.Containers(ctx)
	if err != nil {
		return nil, &host.Error{Verb: bootVerb, Step: stepState, Err: err}
	}
	lines := make([]BootLine, 0, len(recorded))
	for _, c := range recorded {
		kind, why := startBox(ctx, d, names, c)
		lines = append(lines, BootLine{Kind: kind, Container: c, Why: why})
	}
	return lines, nil
}

// The wait for a started container's RUNNING: lxc@<c>.service's start returns once lxc-start
// runs, before the container's init does, and reconcile asks lxc-info next.
const (
	BootRunningWait = 60 * time.Second
	BootRunningGap  = time.Second
)

// startBox is one recorded container's start, or why there is none. The start is the host
// systemd's lxc@<c>.service (LXC's own template unit), never lxc-start run from the boot unit:
// every teamchat unit has a private mount namespace (v1names.InaccessiblePaths), which a
// container it started would inherit.
func startBox(ctx context.Context, d Deps, names []string, c paths.ContainerName) (BootKind, string) {
	if !slices.Contains(names, c.String()) {
		return BootMissing, whyMissing
	}
	st, err := d.LXC.State(ctx, c.String())
	if err != nil {
		return BootRefused, err.Error()
	}
	if st == lxccmd.Running {
		return BootRunning, ""
	}
	unit, err := containerUnit(c.String())
	if err != nil {
		return BootRefused, err.Error()
	}
	if err := d.Units.Start(ctx, unit); err != nil {
		return BootRefused, fmt.Sprintf("the start of a %s container through %s: %v", st, unit, err)
	}
	return waitRunning(ctx, d, c, unit)
}

// waitRunning asks lxc-info every BootRunningGap, for at most BootRunningWait, until the
// container is RUNNING; a container that is not by then, a failed lxc-info or a wait cut short is
// REFUSED, naming the container and its last state.
func waitRunning(ctx context.Context, d Deps, c paths.ContainerName, unit string) (BootKind, string) {
	deadline := d.Clock.Now().Add(BootRunningWait)
	for {
		st, err := d.LXC.State(ctx, c.String())
		switch {
		case err != nil:
			return BootRefused, fmt.Sprintf("container %s after %s started: %v", c, unit, err)
		case st == lxccmd.Running:
			return BootStarted, ""
		case !d.Clock.Now().Before(deadline):
			return BootRefused, fmt.Sprintf("container %s is %s %s after %s started; see journalctl -u %s", c, st, BootRunningWait, unit, unit)
		}
		if err := errors.Join(d.Clock.Sleep(ctx, BootRunningGap), ctx.Err()); err != nil {
			return BootRefused, fmt.Sprintf("the wait for container %s to run: %v", c, err)
		}
	}
}

// lxcTemplate is LXC's own template unit, upstream's lxc@.service (`lxc-start -F -n %i`).
const lxcTemplate = "lxc@"

// containerUnit is lxc@<name>.service, for a name the container grammar accepts and only then:
// no '@', '/', '\' or other character that would make systemd start another unit, and no
// leading '-' that lxc-start would read as an option.
func containerUnit(name string) (string, error) {
	c, err := paths.ParseContainerName(name)
	if err != nil {
		return "", fmt.Errorf("no lxc@ unit for %q: %w", name, err)
	}
	return lxcTemplate + c.String() + ".service", nil
}
