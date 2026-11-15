package apply

import (
	"cmp"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The human operations this build serves (Plan 00209 SPEC-n2h HU2, HU4, HU5). `human lock` and
// `human unlock` wait on the owner's decision n2h-D12 (a roster state `locked`) and on the
// real-Tuwunel proof n2h-D20.
const (
	OpHumanPassword = "human.password"
	OpHumanLogout   = "human.logout"
	OpHumanDevices  = "human.devices"
)

// HumanRequest is one human verb: the op and the declared human's name.
type HumanRequest struct {
	Op, Name string
}

// HumanPasswordLen is the length of a human's password, of humanAlphabet (HU2, n2h-D14).
const HumanPasswordLen = 32

const humanAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// humanByteLimit is the largest multiple of the alphabet's size a byte can hold: a byte at or
// above it is drawn again, so every character is equally likely (no modulo bias).
const humanByteLimit = 256 - 256%len(humanAlphabet)

// Password is a human's new password; no format of it shows it, only Reveal does.
type Password string

func (Password) String() string { return "human password" }

// Format writes String for every verb, so the password never reaches a log by a format.
func (p Password) Format(f fmt.State, _ rune) {
	if _, err := io.WriteString(f, p.String()); err != nil {
		return
	}
}

// Reveal is the password itself, for the one answer that carries it.
func (p Password) Reveal() string { return string(p) }

// RunHuman runs one human verb over the applied team (`team.json`): the human must be declared,
// enrolled in the roster and have an account (HU1, n2h-D16).
func RunHuman(ctx context.Context, d Deps, team declare.Team, req HumanRequest) (Result, error) {
	r := newRun(d, team, req.Op)
	err := r.humanVerb(ctx, req)
	r.res.Rooms = r.files.rooms
	return r.res, withOp(err, req.Op)
}

func (r *run) humanVerb(ctx context.Context, req HumanRequest) error {
	if !slices.Contains([]string{OpHumanPassword, OpHumanLogout, OpHumanDevices}, req.Op) {
		return refused(StepHumans, fmt.Errorf("%s is not a human verb this build serves", req.Op))
	}
	if err := r.loadApplied(); err != nil {
		return err
	}
	id, err := r.humanTarget(ctx, req.Name)
	if err != nil {
		return err
	}
	if req.Op == OpHumanDevices {
		return r.humanDevices(ctx, id)
	}
	if r.files.roster.Pending != nil {
		if err = r.rollForward(ctx); err != nil {
			return err
		}
	}
	return r.humanReset(ctx, req, id)
}

// humanTarget is the human's user ID once HU1's refusals pass; each is made before any change.
func (r *run) humanTarget(ctx context.Context, name string) (string, error) {
	if !slices.Contains(r.team.Humans, name) {
		return "", refused(StepHumans, fmt.Errorf("human %s is not declared in team %s", name, r.team.ID))
	}
	if h, known := r.files.roster.Humans[name]; !known || h.State != HumanEnrolled {
		return "", refused(StepHumans, fmt.Errorf("human %s is not enrolled in the roster: run team apply first", name))
	}
	id := proto.UserID(name, r.team.ServerName)
	if _, found, err := r.d.Admin.Account(ctx, id); err != nil {
		return "", failed(StepHumans, err)
	} else if !found {
		return "", refused(StepHumans, fmt.Errorf("human %s has no account on the homeserver: run team apply first", name))
	}
	return id, nil
}

// humanReset is HU2 (password) and HU4 (logout): the roster first, with revoked_seq the seq of
// that write (HU3), then a reset that signs every device out, then needs cleared for a password,
// then the rooms step (HU11). Only password answers the new password.
func (r *run) humanReset(ctx context.Context, req HumanRequest, id string) error {
	pw, err := newHumanPassword()
	if err != nil {
		return failed(StepHumans, err)
	}
	h := r.files.roster.Humans[req.Name]
	h.RevokedSeq = r.files.roster.Seq + 1
	r.files.roster.Humans[req.Name] = h
	if err = r.save(StepHumans); err != nil {
		return err
	}
	if err = r.d.Admin.ResetPassword(ctx, id, pw); err != nil {
		return failed(StepHumans, err)
	}
	if req.Op == OpHumanLogout {
		r.note("human " + req.Name + " signed out")
		return r.rooms(ctx)
	}
	if h.Needs == needsPass {
		h.Needs = ""
		r.files.roster.Humans[req.Name] = h
		if err = r.save(StepHumans); err != nil {
			return err
		}
	}
	r.note("human " + req.Name + " password set")
	if err = r.rooms(ctx); err != nil {
		return err
	}
	r.res.Password = Password(pw)
	return nil
}

// humanDevices is HU5: the account's devices, sorted by ID; nothing is written.
func (r *run) humanDevices(ctx context.Context, id string) error {
	devices, err := r.d.Admin.Devices(ctx, id)
	if err != nil {
		return failed(StepHumans, err)
	}
	slices.SortFunc(devices, func(a, b matrix.Device) int { return cmp.Compare(a.ID, b.ID) })
	r.res.Devices = devices
	return nil
}

// newHumanPassword is HumanPasswordLen characters of humanAlphabet from crypto/rand, each drawn
// uniformly (n2h-D14).
func newHumanPassword() (string, error) {
	out := make([]byte, 0, HumanPasswordLen)
	buf := make([]byte, 2*HumanPasswordLen)
	for len(out) < HumanPasswordLen {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("the random source: %w", err)
		}
		for _, b := range buf {
			if int(b) < humanByteLimit && len(out) < HumanPasswordLen {
				out = append(out, humanAlphabet[int(b)%len(humanAlphabet)])
			}
		}
	}
	return string(out), nil
}
