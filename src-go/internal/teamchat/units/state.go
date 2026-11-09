package units

import (
	"context"
	"encoding/hex"
	"fmt"
)

// managerKillUnit is systemd's KillUnit(name, whom, signal), which sends a signal and waits for
// no job (SD10).
const managerKillUnit = managerIface + ".KillUnit"

// notFound is the LoadState of a unit systemd has no file or transient unit for.
const notFound = "not-found"

// maxSignal is the highest Linux signal number, SIGRTMAX.
const maxSignal = 64

// State is what the backup and the restore read of a unit (SD10): its load and active states,
// its current invocation in hex ("" when it has none), its automatic restarts and the result of
// its last run.
type State struct {
	Load, Active string
	InvocationID string
	NRestarts    int64
	Result       string
}

// State is the unit's State, each value decoded strictly; a unit systemd cannot load is
// not-found and inactive.
func (m *Manager) State(ctx context.Context, unit string) (State, error) {
	s, path, found, err := m.unit(ctx, unit)
	if err != nil {
		return State{}, err
	}
	if !found {
		return State{Load: notFound, Active: inactive}, nil
	}
	var st State
	if st.Load, err = token(ctx, m, s, unit, path, "LoadState", false); err != nil {
		return State{}, err
	}
	if st.Active, err = token(ctx, m, s, unit, path, "ActiveState", false); err != nil {
		return State{}, err
	}
	id, err := property[[]byte](ctx, m, s, unit, path, unitIface, "InvocationID")
	if err != nil {
		return State{}, err
	}
	switch len(id) {
	case 0:
	case idBytes:
		st.InvocationID = hex.EncodeToString(id)
	default:
		return State{}, fmt.Errorf("systemd state of %s: %w: InvocationID is %d bytes", unit, ErrMalformed, len(id))
	}
	restarts, err := property[uint32](ctx, m, s, unit, path, serviceIface, "NRestarts")
	if err != nil {
		return State{}, err
	}
	st.NRestarts = int64(restarts)
	result, err := property[string](ctx, m, s, unit, path, serviceIface, "Result")
	if err != nil {
		return State{}, err
	}
	if !stateToken.MatchString(result) {
		return State{}, fmt.Errorf("systemd state of %s: %w: Result is not a state word", unit, ErrMalformed)
	}
	st.Result = result
	return st, nil
}

// Kill sends signal to whom ("main", "control" or "all") of the unit's processes (SD10).
func (m *Manager) Kill(ctx context.Context, unit, whom string, signal int32) error {
	if err := checkUnit(unit); err != nil {
		return err
	}
	switch whom {
	case "main", "control", "all":
	default:
		return fmt.Errorf("systemd kill %s: %q is not main, control or all", unit, whom)
	}
	if signal < 1 || signal > maxSignal {
		return fmt.Errorf("systemd kill %s: signal %d is out of range", unit, signal)
	}
	return m.call(ctx, "kill "+unit, managerKillUnit, unit, whom, signal)
}

// Start starts the unit and waits for its job.
func (m *Manager) Start(ctx context.Context, unit string) error {
	return m.job(ctx, "start", managerStartUnit, unit)
}

// Stop stops the unit and waits for its job.
func (m *Manager) Stop(ctx context.Context, unit string) error {
	return m.job(ctx, "stop", managerStopUnit, unit)
}
