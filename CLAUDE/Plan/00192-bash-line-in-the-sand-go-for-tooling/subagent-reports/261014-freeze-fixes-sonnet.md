# Freeze port: fixes for the Fable adversarial review (branch go-port/freeze)

Review: [261014-fable-review-freeze.md](261014-fable-review-freeze.md). Each fix has a test that
failed first (compile failure or an assertion). Nothing pushed, nothing merged; no Ansible run
(syntax-check only).

## Blockers

- **B1 sudo readiness.** `sudo.Sudo` has an `OpenTTY` seam; `Ready` decides "can ask" by whether
  `/dev/tty` opens (nil seam means no terminal), not by stdin and stderr being terminals. `New`
  uses `/dev/tty`; `teams` passes its own `OpenTTY`, `lxcfreeze` the `Env`'s. Tests: stdin and
  stderr not terminals but `/dev/tty` opens -> `sudo -v` runs; nil seam -> refused. The golden
  scenario `lxc-sudo-needs-password-no-terminal` holds its intent unchanged (no controlling
  terminal in the capture, so still a clean refusal with the same stderr). The ccy-teams suite
  (323) and the teams/sessions Go tests are green. `ACCEPTANCE-freeze.md` no longer redirects
  stderr (it is rewritten around S3).
- **B2 Ctrl-C mid-batch.** `actOn` checks `ctx.Err()` before each act and after a failed act; a
  cancelled batch returns `context.Canceled` (not `ErrSomeFailed`), prints the thaw hint for what
  did succeed and one line `<tool>: interrupted — N of M container(s) done, the rest were not touched.`, exit 130. `pass` checks the context before a refresh, so the menu ends instead of
  dying on `podman ps`. `lxcfreeze` thaw: a cancel during the DHCP renewal returns the context
  error, never "thawed, but ... nmcli failed: context canceled". Tests: mid-batch cancel, already
  cancelled, the menu loop (no refresh, no second menu), refresh skipped, the lxc renewal.

## Should-fix

- **S1.** `programRunners` table (`sudo.Sudo` `Capture`/`AsRoot`/`Run`, lxcfreeze `runAct` and
  `probe`) checked at `CallExpr` through the same walk as an `execx.Cmd` (constant, owner table,
  wrappers, shell `-c`); spread slices must show a literal head or the function needs a
  `dynamicNames` row (four rows added with reasons, three of them for helpers that take the
  program from their callers); a runner taken as a value or method expression is flagged;
  `lxc-attach` is a wrapper (`--`) whose inner program is judged even for its owner. Fixtures
  `badrun` and `lxcfreeze/runs.go` were red first. **The review's "expected zero findings" was
  wrong**: the new rule found `renew.go` building the `lxc-attach ... nmcli` argv in a variable and
  spreading it into `Capture`; the argv is now spelled in the call. Control plant
  (`Capture(ctx, "nmcli", ...)` in `internal/freeze`) reported and removed.
  `CLAUDE/GoProjectRules.md` updated (rule text, fixtures, real-tree result, control).
- **S2.** An act that meets `sudo.ErrNeedsPassword` returns a `freeze.Die` ("sudo's password
  timestamp lapsed part-way through ..."); the batch stops at it (any `*DieError` from an engine's
  `Act` is a batch-ending fatal), the thaw hint still names what was done, one message instead of
  N. Tests at both levels, for each of the four lxc commands.
- **S3.** `deploy.bash` saves the bash tools' `list` and `freeze --all -n` stdout and stderr to
  `untracked/freeze-acceptance/before-*` before the first play, only while the installed tool has
  an interpreter line (a compiled tool keeps the earlier baseline; a bash tool that fails stops the
  deploy before it is replaced); skipped under `--check`. New `acceptance.bash` (built on
  `_planlib.inc.bash`, `gather` mode) checks both tools are ELF and `~/.local/lib/freeze` is gone,
  runs the same four commands and compares stdout, keeping stderr and any diff in the run
  directory; a missing baseline is NOT ESTABLISHED and fails. `deploy.bash` ends with it as a leg
  (R9). `ACCEPTANCE-freeze.md`, `meta-deploy.bash` comment, `PLAN.md` updated. Not run (host
  only); the ELF and interpreter-line probes were exercised in a scratch script. No
  install-specific names.
- **S4.** `Inventory[R]`, `Entry[R]` (field `Record R`), `Engine[R]`, `Session[R]`,
  `app.Tool[R]`/`TargetEngine[R]`; `Entry.Data any` and both `recordOf` functions are gone, so
  there is no silent zero record.
- **S5.** `Config.Validate` runs at the top of `app.ExecuteClosing`; test with a blank state word.
- **S6.** The `PICKED=() PICKED_WHY=()` fold in `scripts/qa-changed.bash` is undone (two
  statements). The if-fold alone keeps it under the merge-base record (code lines 543 vs 548,
  statements 587 vs 588), so nothing else was cut. The allowlist line is 543/18/587: that is up
  from the previous commit's 542/586, down from the base's 548/588; `qa-bash-limits.bash` accepts
  it, since it compares with the merge-base.
- **S7.** The count is 79 identical, 57 differ (the wave-4 report said 82/54; corrected), and
  SPEC 2.1 now lists the lxcfreeze usage line change ("You will be prompted once per run" becomes
  "sudo is asked again if its timestamp has expired") as a bug 6 consequence.

## Nits done

- Typed `cli.Mode` with per-tool constants for the target flags.
- `app.RunContext` replaces the duplicated "Execute then ExitCode" line in both tools' `App`.
- `test_affected_plays.py` fixture names are neutral (`shared-lib`).
- `tasks/build-go-command.yml` says `argv` is a list.

## Nits not done

- N2 menu keys (`net:`/`id:`/`bridge:` strings): a structured key changes the `Chooser` interface
  and about ninety test references for a parse that lives in one place per engine, with unknown-key
  tests. Left as is.
- N3 (iterator over entries), N4 (concurrent lxc probes), N5 (`Term{Plain}`), N6, N7, N10: not
  asked for.

## Verification

`scripts/qa-go.bash` (fmt, vet, `-race` tests, 95.0% coverage, golangci-lint, project rules, qa
module) green; `scripts/qa-bash-limits.bash` green; `scripts/test-qa-changed.bash` 73 passed;
`scripts/qa-changed.bash` and `--base <branch start>` green (including `test-ccy-teams-command.bash`
323 passed); `scripts/qa-ansible-syntax.bash` (all playbooks, the three plays included) green.
`qa-all.bash` not run, as asked.
