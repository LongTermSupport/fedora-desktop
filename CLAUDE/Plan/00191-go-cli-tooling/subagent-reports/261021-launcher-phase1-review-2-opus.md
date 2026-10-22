VERDICT: MERGE

I reviewed branch agent-ae2815539d021bbf4-e1475f59 at 0da92893 against 7c09e65f and changed nothing in it. Checks run:
- `qa-changed --base 7c09e65f` passed (rc 0, 16 files, the Go-backed suites green).
- `go test -race -shuffle=on -count=5` passed for execx, ui, engine and tokens. `qa/rules` is a separate module and passed with -race.
- I probed for goroutine leaks with a `go test -overlay` file kept under `/workspace/untracked/scratch/r2leak/`, so the worktree is untouched.

**Round-1 dispositions, checked**

1. Holds. `relabel.go:125` `FixArgv` now ends with `"{}", "+"`, and `FixCommand` is `"sudo "+quoteAll(FixArgv(...))`. `relabel_test.go` checks the full argv. `TestFixArgvRunsUnderFind` runs the real `find` with a fake `chown` on PATH and checks the exact arguments, so a missing `+` would fail it.
2. Holds. `exec.go:52` `ChildEnv` drops every base entry whose exact name is in `unset` or `add`, then keeps only the last added entry of each name.
   - Case: matching is exact. That is right on Linux, where `getenv` is case-sensitive. `PASS` against `PASSWORD` and `A` against `AB` are both tested.
   - Names with and without values: `envKey` splits on the first `=`. A bare `"FOO"` in `add` would remove the parent's `FOO`. This is harmless because `Cmd.Env` is documented as KEY=VALUE, and no production caller passes a bare name (grep).
   - PATH and other unrelated variables are kept. `TestFixArgvRunsUnderFind` shows an `Env` PATH replacing the parent's rather than doubling it.
   - `ChildEnv` has one caller, `secret.go:93`, which adds secrets after `Env`, so a secret wins over an `Env` entry of the same name. `TestOSExecGivesTheChildOneValueOfEachName` proves one value per name through the real `OS.Exec` path, using only the `syscallExec` seam.
3. Holds. `secret.go:17-23` names both leftovers: the child's own children, and `/proc/self/environ`.
4. Holds, with a caveat (finding 1). A cancel returns `ErrCancelled` wrapping `context.Cause`, and a context that has already ended is refused before any read. Both are tested.
5. Holds. The Foreground doc says to read the child's status from `Exit`.
6. Holds. Kind is unexported (`engine.go:61-64`) and `New` refuses anything but podman or docker. Every path to `cmd` (`run`, `info`) goes through `known()`, which refuses the zero Engine, and `SELinuxMode` and `UIDMap` call `known()` too. Nothing outside the package builds an Engine (grep `Engine{`). The `table.go:131` reason is now true by construction, apart from code inside the package itself.
7, 8. Hold. `src-go/CLAUDE.md` lists `engine` and `tokens`, the Secret rule names `execx.Secret` and `TakeSecret`, and the follow-ups are in PLAN.md. The T5 and T7 wording is fixed.
9. Holds and is test-only. `processGone` is in `session_test.go:53` and is used only by `living`, which only `dies` calls; no test asserts that a process is alive. Reading ESRCH as "ended" is right: `/proc/<pid>/stat` returns ESRCH only for a task that is exiting or already reaped. EACCES still fails the test, and that case is tested. The code under test is unchanged, so no real failure can be hidden.

**New findings (none blocking)**

1. **Each `ui.Lines` leaks one goroutine for the life of the process (`ui/lines.go:224-233`).** `readLines` ranges over `want`, which is never closed. My overlay probe: 2 goroutines before, 52 after 50 `Lines` that each answered one question normally. The doc at lines 38-40 says so, and the launcher will hold one `Lines`, so this is acceptable for phase 1. Two consequences phase 2 should handle:
   - (a) After a cancelled question the abandoned `ReadString` on stdin is still pending. If the flow then runs a `Foreground` child on the same terminal (`podman run -it`), the parent's read takes the first line typed, which the child never sees. I confirmed that a cancelled read's line is handed to the next question.
   - (b) Any future goleak-style test will flag every `Lines`.

   Suggest a `Close` that ends the reader, or a rule that a flow exits after a cancel. Either belongs in the PLAN follow-ups next to the Foreground/SignalContext note.
2. **Copying a `Lines` after its first question is unsafe.** The `want`, `replies`, `pending` and `ended` state is copied, and `l.want <- struct{}{}` at `lines.go:187` runs outside the `select`, so it does not watch ctx. A copy with `pending=false`, made while the original's read is abandoned, blocks there, and a cancel cannot end it. The old `*bufio.Reader` field had a milder form of this copy hazard. Suggest putting the send inside the `select`, or documenting "use by pointer only". There are no production callers yet (grep).
3. **Nit: `TestFixArgvRunsUnderFind` assumes `t.TempDir()` allows execution.** On a noexec TMPDIR it would fail with an unclear message. This is environment-only, not a code defect.
4. The PLAN.md Task 4.5 text is accurate against the code: the `+` fix with a real-find test, `ChildEnv` giving one value per name, `engine.New`, the Lines context, the documented interplay, and ESRCH. It does not mention the Lines goroutine; adding it with finding 1 would be enough.
5. No install-specific names in the diff. The only paths are the round-1 report's `/home/user` and `/home/u` placeholders.