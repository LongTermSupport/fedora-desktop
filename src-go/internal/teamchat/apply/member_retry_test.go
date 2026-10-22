package apply

import (
	"errors"
	"slices"
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

// MO5: an account deactivated after an add claimed its handle drops the claim, so the pending
// add blocks no later verb.
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
	_, err := RunMember(t.Context(), w.memberDeps(nil), team, MemberRequest{Op: OpMemberSet, Handle: other, HumanText: false})
	refusedAt(t, "the roll-forward", err, StepAccount, "deactivated")
	if e, ok := errors.AsType[*Error](err); !ok || e.Op != OpMemberAdd {
		t.Fatalf("the failure names %v, want the pending op", err)
	}
	f := w.file(t).roster
	if _, held := f.Members[third]; held || f.Pending != nil {
		t.Fatalf("roster %+v", f)
	}
	res := w.member(t, team, MemberRequest{Op: OpMemberSet, Handle: other, HumanText: false})
	if !slices.Contains(res.Changed, "member "+other+" human_text false") {
		t.Fatalf("changed %v", res.Changed)
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
