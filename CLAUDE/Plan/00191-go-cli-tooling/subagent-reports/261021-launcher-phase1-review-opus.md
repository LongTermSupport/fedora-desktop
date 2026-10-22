VERDICT: FIX-FIRST

Branch agent-a89bf0f342f6e5223-47c5fa84 @ 7c09e65f against integration/261021-membership. I changed nothing. `qa-changed --base integration/261021-membership` passed (golangci, go-qa, bash-limits, the Go-backed suites). I ran the race tests 3x with shuffle, then 5x more, then 30x of the Foreground/Signal/Cleanup/Secret tests at -cpu 1,4. All passed apart from one failure in a test this branch did not change (finding 9). Coverage: engine 100%, tokens 99.2%, ui 96.7%, execx 94.4%, all above the 90% floor. No `nolint` or other suppressions, every package has its doc, and the only paths are `/home/user` and `/home/u` placeholders.

**Blocking**

1. **`FixArgv` returns a find command that cannot run.** `src-go/internal/engine/relabel.go` (`FixArgv`, `FixCommand`): `FixArgv` returns `find DIR PREDS -exec chown -h U:G {}` with no closing `+`. Only `FixCommand`, the text shown to the person, adds it. Run as returned, find fails with "missing argument to `-exec'", so the relabel fix (bash's `sudo find ... -exec chown -h ... {} +`) could never succeed. The package doc's E8 says "the fix being exactly the argv that runs", which is false. `relabel_test.go:137-141` checks that the last five words end at `{}` and compares the fix against `quoteAll(append(argv, "+"))`, so the test locks the defect in. Fix: `FixArgv` should include `+`, `FixCommand` should quote it unchanged, and the test should assert the full argv.

**Not blocking (fix in phase 1 or before phase 2's cut-over)**

2. **"A secret is added last, so it wins" is false for `OS.Exec`.** `secret.go` (`childEnv`) only drops `c.Unset` from the parent's environment and then appends `NAME=value`. `os/exec` (used by Run, Foreground and session runs) removes duplicate names keeping the last, but `OS.Exec` passes the list straight to `syscall.Exec` (`exec.go:94-98`), which removes nothing. If the parent still exports the same name (`NewSecret` used rather than `TakeSecret`), the exec'd child gets two entries, and glibc `getenv` returns the first, the parent's. Fix: strip the parent's copies of each secret's name (and of each `Env` key) in `childEnv`.

3. **Secret leak review.** I found no leak from this code:
   - `String` and `GoString` redact, and fmt calls them for the exported `Cmd.Secrets` field under `%v`, `%+v` and `%#v`.
   - The refusal error names the variable, not the value (tested).
   - `Fake` stores the Cmd, but only redacted printing reaches it.
   - Foreground leaves `Env` nil, so the child inherits the parent's whole environment, as bash did. The "never exported to unrelated children" decision therefore rests on callers using `TakeSecret`; phase 2 should test that.
   - The substring check on argv fails closed: a very short value refuses harmless commands, which is acceptable.
   - Two residual points to note in docs: `/proc/self/environ` keeps the original value after `Unsetenv`, and a secret necessarily reaches the child's own children. src-go/CLAUDE.md's rule "A password goes to its consumer on stdin" should now mention `Secret`.

4. **`ui.Lines` takes no context.** `ui/lines.go` `readLine` blocks on `ReadString`. In the launcher, `SignalContext` catches INT/TERM/HUP/QUIT, so a Ctrl-C or SIGTERM during a Lines prompt cancels the context while the read blocks until input arrives. `SignalContext` also keeps catching every later signal until `stop`, so a second Ctrl-C does nothing. Bash's `read` returns on the trap. Before phase 2 wires Lines in, add a context-aware read or document closing stdin on cancel.

5. **Foreground and SignalContext together.** Both register for SIGINT, and Go delivers a signal to every registered channel. While Foreground "outlives" a SIGINT, an active `SignalContext` cancels the flow, so when the child exits the main returns 130 instead of the child's status. Low impact with `podman run -it`, because a raw-mode tty turns Ctrl-C into a byte rather than a signal. Phase 2 should state the intended combination.

6. **The `Engine.cmd` exemption row (`qa/rules/table.go:128`).** This follows the documented mechanism (GoProjectRules.md: a `dynamicNames` row with its reason), and sessprobe already has two such rows for the container engine, so it is not a weakening in itself. But the reason, "chosen by engine.Select from its own two-name table", is not enforced. `Engine{Kind: Kind(s)}` can be built from any string, and `ImageInspect`, `Containers`, `Remove` and `Networks` never check `Kind`; only `Rootless`, `SELinuxMode` and `UIDMap` do. Have `cmd` refuse any kind other than Podman or Docker, or build an Engine only from a `Choice`, so the reason holds.

7. **Duplication.** `internal/wsitoken` (K3) already parses `NAME.YYYY-MM-DD.token` with its own regexp, and its rules contradict T2 and T5: a file dated today counts as usable, and the newest file wins. That gives two Go readers of the same pool with opposite rules. Phase 1 needn't fix it, but there should be a follow-up to make wsitoken use `tokens`. `engine.Containers` overlaps `sessprobe.RunningContainerNames`, which is acceptable since they serve different owners. `ui.Lines` beside `ui.Gum` is justified by decision D5 and lives in `ui`, but uses the opposite colour flag (`Colour` against Gum's `Plain`), which is a small inconsistency. src-go/CLAUDE.md's layout list does not name `engine` or `tokens`.

8. **Parity: no regression for real token files.**
   - Every path in bash writes `<name>.<date>.token`: `create_token` (`token-management.bash:1205`) and the import (`:1895`). The date comes from `date -d +90days`, and names are limited to letters, digits, `_` and `-`. All of those still parse as dated under T1, so no existing file is newly rejected.
   - Only invented forms (`work2030-06-01.token`, impossible dates) change, and both are declared.
   - The `--list-tokens` text matches `list_tokens`, including the undated-file line.
   - Every case of `rootless-guard` and `selinux-verdict` is ported one for one. E6 (docker report unreadable, so relabel) and the stricter uid map (a range of zero uids is refused) both fail toward the safe side.
   - `Decide` matches `select_token`: missing directory 1, empty or expired-only pool 3, host with a missing or empty pool goes to the Desktop account.
   - Nit: the T7 doc calls the missing directory "a usage error", but the code exits 1 (`term.ExitFailure`, `pool.go` `Decide`), as bash does. Fix the wording.
   - Small undeclared difference: bash `--token NAME` globbed `NAME.*.token`, undated files included, while Go `Earliest` takes dated files only. Harmless, but T5 should say so.

9. **Pre-existing flake, not from this branch.** `TestSessionDeadlineKillsTheGroupAfterTheShellExited` (`execx/session_test.go:243`, file unchanged here) failed once: "read <pid>/stat: no such process". Its `dies` check treats a process already reaped (ESRCH) as an error. It passed in all later runs. It should be filed separately.

10. **Signals and process groups.** The Foreground tests use real `sh` children and real signals, and they are sound:
    - The process-group test checks the child and its own child.
    - TERM and HUP are forwarded, and a death by signal is reported as 128+n.
    - The INT test would fail if the INT had been forwarded, because `got-int` would come before `line=done`.
    - `Notify` is registered before `Start`, the wait goroutine always ends, and no terminal state changes because the group is never changed.

The fix needed before merge is finding 1. Findings 2 and 6 are a few lines each and worth taking in the same commit.