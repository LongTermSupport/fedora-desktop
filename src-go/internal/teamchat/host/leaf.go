package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// leafStep is the one step that makes the homeserver's leaf again (SPEC-cross-network X14, X31):
// `team apply` runs it under its lock, and the health tick runs it with no apply, so a team that
// is not applied for a year keeps a valid leaf. A leaf is derived, so one that is missing, torn,
// or signed by another CA is made again rather than refused, and the step says why.
type leafStep struct {
	root  string
	team  paths.TeamID
	maker pki.Maker
}

// leafOutcome is what the step found and did: the CHANGED line ("" for a leaf kept), and when
// the leaf in force ends and was made (zero when no leaf could be read).
type leafOutcome struct {
	line             string
	notAfter, madeAt time.Time
}

func (o leafOutcome) made() bool { return o.line != "" }

// LeafError is a leaf step that failed: Cause is the step that failed in a fixed form, which a
// status problem can carry (one line, bounded: the paths in it are the team's own fixed paths),
// and Err the error itself.
type LeafError struct {
	Cause string
	Err   error
}

func (e *LeafError) Error() string { return e.Cause + ": " + e.Err.Error() }

func (e *LeafError) Unwrap() error { return e.Err }

// leafFailed is err as a LeafError with cause, or nil for none.
func leafFailed(cause string, err error) error {
	if err == nil {
		return nil
	}
	return &LeafError{Cause: cause, Err: err}
}

// ensure judges the leaf against the CA certificate caPEM and want, and when it must be made
// again signs a new one with the CA sign gives and writes it, the key before the certificate.
// The CA's key is asked for only then, so a leaf that is kept never needs it. On an error the
// outcome still holds the end of the leaf in force.
func (s leafStep) ensure(caPEM []byte, sign func() (pki.CA, error), want pki.Names) (leafOutcome, error) {
	out, err := s.verdict(caPEM, want)
	if err != nil || !out.made() {
		return out, err
	}
	kept := leafOutcome{notAfter: out.notAfter, madeAt: out.madeAt}
	ca, err := sign()
	if err != nil {
		return kept, leafFailed("the CA key "+s.root+paths.CAKeyFile(s.team)+" cannot be read", err)
	}
	leaf, err := s.make(ca, want)
	if err != nil {
		return kept, err
	}
	for _, f := range s.files(leaf) {
		if err = state.WriteFile(f.Path, f.New, f.Mode, f.GID); err != nil {
			return kept, leafFailed("the leaf cannot be written to "+s.root+paths.TLSDir(s.team), err)
		}
	}
	return leafOutcome{line: out.line, notAfter: leaf.NotAfter(), madeAt: leaf.MadeAt()}, nil
}

// make signs a leaf for want with ca: apply's, the tick's and a CA rotation's one way to make it.
func (s leafStep) make(ca pki.CA, want pki.Names) (pki.Leaf, error) {
	leaf, err := s.maker.NewLeaf(ca, want)
	if err != nil {
		return pki.Leaf{}, leafFailed("the leaf cannot be signed by the CA", err)
	}
	return leaf, nil
}

// files are the leaf's two files where apply writes them, with its modes, the key before the
// certificate.
func (s leafStep) files(leaf pki.Leaf) []Replacement {
	return []Replacement{
		{Path: s.root + paths.LeafKeyFile(s.team), New: leaf.KeyPEM(), Mode: keyMode, GID: -1},
		{Path: s.root + paths.LeafCertFile(s.team), New: leaf.CertPEM(), Mode: certMode, GID: -1},
	}
}

// verdict is the leaf on disk judged: its line when it must be made, and its end when it parses.
func (s leafStep) verdict(caPEM []byte, want pki.Names) (leafOutcome, error) {
	unread := "the leaf's files in " + s.root + paths.TLSDir(s.team) + " cannot be read"
	cert, haveCert, err := state.ReadFile(s.root+paths.LeafCertFile(s.team), fileMax)
	if err != nil {
		return leafOutcome{}, leafFailed(unread, err)
	}
	key, haveKey, err := state.ReadFile(s.root+paths.LeafKeyFile(s.team), fileMax)
	if err != nil {
		return leafOutcome{}, leafFailed(unread, err)
	}
	switch {
	case !haveCert && !haveKey:
		return leafOutcome{line: "leaf made"}, nil
	case !haveCert || !haveKey:
		return leafOutcome{line: "leaf made again: a file of its pair is not there"}, nil
	}
	leaf, err := pki.ParseLeaf(caPEM, cert, key)
	if err != nil {
		return leafOutcome{line: "leaf made again: " + strings.TrimPrefix(err.Error(), "pki: ")}, nil
	}
	out := leafOutcome{notAfter: leaf.NotAfter(), madeAt: leaf.MadeAt()}
	renewal, err := leaf.NeedsRenewal(s.maker.Clock, want)
	if err != nil {
		return out, leafFailed("the leaf's wanted names are not valid", err)
	}
	if renewal.Needed() {
		out.line = "leaf made again: " + renewal.String()
	}
	return out, nil
}

// leafNames are what the leaf is reached by: the derived listen addresses, and the url host when
// it is not an IP literal (X6 already holds a literal to the listen addresses). The declaration
// holds the host to its canonical, lower-case form.
func leafNames(rawURL string, listen []netip.Addr) (pki.Names, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return pki.Names{}, err
	}
	names := pki.Names{IPs: slices.Clone(listen)}
	if _, literal := netip.ParseAddr(u.Hostname()); literal != nil {
		names.DNS = u.Hostname()
	}
	return names, nil
}

// AppliedLeafNames are the names the last apply under root made the leaf for: the listen set
// units.json records and the url host of team.json, both written by the same apply.
func AppliedLeafNames(root string) func(paths.TeamID) (pki.Names, error) {
	return func(team paths.TeamID) (pki.Names, error) {
		declared, listen, err := appliedListen(root, team)
		if err != nil {
			return pki.Names{}, err
		}
		return leafNames(declared.URL, listen)
	}
}

// appliedListen is what the last apply under root recorded of a team's reach: team.json, and the
// listen set of units.json.
func appliedListen(root string, team paths.TeamID) (declare.Team, []netip.Addr, error) {
	units, err := readUnits(root, team)
	if err != nil {
		return declare.Team{}, nil, err
	}
	file := root + paths.TeamDeclaration(team)
	raw, found, err := state.ReadFile(file, fileMax)
	if err == nil && !found {
		err = fmt.Errorf("%s is not there: the team was never applied", file)
	}
	if err != nil {
		return declare.Team{}, nil, err
	}
	declared, err := declare.DecodeTeamFile(raw)
	if err != nil {
		return declare.Team{}, nil, fmt.Errorf("%s: %w", file, err)
	}
	listen := make([]netip.Addr, 0, len(units.listen))
	for _, text := range units.listen {
		a, err := netip.ParseAddr(text)
		if err != nil {
			return declare.Team{}, nil, fmt.Errorf("units.json: %w", err)
		}
		listen = append(listen, a)
	}
	return declared, listen, nil
}

// Terminator is the team's TLS terminator as its caller read it: active, and since when.
type Terminator struct {
	Active bool
	Since  time.Time
}

// servesOldLeaf is a terminator that started before its leaf was made, so it serves the leaf it
// read at its start (a unit reads its credentials only then, X18c). The make time is to the
// second, so a start in the same second as the make, and before it, is not seen.
func (t Terminator) servesOldLeaf(madeAt time.Time) bool {
	return t.Active && !madeAt.IsZero() && t.Since.Before(madeAt)
}

// LeafRenewal is what a renewal found and did: the leaf made again and why, the terminator
// restarted, and when the leaf in force ends (zero when no leaf could be read).
type LeafRenewal struct {
	Made      bool
	Why       string
	Restarted bool
	NotAfter  time.Time
}

// Restarter restarts a unit and returns once its restart job has finished.
type Restarter interface {
	Restart(ctx context.Context, unit string) error
}

// LeafRenewer is X31: the health tick's renewal of a team's leaf, with no apply. Names are the
// names the leaf must hold (AppliedLeafNames on the host). The caller holds the team lock.
type LeafRenewer struct {
	Root  string
	Clock Clock
	Rand  io.Reader
	Units Restarter
	Names func(paths.TeamID) (pki.Names, error)
}

// Renew makes the team's leaf again by apply's step when X14's rule says so, and restarts the
// terminator when it is active and started before the leaf in force was made, which also tries
// again a restart that failed after the leaf was written. An inactive terminator is left as it
// is. It never makes a CA: a team with none, or a CA whose key cannot be read, is an error.
func (r LeafRenewer) Renew(ctx context.Context, team paths.TeamID, run Terminator) (LeafRenewal, error) {
	caFile := r.Root + paths.CACertFile(team)
	caPEM, found, err := state.ReadFile(caFile, fileMax)
	if err == nil && !found {
		err = errors.New("it is not there: team apply makes it")
	}
	if err != nil {
		return LeafRenewal{}, leafFailed("the CA certificate "+caFile+" cannot be read", err)
	}
	want, err := r.Names(team)
	if err != nil {
		return LeafRenewal{}, leafFailed("the names the leaf is for cannot be read from units.json and team.json", err)
	}
	step := leafStep{root: r.Root, team: team, maker: pki.Maker{Clock: wallClock{r.Clock}, Rand: r.Rand}}
	sign := func() (pki.CA, error) {
		ca, made, loadErr := loadCA(r.Root, team)
		if loadErr == nil && !made {
			loadErr = fmt.Errorf("the CA of team %s is not there: team apply makes it", team)
		}
		return ca, loadErr
	}
	out, err := step.ensure(caPEM, sign, want)
	got := LeafRenewal{Made: out.made() && err == nil, NotAfter: out.notAfter}
	if got.Made {
		got.Why = out.line
	}
	if err != nil || !run.servesOldLeaf(out.madeAt) {
		return got, err
	}
	unit := paths.TLSUnit(team)
	if err = r.Units.Restart(ctx, unit); err != nil {
		return got, leafFailed("the restart of "+unit+" failed", err)
	}
	got.Restarted = true
	return got, nil
}
