# Plan 00201 M6 review (Fable): `cc --teams` on the shared flow, consent in Go

Reviewed `git diff F44...agent-a86fea012b7088174-17f3111c` (commit 022045ac) against
SPEC-many-teams.md S-E1 to S-E6 and the builder's report
`261015-m6-cc-teams-sonnet.md`. Read-only; evidence was gathered by reading the code and
running `semgrep`, `scripts/qa-bash-limits.bash` and `go test ./internal/teams/...` on the
branch.

## Checks that pass

- **Consent cannot be granted without gum exiting 0.** `consent.go:69-77`: gum exit 1 is a
  decline, any other non-zero (130 on Ctrl-C) is `ErrCancelled`, both record nothing and
  return 1. `--default=false` is an explicit argv flag, so a `GUM_CONFIRM_DEFAULT` in the
  environment cannot flip it; `GUM_CONFIRM_TIMEOUT` would resolve to the default, No. EOF on
  stdin makes gum fail, not succeed. The record is written `O_EXCL|O_NOFOLLOW`, 0600, into
  a folder checked `Lstat` (not a symlink, 0700). A symlinked folder or record is refused 78
  before anything is read through it. The gum process runs with `Stdin: g.Stdin` and
  `Stderr: g.Stderr`; stdout is the captured payload, so the chooser's `$()` capture in
  `cc_teams_choose` sees only the list. Ten Go tests cover accept, decline, cancel, already
  recorded, podman never asks, several handles, order (after the list), unofferable seat,
  symlinked record, symlinked folder, no gum.
- **Parity.** Same grammar (`cc_teams_split_args` mirrors ccy's "an option where the value
  belongs means it was left out"), same `choose-teams` entry (multi-select and widening are
  the M4/M5 code paths, unchanged: widening still happens only on the `add` answer in
  `widen.go`), `describe-seats --block` for the launch block, every message on stderr, and
  `scripts/test-cc-teams.bash:125,173,218` pin "nothing on stdout".
- **Ratchet and parse.** `scripts/qa-bash-limits.bash` exits 0 with the allowlist row at
  113/9/170 and the `ui` mark gone. `semgrep --lang bash` parse errors: old `cc-teams.bash`
  1, new 0; `cc` 0 before and after. `go test ./internal/teams/...` passes.
- **Spec drift that is honest.** Deviation 4 (Ctrl-C is a decline, exit 1) is what S-E4
  asks for ("Declined ... exit 1") and is what the tests pin. Deviation 5 (prompt sentence)
  matches S-E5's example.

## Findings

### F1. A readable pre-existing record is "accepted" by Go but refused by pingbus — seats taken, then the session refused (HIGH)

`src-go/internal/teams/chooser/consent.go:101-121` (`recordState`) treats any regular file at
`~/.config/pingbus/element-accepted` as consent. `helpers/pingbus/config.py:201-220`
(`element_accepted`) additionally demands `mode & 0o077 == 0` and owner uid on the file.
Scenario: the owner (or a hand-written `touch`) leaves a 0644 record. `choose-teams` prints
the list without asking, cc runs `seat take` (sudo, a new member and token), and
`pingbus seat exec` then refuses the session beside Element. That is the exact "member made
without consent" ordering S-E4 forbids, reached through the happy path. Fix: `recordState`
must apply pingbus's rule (regular, owner uid, no group/other bits) and refuse 78 with
"chmod 600 it, or remove it", so the two readers agree; add a Go test with a 0644 record.
The bash test `test-cc-teams.bash:286` pins only the path, not the rule.

### F2. Deviation 1 is a fail-fast violation, and the unattended branch is barely reachable (MEDIUM)

`files/var/local/claude-code/cc-teams.bash:60-72` (`cc_teams_choose`): with no terminal the
list is checked `--resolved` and `cc_teams_take` runs before any consent check. `cc:41`
already refuses when stdin or stdout is not a TTY, so the branch is reached only by
`cc --teams 1@t 2>file`, where gum would be invisible anyway. In that state cc makes the
member, then pingbus refuses at exec: a loud failure, but after the irreversible step, and
contrary to the file's own header ("a declined start leaves no member"). Fix: cc should
always call `ccy-teams choose-teams --type=host LIST`, and the Go consent should decide
attendedness itself: with no record and stdin or stderr not a terminal, exit 78 "the Element
consent needs a terminal; start cc --teams once from one". Then `cc_teams_attended` and the
`--resolved` branch go, cc-teams.bash shrinks further, and the ordering is one code path.

### F3. Open problem A confirmed: the per-session teams file is never removed and can label a non-bus session (MEDIUM, pre-existing from M5)

`files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/hooks/hooks.json` registers
SessionStart, UserPromptSubmit and Stop only. `helpers/pingbus/hooks.py:528-531` has a
working `session-end` branch calling `forget_session_teams`, and
`docs/agent-bus-protocol.md:665` claims "SessionEnd removes it", but nothing fires it;
`tests/helpers/pingbus/test_plugin_contract.py:43-44` even pins session-end as unregistered.
Reuse is real: `claude --resume <id>` keeps the session id. A later plain `cc --resume <id>`
(no `--teams`, no plugin) fires no pingbus hook, and the status line
(`.claude/hooks/handlers/status_line/bus_teams.py`) reads the stale file and shows
`bus: A,B` for a session on no bus. Fix for M7, as the builder proposes: register SessionEnd
in hooks.json (the hook is already there and tested), update the contract test, and have the
status-line reader ignore records older than the session's start or stamp them with a pid
the reader can check. Correct the DESIGN/protocol doc either way.

### F4. Open problem B confirmed on F44: `namesFile` matches "cc" as a substring (MEDIUM, pre-existing)

`src-go/qa/changed/suites.go:195-197`: `strings.Contains(text, filepath.Base(rel))` with
`rel = files/var/local/claude-code/cc` matches "accept", "succeed", "occur". The three scripts
contain 2, 10 and 1 `cc` substrings and zero `cc` words. `scripts/qa-gates.tsv` and
`src-go/qa/changed` are identical to F44, so F44 has it too. Second layer: `loadPopulation`
(`suites.go:25-43`) admits `detail:`-reader gates into the suite population, and
`judgeCount` (`run.go:336-345`) then demands a `passed:` line they never print. Minimal fix,
in `src-go/qa/changed/suites.go`: match the base name as a path token (preceded by `/`,
whitespace or a quote, followed by a non-name character), and in `gates.Population` or
`loadPopulation` keep only rows whose readers include `count`. Both belong on F44 as their
own fix, not in this plan's merge; this branch's builder should not be asked to carry it.

### F5. Deviation 3 is correct, but the open-folder check runs after `MkdirAll` and is not applied on the read path (LOW)

`consent.go:148-157`: `MkdirAll(dir, 0o700)` then `Lstat` and a perm check. For a folder
that already exists 0755 the message "chmod 700 it first" is right (the spec says 0700 and
pingbus checks only uid on the folder). Two gaps: the check is TOCTOU-sized (Lstat then
OpenFile by path, not by dirfd) which is acceptable under the owner's own home, and
`recordState` does not check the folder's perms at all, so a 0755 folder with a 0600 record
passes silently. Fold the folder rule into F1's shared check.

### F6. An explicitly named new seat number cannot pass the consent (LOW, deviation 2)

`consent.go:127-144` (`handles`) refuses any `SEAT@TEAM` that `seat choose` does not list,
yet `seat take` legitimately makes a new number. Only while the record is absent, and the
message says what to do, so acceptable; note it in docs/agent-bus.md next to the grammar.

### F7. meta-deploy.list entry reads as a run-on paragraph (LOW)

`CLAUDE/Plan/meta-deploy.list:17` splices the new M6 text into the middle of the M4/M5
sentence ("...writes its per-session record). `ccy --teams` in a checkout no team covers now
offers..."). Split into two sentences so the owner can read what the run does.

## Verdict

**MERGE AFTER FIXES.** F1 must be fixed before merge: it is a consent bypass in the happy
path that ends in the exact ordering S-E4 exists to prevent. F2 should be fixed in the same
pass (it is the same ordering defect by a different door, and the fix removes code). F3 and
F4 are pre-existing, confirmed, and belong to M7 and to an F44 QA fix respectively; neither
blocks this branch. F5 to F7 are small and may ride along.
