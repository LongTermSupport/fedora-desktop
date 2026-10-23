# Adversarial review r1: unknown team, child exit codes, cc KIND (branch agent-a8c69aa67110156ce-c48195fd)

Scope: `git diff F44` over the working tree, which is mid-merge with F44 and has all conflicts resolved. The review was read-only.

## What was run

- `go build ./...`, then `go vet` and `go test` on `internal/teams/...`, `internal/term`, `internal/agentbus` and `internal/sessions`: all pass.
- `go test ./rules/` in `src-go/qa`: passes.
- The project rules driver, run as `go vet -vettool` over the whole merged `src-go` tree: no findings. The code F44 brought in does not trip `project_child_exit` or `project_row_kind`.
- `scripts/test-ccy-teams-command.bash`: 383 passed, 0 failed. `scripts/test-cc-teams.bash`: 83 passed, 0 failed.
- Added lines were scanned for install-specific names. The only handles are placeholders (`proj.2+box.host`, `<team>.2+local.host`). No leak.

## Checked and sound

- **Headless stays fail-fast.** `chooser.Run` refuses a team named alone with no terminal (exit 64). The launcher only calls the chooser when someone is at the terminal. Unattended launches go through `agent-bus seat check --resolved`.
- **Leaving the new question is quiet.** Esc on the question exits 0 with an empty payload. Both the ccy launcher and `cc-teams.bash` read an empty payload as "left, exit 0". This matches the chooser's existing contract.
- **Retries are bounded.** The question comes back at most 3 times (`maxRounds`), then exits 1 with a message.
- **The wizard offers the typed name.** `RunForChooser(ctx, name)` puts the typed name in as the Team name. `declare.Check` still validates it, and `named` is reset on a plain `Run`.
- **Exit-code translation.** `term.FromChild` and `term.Explain` are correct and tested. The 13 `Kind == KindCcy` to `IsSession` swaps on seat rows keep their old behaviour.
- **Version bump and changelog.** CCY 3.100.22 is above F44's 3.100.18, and 3.100.21 exists on another branch. The changelog order is right.
- **Plan and journal.** Tasks 7.6 to 7.9, the status line and the journal correction agree with each other. `changed-plays` in meta-deploy covers `play-claude-yolo`.

## Findings

01. **medium: `IsSession` now fires on member rows too, and the catalogue says it doesn't.**

    - **What changed.** `agentbus.KindOf` makes every roster member with a `.host` handle `cc`. That includes the member rows `snapshot.go:292` builds with `Dir "-"` (for example a cc session on another desktop). So `Kind.IsSession()` is now true for rows that used to be kind `host`.
    - **The bug.** `seats.SnapshotRunning` (role.go:114) checks `row.Kind.IsSession()` without `row.Dir != "-"`. A host-cc coordinator with no seat row here now reads "(not running)". It should read "(whether it is running is not known here)", which is what that function's own doc promises. This already happens for podman members on F44.
    - **Other behaviour changes.** Host member rows now get the unavailable Attach line, and the Incomplete branch of `RemoveOrPark` (it refuses to park them).
    - **The wrong claim.** GoProjectRules.md and the DBF report both say "`IsSession` is true for exactly those rows". That is false.
    - **Fix:**
      - Guard SnapshotRunning with `row.Dir != "-"`.
      - Add a test for a host member with no seat row: it should read Unknown.
      - Correct the catalogue sentence and list the member-row changes.

02. **medium: the unknown-team options and the swap list are built from the original list, not the current one.**

    - **Example 1.** `--teams nosuch,other` where neither exists. Choose "Start on other without nosuch", and the next question offers "Start on nosuch without other". Taking that leaves nothing, so the run says "No team chosen" and exits 0.
    - **Example 2.** Swap one unknown for beta with `others(declared, items)`. A second unknown can then be offered beta again, which gives a duplicate team in the payload.
    - **Fix:**
      - Pass the list as it stands to `known`, `askUnknown`, `unknownOptions` and `others`: the refs so far plus the items still to come, without the ones left out and with each swap included.
      - Add tests for two unknown teams and for swap then swap.

03. **medium: `ccy --help` still says nothing about this.** `src-go/internal/ccylaunch/help.txt` has no line on the unknown-team offer and none on the `--teams` exit codes (64, 75, 78). Request 2 is about what the shell gets from `ccy`, and the owner ran `ccy`. Only `docs/ccy.md` and `ccy-teams --help` were updated.

    - **Fix:** add to the `--teams` entry that an unknown team is offered for creation on a terminal, and which exit codes `--teams` can end with. Update the difftest golden for the help text if it pins it.

04. **medium/low: the bash launchers still pass agent-bus's raw exit codes to the shell.**

    - **Where.** `ccy_seats_check` and `ccy_seats_take` (`return "$rc"`), then `exit "$rc"` in `claude-yolo`. `cc_teams_choose` does the same in `cc-teams.bash`.
    - **The gap.** A code agent-bus does not document there (1, 2, 69, 126) reaches the shell untranslated. The DBF sweep left bash out on purpose ("they read the code they are given"), but request 2 has no such exception.
    - **Fix:** map anything other than 64, 75, 77 or 78 to 1 in those helpers, and pin it in `test-ccy-agent-bus-seats` or the cc-teams suite. If not, record the decision to leave it to the owner.

05. **low: a malformed team name now gets the create offer instead of a usage error.** On an attended launch the chooser runs before `agent-bus seat check`. `known` only asks systemctl, and every non-zero exit there reads as "down", so `--teams Foo_Bar` gets "This machine has no team called Foo_Bar / Create team Foo_Bar now". Before, `agent-bus seat choose` refused it with the grammar reason.

    - **Fix:** check the team grammar first, with `agent-bus seat check` without `--resolved`, or `declare.Check` on the name. Refuse with 64 before asking.

06. **low: a team made from the unknown path is not checked against this checkout.** `list.go created()` refuses a new team that does not cover the checkout. `answerUnknown` takes it with no check. The two paths also disagree on `ExitOK` with no team: list.go calls it a failure, unknown.go asks again.

    - **Fix:** share one check, or record why a named team skips the coverage check (named teams never had it).

07. **low: "Choose one of this machine's teams instead" can offer a team whose homeserver is down.** Picking one goes straight into agent-bus's exit 78.

    - **Fix:** mark down teams the way the list does (`entries()` already computes `down`), or leave them out.

08. **low: a team declared but absent is taken as known.** `Declared` includes entries with `Present` false, so such a team fails with 78 as it did before.

    - **Fix:** treat `!Present` as not on this machine, or offer `ccy-teams apply`.

09. **low: the 00191 SPEC is out of date.** `SPEC-ccy-teams.md` line 766 still says "KIND = type with podman shown as `ccy`" with no mention of host shown as `cc`. Divergence 06 (line 938, seat KIND hard-coded `ccy`) is not marked fixed.

    - **Fix:** update both lines.

10. **low: nothing tests the built binary on this path.** No black-box suite step runs `choose-teams` with an unknown team through the built binary. Only the chooser rig's unit tests cover it.

    - **Fix:** add one `test-ccy-teams-command.bash` case under a pty, or accept the unit tests plus the owner test in Task 7.9.

`--teams <seat>@<unknown>` on an attended launch still exits 78. That is properly referred to the owner; it is not a finding.

VERDICT: CHANGES
