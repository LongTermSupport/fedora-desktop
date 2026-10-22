package apply

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// MO5: a `member add --seat next` that crashed, asked again the same way (lxc apply's retry),
// finishes the handle it claimed instead of claiming the next.
func TestASeatNextRetryFinishesTheClaimedHandle(t *testing.T) {
	for _, c := range []crash{{"ResetPassword", 1, true, StepCredential}, {"Invite", 1, false, StepRooms}, {"Send", 1, true, StepNotice}} {
		t.Run(c.method, func(t *testing.T) {
			w := newWorld(t)
			team := w.team(t)
			bad := &crashing{Admin: w.admin, method: c.method, n: c.n, landed: c.landed}
			if _, err := RunMember(t.Context(), w.memberDeps(bad), team, add(0, credToken)); !errors.Is(err, errCrash) {
				t.Fatalf("err = %v", err)
			}
			claimed := w.file(t).roster.Pending.Handle
			res := w.member(t, team, add(0, credToken))
			f := w.file(t).roster
			if res.Handle != claimed || res.Credential == nil || len(f.Members) != 1 || f.Pending != nil {
				t.Fatalf("retry answered %s (claimed %s), members %v", res.Handle, claimed, f.Members)
			}
			if n := countPrefixed(w.notices(t), "added "); n != 1 {
				t.Fatalf("notices %v", w.notices(t))
			}
		})
	}
}

// MO5: the same verb on the same handle with other arguments is not a retry of the pending one.
func TestARetryWithOtherArguments(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	w.member(t, team, add(3, credPassword))
	bad := &crashing{Admin: w.admin, method: "PutState", n: 1}
	if _, err := RunMember(t.Context(), w.memberDeps(bad), team, MemberRequest{Op: OpMemberSet, Handle: third, HumanText: false}); !errors.Is(err, errCrash) {
		t.Fatalf("err = %v", err)
	}
	res := w.member(t, team, on(OpMemberSet, third))
	if !w.file(t).roster.Members[third].HumanText || !slices.Contains(res.Changed, "member "+third+" human_text true") {
		t.Fatalf("set true after a crashed set false: changed %v", res.Changed)
	}

	w = newWorld(t)
	team = w.team(t)
	bad = &crashing{Admin: w.admin, method: "Invite", n: 1}
	if _, err := RunMember(t.Context(), w.memberDeps(bad), team, add(3, credPassword)); !errors.Is(err, errCrash) {
		t.Fatalf("err = %v", err)
	}
	other := add(3, credPassword)
	other.HumanText = false
	mark := w.mark()
	_, err := RunMember(t.Context(), w.memberDeps(nil), team, other)
	refusedAt(t, "add with another human_text", err, StepFiles, "pending")
	if len(w.writes(mark)) != 0 || w.file(t).roster.Pending == nil {
		t.Fatalf("the refusal wrote %v", w.writes(mark))
	}
}

// MO3: an account that is there deactivated is refused before any change, for add and return.
func TestADeactivatedAccountIsRefusedBeforeAnyChange(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	w.member(t, team, add(2, credPassword))
	w.member(t, team, on(OpMemberPark, other))
	if _, err := w.admin.CreateAccount(t.Context(), user(third), "x-pass-1", ""); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{third, other} {
		if err := w.admin.SetDeactivated(t.Context(), user(h), true); err != nil {
			t.Fatal(err)
		}
	}
	mark, seq := w.mark(), w.file(t).roster.Seq
	_, err := RunMember(t.Context(), w.memberDeps(nil), team, add(3, credPassword))
	refusedAt(t, "add", err, StepAccount, "deactivated")
	_, err = RunMember(t.Context(), w.memberDeps(nil), team, on(OpMemberReturn, other))
	refusedAt(t, "return", err, StepAccount, "deactivated")
	if len(w.writes(mark)) != 0 || w.file(t).roster.Seq != seq {
		t.Fatalf("a refusal wrote: %v", w.writes(mark))
	}
}

// C 3.6.3: a guest seat's token is copied for the health tick, and the copy goes with the
// credential (a park, a password issue, a retire).
func TestATokenIsCopiedForTheHealthTick(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	copyOf := func() (string, bool) {
		t.Helper()
		file := filepath.Join(w.dir, "guest", third+".token")
		raw, err := os.ReadFile(filepath.Clean(file))
		if errors.Is(err, fs.ErrNotExist) {
			return "", false
		}
		if err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("the copy's mode %v %v", info.Mode(), err)
		}
		return string(raw), true
	}
	res := w.member(t, team, add(3, credToken))
	if got, ok := copyOf(); !ok || got != res.Credential.Secret {
		t.Fatalf("the copy after add: %t", ok)
	}
	w.member(t, team, on(OpMemberPark, third))
	if _, ok := copyOf(); ok {
		t.Fatal("a copy after park")
	}
	req := on(OpMemberReturn, third)
	req.Credential = credToken
	res = w.member(t, team, req)
	if got, ok := copyOf(); !ok || got != res.Credential.Secret {
		t.Fatal("no copy after return")
	}
	w.member(t, team, on(OpMemberReissue, third))
	if _, ok := copyOf(); ok {
		t.Fatal("a copy after a password reissue")
	}
	req = on(OpMemberReissue, third)
	req.Credential = credToken
	w.member(t, team, req)
	w.member(t, team, on(OpMemberRetire, third))
	if _, ok := copyOf(); ok {
		t.Fatal("a copy after retire")
	}
}

// The token copy's failures are the step's: no directory configured, or one that cannot be made.
func TestATokenCopyThatCannotBeKeptFailsTheStep(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	d := w.memberDeps(nil)
	d.GuestDir = ""
	if _, err := RunMember(t.Context(), d, team, add(3, credToken)); err == nil || !strings.Contains(err.Error(), "step credential") {
		t.Fatalf("no directory: %v", err)
	}
	file := filepath.Join(w.dir, "a-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	d.GuestDir = filepath.Join(file, "guest")
	for _, req := range []MemberRequest{add(3, credToken), on(OpMemberPark, third)} {
		if _, err := RunMember(t.Context(), d, team, req); err == nil || !strings.Contains(err.Error(), "not a directory") {
			t.Fatalf("%s under a file: %v", req.Op, err)
		}
	}
}

// MO4: `--seat next` passes a free number whose account is deactivated, recording it retired.
func TestSeatNextPassesADeactivatedAccount(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	if _, err := w.admin.CreateAccount(t.Context(), user(coord), "x-pass-1", ""); err != nil {
		t.Fatal(err)
	}
	if err := w.admin.SetDeactivated(t.Context(), user(coord), true); err != nil {
		t.Fatal(err)
	}
	res := w.member(t, team, add(0, credPassword))
	f := w.file(t).roster
	if res.Handle != other || f.Members[coord].State != MemberRetired || !slices.Contains(res.Changed, "member "+coord+" retired") {
		t.Fatalf("answered %s, changed %v, members %v", res.Handle, res.Changed, f.Members)
	}
}

// MO5: an account deactivated after an add claimed its handle ends the add with the handle
// retired, and a roll-forward that ends it this way lets the caller's verb run, with a note.
func TestAClaimWhoseAccountIsDeactivatedIsDropped(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	bad := &crashing{Admin: w.admin, method: "Account", n: 2}
	if _, err := RunMember(t.Context(), w.memberDeps(bad), team, add(3, credPassword)); !errors.Is(err, errCrash) {
		t.Fatalf("err = %v", err)
	}
	if _, err := w.admin.CreateAccount(t.Context(), user(third), "x-pass-1", ""); err != nil {
		t.Fatal(err)
	}
	if err := w.admin.SetDeactivated(t.Context(), user(third), true); err != nil {
		t.Fatal(err)
	}
	w.addMember(t, other, active())
	res := w.member(t, team, MemberRequest{Op: OpMemberSet, Handle: other, HumanText: false})
	f := w.file(t).roster
	if f.Members[third].State != MemberRetired || f.Pending != nil || !slices.Contains(res.Changed, "member "+third+" retired") {
		t.Fatalf("roster %+v, changed %v", f, res.Changed)
	}
	if !slices.Contains(res.Changed, "member "+other+" human_text false") || len(res.Notes) != 1 || !strings.Contains(res.Notes[0], third) {
		t.Fatalf("changed %v, notes %v", res.Changed, res.Notes)
	}

	// The same verb asked again is the one refused.
	w = newWorld(t)
	team = w.team(t)
	if _, err := RunMember(t.Context(), w.memberDeps(&crashing{Admin: w.admin, method: "Account", n: 2}), team, add(3, credPassword)); !errors.Is(err, errCrash) {
		t.Fatalf("err = %v", err)
	}
	if _, err := w.admin.CreateAccount(t.Context(), user(third), "x-pass-1", ""); err != nil {
		t.Fatal(err)
	}
	if err := w.admin.SetDeactivated(t.Context(), user(third), true); err != nil {
		t.Fatal(err)
	}
	_, err := RunMember(t.Context(), w.memberDeps(nil), team, add(3, credPassword))
	refusedAt(t, "the retry", err, StepAccount, "deactivated")
	if f := w.file(t).roster; f.Pending != nil || f.Members[third].State != MemberRetired {
		t.Fatalf("roster %+v", f)
	}
}

// MO5: a roll-forward's failure names the pending operation, not the caller's.
func TestARollForwardFailureNamesThePendingOp(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	if _, err := RunMember(t.Context(), w.memberDeps(&crashing{Admin: w.admin, method: "Invite", n: 1}), team, add(3, credPassword)); !errors.Is(err, errCrash) {
		t.Fatalf("err = %v", err)
	}
	w.addMember(t, other, active())
	_, err := RunMember(t.Context(), w.memberDeps(&crashing{Admin: w.admin, method: "Invite", n: 1}), team, MemberRequest{Op: OpMemberSet, Handle: other, HumanText: false})
	if e, ok := errors.AsType[*Error](err); !ok || e.Op != OpMemberAdd || e.Step != StepRooms {
		t.Fatalf("err = %v", err)
	}
}

// MO5, C23: return and reissue crashed after their password reset landed, asked again, answer a
// working credential, and the one set before the crash no longer signs in.
func TestReturnAndReissueRetriedLeaveOneCredential(t *testing.T) {
	for _, op := range []string{OpMemberReturn, OpMemberReissue, OpMemberAdd} {
		t.Run(op, func(t *testing.T) {
			w := newWorld(t)
			team := w.team(t)
			req := add(3, credPassword)
			if op != OpMemberAdd {
				w.member(t, team, req)
				req = on(op, third)
			}
			if op == OpMemberReturn {
				w.member(t, team, on(OpMemberPark, third))
			}
			bad := &crashing{Admin: w.admin, method: "ResetPassword", n: 1, landed: true}
			if _, err := RunMember(t.Context(), w.memberDeps(bad), team, req); !errors.Is(err, errCrash) {
				t.Fatalf("err = %v", err)
			}
			before := bad.passwords[len(bad.passwords)-1]
			res := w.member(t, team, req)
			if res.Credential == nil || !w.signsIn(t, third, res.Credential.Secret) || w.signsIn(t, third, before) {
				t.Fatalf("the retry's credential %+v; the one before the crash still signs in: %t", res.Credential, w.signsIn(t, third, before))
			}
			if f := w.file(t).roster; f.Pending != nil || f.Members[third].State != MemberActive {
				t.Fatalf("roster %+v", f)
			}
		})
	}
}

// C 3.6.1: a revoke leaves no credential recorded, at a new cred_seq.
func TestARevokeRecordsNoCredential(t *testing.T) {
	w := newWorld(t)
	team := w.team(t)
	w.member(t, team, add(3, credPassword))
	issued := w.file(t).roster.Members[third].CredSeq
	w.member(t, team, on(OpMemberPark, third))
	if m := w.file(t).roster.Members[third]; m.Cred != "" || m.CredSeq <= issued {
		t.Fatalf("parked %+v", m)
	}

	w = newWorld(t)
	team = w.team(t)
	if _, err := RunMember(t.Context(), w.memberDeps(&crashing{Admin: w.admin, method: "Invite", n: 1}), team, add(3, credPassword)); !errors.Is(err, errCrash) {
		t.Fatalf("err = %v", err)
	}
	issued = w.file(t).roster.Members[third].CredSeq
	w.addMember(t, other, active())
	w.member(t, team, MemberRequest{Op: OpMemberSet, Handle: other, HumanText: false})
	if m := w.file(t).roster.Members[third]; m.Cred != "" || m.Needs != needsReissue || m.CredSeq <= issued {
		t.Fatalf("revoked %+v", m)
	}
}
