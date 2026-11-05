# n3b slice review, part B: the trust boundary and the identity

Slice 0b7542a9b..0f2f54a31. Scope: host/lxc/guestroot, lxccmd, unit, lxc/guestbus, guestfacts,
lxcstate, declare, paths, member/guest, member/cli, and the QA rule rows. The reviewer's write was
blocked by the cross-worktree guard, so the lead saved its findings from its message.
`go test -race` passes on all 10 packages.

## Verdict: FIX-FIRST

2 major, 3 minor, 2 nit.

## Findings

1. **B1, major: a guest user's file lease fails every seat in the container.**
   guestroot's reopenFD (read.go:264, O_NONBLOCK) answers EAGAIN when a guest user holds an
   F_WRLCK lease on a file it owns: ccy.env.local, .git/config or seat.lock. The reviewer verified
   this as uid 0. The error is not ErrRefused, so:

   - apply.readsFailed fails every seat in the container as a host fault, and the phase exits
     non-zero;
   - LP1's probe returns an error instead of free.

   One guest user can therefore fail every member in the container. Fix: map EAGAIN on the reopen
   to ErrRefused, so it fails that member alone. Add a lease test.

2. **B2, major (TB2.4): guest text reaches root's stderr.**
   guestbus's SystemdError.Error prints the guest peer's D-Bus error name and message, and
   ErrMalformed prints the reply body with %v (guestbus.go:63, 188, 208). Both reach root's stderr
   through restart.go:70 and apply's r.step.

   Fix: a closed grammar for the error name; drop the message; print only the count and the
   signature. Add a test.

3. **B3, minor: the tmux check is wider than the unit.**
   The unit runs /usr/bin/tmux, but the LX4 check accepts tmux anywhere in SystemDirs. Check the
   exact path the unit runs.

4. **B4, minor: the checkout rule is weaker in declare, and kept in five copies.**
   declare's validCheckout refuses only \\n and \\r, so tab, ESC and NUL pass into the SECURITY line
   and the approval gate; the unit refuses them later. Use one paths grammar for the checkout and
   the seat bounds, instead of five copies.

5. **B5, minor: guest reads have no deadline.**
   A FUSE allow_other mount at the guest root (a privileged container) can stall the apply. Bound
   each guest read.

6. **B6, nit:** the git subsection escape in `[remote "orig\in"]` is not checked.

7. **B7, nit:** passwd does not refuse a second account that shares the agent's uid.

## Checked and found sound

- openat2 and O_PATH reads and writes; the pin; DialSocket.
- The parsers, apart from B6 and B7.
- lxccmd.
- The seat key.
- The unit and tmux names.
- The capped reader.
- declare's duplicate key.
- The QA rules.
