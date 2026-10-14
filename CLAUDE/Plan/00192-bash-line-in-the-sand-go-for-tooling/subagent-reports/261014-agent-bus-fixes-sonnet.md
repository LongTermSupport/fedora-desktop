# agent-bus-install: fixes for the Fable review (Plan 00192, Task 3.2)

Branch `go-port/agent-bus-install`, after merging F44 (one conflict: both sides appended a
journal entry; both kept). Review: `261014-fable-review-abi.md`. Not pushed, not merged into F44.

## What was done, by finding

1. **Umask.** The process-wide `umask 077` is gone. `main` became `execute()` plus an exit, and a
   test sets umask 022, runs `execute()` and asserts it is still 022 afterwards (red first: it saw
   077). Every installer write already carries an explicit mode on its descriptor, so nothing
   else needed it; the children (`dnf`, `useradd`, `zstd`, the bundler) inherit the caller's umask
   as under bash.
2. **B12/B13 docs and cases.** ACCEPTANCE section 3 now says both were fixed (`AddressesMatch`,
   `ParseZone`) and what the host probes confirm. Two suite cases added (309 cases, all pass):
   an IPv6 `--bus-address` second run prints no `CHANGED` and runs no `nmcli modify` (the nmcli
   stub now escapes colons like terse mode does), and a `zone-of` stub that answers `no zone`
   with exit 0 still puts the rule in the default zone. SPEC B12/B13 rows say FIXED.
3. **Trust claims.** Play header, `docs/agent-bus.md`, `docs/playbooks.md`, ACCEPTANCE, PLAN,
   `deploy.bash`, the SPEC and the cut-over report now say what narrowed: root runs the installed
   binary, not a checkout script. Stated plainly as remaining: the bundler is checkout Python run
   as root, the admin wrapper is copied from the checkout and run as root later, and the binary is
   copied from the user's build cache. I did not narrow further: running only the bundler as the
   user would not close anything while root still installs and runs the checkout's wrapper and the
   zipapp it builds, and the bundler's changed/unchanged marker compares against root's output.
4. **Drift defence restored, generalised, without growing bash.**
   - `tasks/build-go-command.yml` now computes each command's source digest
     (`installer_digest.py --command <name>`, new `command` parameter) and always adds
     `-X main.SourceDigest=`; the agent-bus play's own digest task and ldflags are gone.
   - Every command's `main` has `var SourceDigest` and answers `--version` through the new
     `internal/srcdigest` package (ccy-teams, ccy-sessions, podfreeze, lxcfreeze, installer).
   - `helpers/go_drift/check.py` + `scripts/go-binary-installs.tsv` (command, installed path,
     play) compare each installed command's `--version` with the checkout's digest; a command that
     cannot report one is drift; tests also check the table against `src-go/cmd`, the plays and the
     mains. `qa-deployed-drift.bash` calls it in one statement. The script's bash-limits record
     went down (two redundant initialisations and a trailing `exit 0` paid for the call), so the
     ratchet was kept. Exit 2 (a check that could not run) also fails the gate, with the helper's
     message on stderr.
   - Not exercised on a real host here (the gate self-skips in a worktree and the container).
5. **Bash-limits loophole.** A gone file beside an exempt twin is excused only when
   `src-go/cmd/<name>/main.go` exists AND the row of `scripts/go-binary-suites.tsv` for `<name>`
   names that twin. Tests: stub command with no row, row naming another suite, row but no command
   all stay orphans. Residual: someone who adds both a stub command and a table row passes; the
   table row is a reviewed, test-checked file.
6. **Seam rule.** The silent return for wrappers in any excused function is reverted. A program
   runner's own body (a function registered in `programRunners`) is the one place a wrapper's
   hidden program is excused, because its callers are judged. `RunTransient` takes the program as
   its own parameter, is registered with argIndex 3, and its `dynamicNames` row is gone. Fixtures:
   `Unexcused` and a row-excused non-runner stay red, a runner's body is clean, call sites flag
   an owned program and a non-constant one.
7. **`tarstate.Write(w, *os.Root, limits)`**; `backup.go` passes `teamFS.Root()` (new `fsx.FS.Root`
   accessor); a test swaps the directory name for a symlink after opening the root and still reads
   the original.
8. **Digest refusal first** in `team()`, before root, Fedora and software checks; the test asserts
   no host program ran and nothing was created.

## Nits

- `makeDir`: `os.Root.OpenFile` follows a symlink at the last component whatever `O_NOFOLLOW`
  says (measured: a test with the flag still followed), so the fix is an `Lstat` after `Mkdir`
  passed as the `before` the open must match.
- The production-build test no longer leans on the `MkdirAll` side effect: it asserts the refusal
  names the real path (root) or is the not-root refusal, and that the prefix never reaches stderr.
- `curl -q` on the download and the readiness probe; the hostile-name message is `%q`-quoted
  (`port N is held by team "x"`), with a test for a newline and an escape in the name.
- Injected-failure sweep: the team file has `allow_from`, so `firewall-cmd` calls are swept;
  `--query-rich-rule=` is a result probe, and for `check` it must be a reported FAIL fact.
- SPEC section 11: B22 (getent status), B23 (pin and resolver order), B24 (quoted name), B25
  (`curl -q`).
- Not done, noted: the Go harness still carries its own homeserver unit (`hsUnitText`); the restore
  tests pin properties of that fixture (`ExecStartPre` and so on), so switching to the real unit
  means rewriting them. `src-go/CLAUDE.md` now says which simulator is the oracle for what. The
  `play-golang.yml` import stays in each play (every Go play imports it; idempotent).

## Checks run

`qa-go.bash` (clean, project rules clean over the real tree), `qa-bash-limits.bash`,
`test-qa-changed.bash` (74), `test-agent-bus-install.bash` (309), the agent_bus and go_drift
Python tests (416), `--syntax-check` of the agent-bus, claude-yolo, podfreeze and lxcfreeze plays.
`qa-changed.bash --base F44` is green except shellcheck SC2034 warnings in
`CLAUDE/Plan/00161-agent-team-bus-matrix/{acceptance,deploy}.bash`, which the branch touched
earlier (not by this work); qa-all excludes plan scripts from that severity, qa-changed does not.
Two items were fixed test-after-code (7 and 8 were edited before their test was run red); both
tests assert behaviour the old code lacked (verified by reading the old paths).

## Host step

None new: `meta-deploy.bash` already runs the plans' plays; the first run rebuilds the five Go
commands with a digest, after which `qa-deployed-drift.bash` on the host checks them. A command
built before this change reports no digest and shows as drift until its play has run.
