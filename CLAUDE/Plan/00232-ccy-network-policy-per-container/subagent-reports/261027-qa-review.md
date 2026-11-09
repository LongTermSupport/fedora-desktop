**Verdict: FIX-BEFORE-MERGE.** Nothing blocks the merge outright, but there are seven should-fix findings. I did not write `subagent-reports/261027-qa-review.md`, because my role forbids changing the repo. The full report is below. One disclosure: I redirected the QA output into `untracked/qa-all-review.log` in the worktree; it is gitignored, but it is a write I should not have made.

### Should fix
1. **`ccy --connect` and `--disconnect` exit 1 while any `off` session is running.** On the unprotected path (`app.go:466-483`) no `state.json` is written, and `refreshOne` (`app.go:389-391`) fails on the missing file. The connect itself has already happened by then. A Docker session's directory is deleted outright (`app.go:380-381`), including its mounted context file. No test covers this.
   - Fix: record the session's state too, have the refresh skip any session that is not enforced, and test it.
2. **The acceptance gate can pass without measuring anything.** `check_blocked`, `check_allowed` and the control loop in `acceptance-checks.bash` read the `RESULT` lines through process substitution, so the probe's exit status is lost. If no lines come back, the check passes.
   - Fix: require the number of `RESULT` lines to equal the number of targets.
3. **The acceptance never exercises `--connect` or the refresh.** The refresh's `AllowUpdate` and `VerifyAllow` have only been tested against fixtures, as the handoff itself notes. The owner named this path.
   - Fix: add a check that connects a session to a network, refreshes, reaches a peer on it, then disconnects.
4. **`meta-deploy.list` is wrong about order.** The `changed-plays` entry (line 47) already runs `play-claude-yolo.yml` before the 00232 entry (line 91). So the policy is live before 00232's "triage before" run, which the entry's comment claims comes first. The `changed-plays` description also does not tell the owner that every later session will run under a network policy.
5. **IPv6 neighbour discovery may be blocked (not measured).** `policy.go:381-395` refuses all output to `fe80::/10`, with no exemption for neighbour discovery. A pasta (`--no-network`) session whose IPv6 gateway is a link-local address could lose IPv6. The triage never probed this.
   - Fix: probe it first, then exempt neighbour solicitations and advertisements if needed.
6. **A failed apply blocks every launch with no way out named.** The message from `notStarted` (`app.go:499-501`) does not mention `CCY_NETWORK_ALLOW=off`.
7. **Tokens now reach every child the command starts.** The Claude and GitHub tokens and the provider keys are inherited by `podman network inspect`, `podman inspect` and `podman unshare nsenter nft`. Before, only the engine's run got them, and `src-go/CLAUDE.md` requires `TakeSecret`.

### Plan drift
- The 00232 row in `CLAUDE/Plan/README.md` (line 39) still says "host triage pending, design awaiting review".
- Neither set of tests was seen failing first. The Go tests' only red was the package not compiling, per the journal entry `174938Z`, and the worker says the bash suite tests were written with the change.

### Nits
- `context.go:77` replaces every "lxc" in the entry text, so a host name containing it gets mangled.
- The podman calls before start have no timeout or signal handling. A container left half-made is removed by the launcher's `podman rm -f` before the next run.

### Checked and clean
- **Version and deploy:** `CCY_VERSION` is 3.101.0, the new lib is staged, and the container version correctly stays the same. The new lib is listed in both `CCY_LIBS` and the image tasks, and the play declares `nftables`.
- **Launcher wiring:** the Go front does not handle `--connect` itself, so the refresh is reached. The difftest goldens were re-recorded.
- **Acceptance path:** it runs the real launcher with `--teams`, checks pingbus both ways, and its teardown parks the seats and restores the Quick Launch record.
- **Public-repo leaks:** the added lines contain only RFC 5737 addresses and `/home/u` test fixtures.

### Mechanical gates
- `qa-all.bash`: passed, 1731 files.
- `plan-qa --sweep`: 0 blocking findings, none for 00232. It swept the main checkout, not the worktree.
- `--syntax-check` on `play-claude-yolo.yml`: OK.
- `go test` on `internal/netpolicy` and the command: OK. `test-ccy-agent-choice.bash`: 75 passed.
- `qa-helper-tests`, the extension compatibility check and ESLint: not triggered by this diff.