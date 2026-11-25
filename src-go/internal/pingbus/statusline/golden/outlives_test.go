package golden

import (
	"errors"
	"syscall"
	"testing"
)

// fakeProcs is a process that ends after a number of checks (never when endAt is negative) and
// whose kill answers killErr.
type fakeProcs struct {
	endAt   int
	killErr error
	checks  int
	kills   int
	pauses  int
}

func (f *fakeProcs) procs() procs {
	return procs{
		ended: func(int) (bool, error) {
			f.checks++
			return f.endAt >= 0 && f.checks > f.endAt, nil
		},
		kill: func(int) error {
			f.kills++
			return f.killErr
		},
		pause: func() { f.pauses++ },
	}
}

// R67: a process seen running at the last check that is gone by the kill (ESRCH) did not
// outlive the bound, and is no problem; any other refused kill still is.
func TestOutlivesDecision(t *testing.T) {
	notPermitted := syscall.EPERM
	for _, c := range []struct {
		name       string
		endAt      int
		killErr    error
		outlived   bool
		wantErr    error
		wantKills  int
		wantPauses int
	}{
		{"ended at once", 0, nil, false, nil, 0, 0},
		{"ended within the bound", 10, nil, false, nil, 0, 10},
		{"ended at the last check", goneChecks, nil, false, nil, 0, goneChecks},
		{"died between the last check and the kill", -1, syscall.ESRCH, false, nil, 1, goneChecks},
		{"outlived, killed", -1, nil, true, nil, 1, goneChecks},
		{"outlived, kill refused", -1, notPermitted, true, notPermitted, 1, goneChecks},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeProcs{endAt: c.endAt, killErr: c.killErr}
			outlived, err := outlives(42, f.procs())
			if outlived != c.outlived {
				t.Errorf("outlived = %v, want %v", outlived, c.outlived)
			}
			if !errors.Is(err, c.wantErr) || (c.wantErr == nil) != (err == nil) {
				t.Errorf("err = %v, want %v", err, c.wantErr)
			}
			if f.kills != c.wantKills {
				t.Errorf("kills = %d, want %d", f.kills, c.wantKills)
			}
			if f.pauses != c.wantPauses {
				t.Errorf("pauses = %d, want %d", f.pauses, c.wantPauses)
			}
		})
	}
}

// A status that cannot be read is an error, not a verdict either way, and nothing is killed.
func TestOutlivesUnreadableStatus(t *testing.T) {
	unreadable := errors.New("unreadable")
	kills := 0
	p := procs{
		ended: func(int) (bool, error) { return false, unreadable },
		kill:  func(int) error { kills++; return nil },
		pause: func() {},
	}
	outlived, err := outlives(42, p)
	if outlived || !errors.Is(err, unreadable) || kills != 0 {
		t.Fatalf("outlives = %v, %v, kills %d; want false, unreadable, 0", outlived, err, kills)
	}
}
