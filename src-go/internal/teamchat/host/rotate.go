package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// FileWriter replaces one file whole, as state.WriteFile does: its data, mode and group (-1
// keeps the writer's).
type FileWriter func(path string, data []byte, mode fs.FileMode, gid int) error

// Replacement is one file a CA rotation replaces: the bytes it holds now, which a failed
// rotation writes back, and the bytes it is to hold.
type Replacement struct {
	Path     string
	Old, New []byte
	Mode     fs.FileMode
	GID      int
}

// TerminatorUnits is what a rotation asks of systemd: the terminator's state, and its restart.
type TerminatorUnits interface {
	State(ctx context.Context, unit string) (UnitState, error)
	Restart(ctx context.Context, unit string) error
}

// CARotation is Plan 00246 SPEC-cross-network X33 at the host: a new team CA and a leaf signed
// by it, for a CA key that is lost or exposed. The caller holds the team lock (whose guard
// refuses while a restore is recorded).
type CARotation struct {
	Root  string
	Clock Clock
	Rand  io.Reader
	Units TerminatorUnits
	Write FileWriter
}

// RotatedCA is a rotation done: the old and the new CA's fingerprints (never a key), and whether
// the terminator was restarted.
type RotatedCA struct {
	Old, New  string
	Restarted bool
}

// Rotate makes the new CA and a leaf for names by apply's leaf step, checks that the leaf
// verifies under the new CA and not under the old and that the two fingerprints differ, and
// only then replaces the CA's files, the leaf's and also's, each key before its certificate. A
// write that fails puts back every file already written, the failed one included, so the old CA,
// leaf and also's files stay whole. An active terminator is then restarted to serve the new
// leaf; an inactive one reads it at its start. A restart that fails leaves the rotation done,
// and the health tick restarts a terminator older than its leaf (X31).
func (r CARotation) Rotate(ctx context.Context, team paths.TeamID, names pki.Names, also []Replacement) (RotatedCA, error) {
	plan, got, err := r.prepare(team, names, also)
	if err != nil {
		return RotatedCA{}, fmt.Errorf("the CA of team %s is not rotated and nothing is replaced: %w", team, err)
	}
	if err = commit(r.Write, plan); err != nil {
		return RotatedCA{}, fmt.Errorf("the CA of team %s is not rotated: %w", team, err)
	}
	unit := paths.TLSUnit(team)
	st, err := r.Units.State(ctx, unit)
	if err == nil && st.Active == active {
		err = r.Units.Restart(ctx, unit)
		got.Restarted = err == nil
	}
	if err != nil {
		return got, fmt.Errorf("the CA is rotated, but %s was not restarted: %w; the health tick restarts it while it is active", unit, err)
	}
	return got, nil
}

// prepare is every file the rotation replaces, its new bytes made and checked, and the two
// fingerprints. Nothing is written.
func (r CARotation) prepare(team paths.TeamID, names pki.Names, also []Replacement) ([]Replacement, RotatedCA, error) {
	old, found, err := loadCA(r.Root, team)
	if err == nil && !found {
		err = errors.New("the team has no CA: team apply makes it")
	}
	if err != nil {
		return nil, RotatedCA{}, err
	}
	maker := pki.Maker{Clock: wallClock{r.Clock}, Rand: r.Rand}
	ca, err := maker.NewCA(caName(team))
	if err != nil {
		return nil, RotatedCA{}, err
	}
	step := leafStep{root: r.Root, team: team, maker: maker}
	leaf, err := step.make(ca, names)
	if err != nil {
		return nil, RotatedCA{}, err
	}
	got, err := checkRotation(old, ca, leaf)
	if err != nil {
		return nil, RotatedCA{}, err
	}
	plan := []Replacement{
		{Path: r.Root + paths.CAKeyFile(team), New: ca.KeyPEM(), Mode: keyMode, GID: -1},
		{Path: r.Root + paths.CACertFile(team), New: ca.CertPEM(), Mode: certMode, GID: -1},
	}
	plan = append(plan, step.files(leaf)...)
	for i := range plan {
		data, there, rerr := state.ReadFile(plan[i].Path, fileMax)
		if rerr == nil && !there {
			rerr = fmt.Errorf("%s is not there: team apply makes it", plan[i].Path)
		}
		if rerr != nil {
			return nil, RotatedCA{}, rerr
		}
		plan[i].Old = data
	}
	for _, f := range also {
		if f.Old == nil {
			return nil, RotatedCA{}, fmt.Errorf("%s has no bytes to put back", f.Path)
		}
	}
	return append(plan, also...), got, nil
}

// checkRotation holds the new material to X33 before anything is written: a leaf that verifies
// under the new CA and not under the old, and a new fingerprint.
func checkRotation(old, ca pki.CA, leaf pki.Leaf) (RotatedCA, error) {
	if _, err := pki.ParseLeaf(ca.CertPEM(), leaf.CertPEM(), leaf.KeyPEM()); err != nil {
		return RotatedCA{}, fmt.Errorf("the new leaf does not verify under the new CA: %w", err)
	}
	if _, err := pki.ParseLeaf(old.CertPEM(), leaf.CertPEM(), leaf.KeyPEM()); err == nil {
		return RotatedCA{}, errors.New("the new leaf verifies under the old CA")
	}
	oldFP, err := pki.Fingerprint(old.CertPEM())
	if err != nil {
		return RotatedCA{}, err
	}
	newFP, err := pki.Fingerprint(ca.CertPEM())
	if err != nil {
		return RotatedCA{}, err
	}
	if oldFP == newFP || bytes.Equal(old.KeyPEM(), ca.KeyPEM()) {
		return RotatedCA{}, errors.New("the new CA is the old one")
	}
	return RotatedCA{Old: oldFP, New: newFP}, nil
}

// commit writes each file's new bytes in order; on a failure it writes back the old bytes of the
// failed file and of each before it, last first. A file that cannot be put back is named: the
// team's files are then mixed, which apply refuses by name.
func commit(write FileWriter, plan []Replacement) error {
	for i, f := range plan {
		err := write(f.Path, f.New, f.Mode, f.GID)
		if err == nil {
			continue
		}
		var stuck []error
		for j := i; j >= 0; j-- {
			if berr := write(plan[j].Path, plan[j].Old, plan[j].Mode, plan[j].GID); berr != nil {
				stuck = append(stuck, berr)
			}
		}
		if len(stuck) > 0 {
			return fmt.Errorf("%w; and these could not be put back, so the files are mixed (restore them from a team backup): %w", err, errors.Join(stuck...))
		}
		return fmt.Errorf("%w; every file is put back and nothing is replaced", err)
	}
	return nil
}
