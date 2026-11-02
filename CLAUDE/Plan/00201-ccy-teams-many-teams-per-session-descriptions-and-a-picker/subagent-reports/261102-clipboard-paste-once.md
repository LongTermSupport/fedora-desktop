# Defence Before Fix report: the self-erasing clipboard (Plan 00201 Task 2.12, check 11)

Branch `fix/teams-clipboard-clear-2`, from `58e6f29ec`. Commits: `d4f2a2da3` (the rules, red),
`6e2e41cd4` (every instance fixed, CCY 3.104.16), then the homeserver-line fix in its own
commit (see the end). Not merged; the coordinator merges.

## Specification followed

Method specification 1.1.0, detector specification 1.0.0, toolchain specification 0.2.0, all
from the plugin's cache (`refresh-spec.bash`: SPEC.md cached 1.1.0, project-prompt.md cached).

## Toolchain

No `defenceBeforeFix`/`extra.defence-before-fix` manifest declaration: the project is Go and
bash, with no composer.json or package.json at the root that declares one. The project's own
toolchain is the one CLAUDE.md names: `go/analysis` analyzers in `src-go/qa/rules`, run by
go-qa-ci's driver through `./scripts/qa-go-qa.bash` (and `qa-changed.bash`), with
`analysistest` as the single-rule harness; and semgrep rules in `.semgrep/` run by their
gates (`qa-patterns.bash` for `bash-conventions.yml`), with `semgrep --test` against the
fixture as the harness. Listing: `rules.All()` (the driver test pins it) and
CLAUDE/GoProjectRules.md's table; identifiers resolve to that catalogue (no per-rule pages:
the gap GoProjectRules.md already records). Calibration assumed: every finding blocks, no
exceptions, none recorded.

## Defect and class

Originating instance: `src-go/internal/teams/login/login.go:507` (`wl-copy --paste-once`) and
the texts at `:459` and `:469` ("on the clipboard for one paste; after that it is gone from the
clipboard"). Class: code, tests or text that rely on a clipboard meant to erase itself after a
paste to keep a secret short-lived, or that tell the person a copied secret is gone after one
paste. Hazard: on GNOME (mutter 50.6, host finding 2026-11-02) the desktop reads a new
selection at once and keeps serving its copy after wl-copy exits, so the password stays
pasteable until the next copy while the person was told it was gone.

## Rule

Two halves, one class:

- Go: `project_self_erasing_clipboard` (`src-go/qa/rules/selferasingclipboard.go`, registered
  in `rules.All()`). Flags, outside `qa/rules` and in tests too, an `execx.Cmd`-shaped literal
  running wl-copy with `--paste-once` or a short-flag word holding `o`, or xclip with
  `-loops N` (N not 0); and any string literal or comment group saying `paste-once`, "for/to/
  serve(s)/after one (a single, the first) paste", "the one paste", "gone from the clipboard"
  or "gone after one paste". Message: `self-erasing clipboard: <what>; copy plainly, then clear
  only while the clipboard still holds the secret (project_self_erasing_clipboard)`. Docs:
  CLAUDE/GoProjectRules.md "project_self_erasing_clipboard".
- Shell: `bash-self-erasing-clipboard` in `.semgrep/bash-conventions.yml` (the qa-patterns
  gate), the same flags and phrases over every published shell script in the repo-owned
  trees, comments included.

Excluded as not carrying the hazard: other programs' `-o` (curl), `wl-copy --type`,
`--clear`, `xclip -loops 0`, and "one paste at stop" (the speech-to-text insert, no secret).
Next wider rule considered and not built: the same claim in every published file, markdown
included (a `generic` semgrep rule over what git would publish, as `gh-scope-outside-ssot`
is). Not built because it needs a gate of its own; referred below.

## Proof

- `go test ./rules/ -run TestSelfErasingClipboard` (src-go/qa) over
  `testdata/src/fix/internal/badclip` (bad.go, bad_test.go flagged; good.go clean) and
  `fix/qa/rules` (owner, clean): passes, so every `want` fires and nothing else does.
- `semgrep --test --config .semgrep/bash-conventions.yml .semgrep/bash-conventions.bash`:
  7/7 rules pass, the new section's 6 `ruleid` and 4 `ok` lines included.
- Red on the tree through `./scripts/qa-changed.bash` at `d4f2a2da3` (its own commit, before any
  fix): `go` and `patterns` failed with exactly the findings below.

## Independent search

Dispatched before the rule existed, kept out of `src-go/qa/rules/` and `.semgrep/`; its report
is `subagent-reports/261102-clipboard-independent-search.md` (20 live items, 5 history).
Text search found the full set of `paste-once`/`wl-copy`/`xclip`/`xsel`/`St.Clipboard` uses
and that no other mechanism (no `-loops`, gpaste, tmux buffer, clear timer) exists outside
the journal. Reading found the consent string continued across lines, the Clipboard Indicator
guard resting on the same premise ("would use up the one paste"), acceptance steps that
expect the erase without naming it ("a second Ctrl-V anywhere pastes nothing"), and that no
other clipboard writer copies a secret. Its Go and bash items are all inside the rule's
findings: the wrapped comment ("served to one / paste only", login.go:431) made the rule read
a comment group as one text with `\s+` between words, before the red commit. Its markdown
items (docs/agent-bus.md:60 and :953-957, ACCEPTANCE-owner.md check 11, PLAN.md Task 2.12)
are outside both rules' languages; they were fixed by hand. Old changelog entries, journals,
subagent reports and DONE-tasks are history and were left; the new changelog entry says the
earlier "serves one paste" no longer holds.

## Sweep and count

Scope (the project's decision recorded here, it had none): every Go package of both modules
(`src-go`, with `-tags ccyteamstest,difftest` too, and `src-go/qa`), vendor excluded; every
published shell script under files/, scripts/, helpers/, extensions/, fedora-install/ and
CLAUDE/Plan/ (545 files). Python and extension JavaScript were searched by hand: no instance.
Count: 13 Go findings on 12 lines (login.go 7, password_test.go 6; none in `qa`) and 2 shell
lines (`scripts/test-ccy-teams-command.bash` 1029, 1031): 15. No narrowing.

## Fixes

All 15 examined individually (two files and one suite section), none by pattern:

- login.go: the copy is plain `wl-copy` (stdin). `SettleClipboard`, called once Element has
  been offered (OpenElement deferred, PasswordRow before the list, SignInGap deferred, the
  create wizard's `afterCreate` deferred), says "Paste the password into Element, then press
  Enter here (within 45 seconds)", waits for Enter bounded by a constant 45 s context, reads
  the clipboard back with `wl-paste --no-newline` (output kept in memory, compared with
  `subtle.ConstantTimeCompare`, never printed; the password is never in any argv) and runs
  `wl-copy --clear` only if it still matches. Something else copied since: left alone and
  said. A failed read or clear: `ERROR: ... the password may still be on the clipboard; copy
  something else to replace it.` The read and clear run under `context.WithoutCancel`, so
  Ctrl-C still clears. The primary selection is never set or touched. No copy without
  wl-paste. Clipboard Indicator on: still no copy, the line now says the extension keeps the
  password in its history past any clear. The cleared line says GNOME or a clipboard history
  may still keep a copy (whether GNOME serves its copy after `wl-copy --clear` is for the host
  to verify).
- password_test.go and friends, tests first through the fakes (red: build failure on the
  missing `SettleClipboard`, then green): cleared when it still holds it, read back with no
  secret in argv and before the clear, left alone when it changed, timeout (a `synctest`
  bubble with a terminal nobody types on), interrupted, failed clear, failed read, no
  wl-paste, Element started before the read, SignInGap clears, the wizard settles once after
  the password and Element and on a cancelled run.
- The bash suite's fake wl-copy handles `--clear`, a fake wl-paste is added, and the checks
  assert the read back, the clear and the "left as it is" case.

No instance was suppressed or left with the hazard in place.

## Decisions referred to the owner

- The markdown half of the class is undefended: no gate runs a rule over published prose. The
  instances found (4) are fixed, 0 remain; building it means a generic semgrep rule and a gate
  of its own (command, wrapper, qa-gates.tsv, routing and QA.md rows), as gh-scopes has. The
  rule is merged at full width for Go and shell; this extension is the owner's call (SPEC
  section 4: a rule left narrower than the search showed). Pending: the owner's decision.
- Plan 00191's SPEC-ccy-teams.md (the port spec, lines 415-416) still describes the old copy
  wording without the clear; it makes no erase claim, so it is not an instance and was left.

## Permanence

Both rules block from `d4f2a2da3`: the Go rule in go-qa's analyzers step (`qa-go-qa.bash`,
`qa-changed.bash`, CI), the shell rule in `qa-patterns.bash`. Green through the project's own
entry point: `./scripts/qa-changed.bash` passed at `6e2e41cd4` (17 changed files, go and
patterns included). `qa-all.bash` was not run (sub-agents may not); the coordinator runs it.

## Toolchain and detector gaps

No per-rule documentation pages for go-qa-ci's `docs_dir` (already recorded in
GoProjectRules.md; the catalogue stands in). No toolchain-spec listing command beyond
`rules.All()` and the catalogue. No manifest declaration of the DBF versions.

## The original defect

Fixed in `6e2e41cd4`, in the same commit as the sweep fixes, deliberately: every one of the 15
instances is the original defect's own code (the copy in login.go, its texts and comments, the
tests that pinned it, and the bash suite's check of the same flow), so no other instance could
be fixed first without fixing the original with it. `TestPasswordOffersTheClipboardOnWaylandAndPutsItOnStdin` `TestPasswordOffersTheClipboardOnWaylandAndPutsItOnStdin`
reproduces it (it asserted `--paste-once` and "for one paste" before; it now asserts a plain
copy, the read back and the clear) and passes. Left for the host: ACCEPTANCE-owner.md check 11
again (cleared; left alone after another copy; and whether GNOME still serves the password
after `wl-copy --clear`). Deploy: `changed-plays` queued in CLAUDE/Plan/meta-deploy.list
(expected: play-claude-yolo.yml; the image is unchanged).

## Conformance review

The `defence-before-fix:conformance-reviewer` reproduced the shell red at `d4f2a2da3`
(`qa-patterns.bash` fails on lines 1029 and 1031) and every single-rule run green at
`65a80a798`, and found no suppression or baseline. Its findings and their resolution:

1. The Go real-tree red and the `qa-changed.bash` green were not re-run by it: the logs are in
   the worktree's `untracked/scratch/qa-red.log` and `qa-green.log` (not tracked).
2. The independent search report was untracked when it looked: it is committed with this
   report.
3. The markdown half undefended: recorded above as pending the owner's decision.
4. The original fixed with the sweep, not after it: explained under The original defect.
5. Sweep scope and the Python and JavaScript zero: its own grep found nothing more.

## Second host note: "Homeserver unknown (not declared)"

A defect, not right UX: the URL is empty whenever the team is not declared present, declares
no port, or has no listen address with no `agent_bus_address`, and the next line said "The
team's Element profile has the homeserver set already", which then cannot be true. Fixed test
first in its own commit: `TestPasswordLoginOfATeamWithNoAddressSaysWhatIsMissing` (three
cases, red on the old text) now passes; the line names the gap ("unknown: <team> declares no
port in the host_vars", ...) and says to set it in the host_vars and run `ccy-teams apply
<team>`. The board's "port unknown (not declared)" row text is a different screen and was left.
