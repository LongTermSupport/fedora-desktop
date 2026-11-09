## 16:22 · action · —   — CI run 1 of the Go tests: the bind-mount test skips where the runner denies the mount

The first CI run of the Go tests on PR 124 (run 41660212107) had one Go failure left after the root-only causes were fixed: `TestWriterRefusesABindMount` in `internal/teamchat/host/lxc/guestroot`, `bind-mount: permission denied`. Every detector stage passed, and so did gitleaks and the branch-contains check.

Cause, as far as it can be established from here: the test makes a mount inside a user namespace of its own (`reexec.StartInUserNamespace`, helper `bindMount`). The runner lets the process create the namespace and then refuses the mount in it (EACCES, the text of the failure), which is how a restricted runner behaves. `StartInUserNamespace` skipped a kernel that refuses the namespace, but a helper that found it could not do its work and exited non-zero was a failure. I could not reproduce the denial here (the agent container allows the mount, and `chmod` on the mount source did not force it because root in a user namespace overrides file modes), so the cause is a hypothesis from the error text, not an observation.

The fix is at the seam, test first. `reexec.ExitSkip` (77) is an exit code a helper ends with when its environment cannot do its work; `StartInUserNamespace` turns exactly that into `Skipf`, showing what the helper wrote to stderr. Any other early end, a signal, and a failure to run still fail the test (`TestOnlyTheSkipExitIsASkip`); `TestAHelperThatCannotDoItsWorkHereSkipsItsTest` holds the positive case. `bindMount` returns `ExitSkip` only on EACCES or EPERM from the mount call itself.

The cost, stated in `CLAUDE/GoProjectRules.md` under the classes that cannot be a rule: where the runner denies the mount, CI never runs this test, and it is the only test that shows the kernel refusing a write through a bind mount (`RESOLVE_NO_XDEV`). Whether CI should allow the mount (a runner setting) or the skip is accepted is the owner's question on the PR; I have not narrowed the test to make it pass.

Verified: `golangci-lint` 0 issues; `go test -race` on `reexec` and `guestroot`.
