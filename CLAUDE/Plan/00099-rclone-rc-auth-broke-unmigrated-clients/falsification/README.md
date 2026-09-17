# Plan 00099 — falsification harnesses

Every claim this plan makes about a fix rests on one of these. They live here, tracked,
rather than under `untracked/scratch/`, because `untracked/` is gitignored wholesale: the
evidence was in no diff, existed on no other clone, and would not have travelled into
`Completed/` with the plan it belongs to. `CLAUDE/Plan/CLAUDE.md` ("Plan-Local Scripts &
Artifacts — IN STONE") puts plan-specific test scripts in the plan folder; this is that.

That location was not cosmetic. The round-4 harness verified check [6] with five **greps**
of the gate's text and never executed it, and nobody saw that because the file was never in
a diff. Round 5's blocking finding was a call to an undefined command in the very block
that harness vouched for.

## What each one establishes

| Harness                           | Kills                                                                                                                                 |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `falsify-b1.bash`                 | the library normalises a path inside the mount to the mount root, so `--copy` resolves an address                                     |
| `falsify-triage-leg.bash`         | CONTROL: with a discoverable address `triage.bash`'s gather leg passes and the run exits 0 — without it, "exit 1" would prove nothing |
| `falsify-coverage-direction.bash` | a check that RAN without being declared is named, and a declared one is not falsely flagged                                           |
| `falsify-drift-summary.bash`      | `qa-deployed-drift.bash`'s failing summary names all three categories                                                                 |
| `falsify-round3-fixes.bash`       | the duplicate-`--rc-addr` refusal, and the round-3 gate fixes                                                                         |
| `falsify-round4-fixes.bash`       | a duplicated `CHECK_CATALOGUE` entry is named and the run is not ACCEPTED                                                             |
| `falsify-round5-note.bash`        | without `note()` check [6]'s differing-address branch exits 127 with no verdict                                                       |
| `falsify-round6-rcaddr.bash`      | the pre-fix `--rc-addr` line exits 1 with **no output** when the flag is absent, in all four copies                                   |

`retired/` holds harnesses that still pass but exercise code paths that no longer exist.
See its `WHY-RETIRED.md`: a green result about an abandoned path carries no information,
which is this plan's own subject one level out.

## Rules these follow

- **Every one has a CONTROL.** A mutant that dies proves nothing unless the unmutated
  version lives. An assertion that fails on everything is not an assertion.
- **`if cmd; then rc=0; else rc=$?; fi`**, never `if ! cmd; then rc=$?`. The `!` inverts the
  status before `$?` is read, so the second form records 0 for every failure — it once made
  a harness vouch for the exact bug it existed to disprove.
- **Blocks are EXTRACTED from the shipped file with asserted anchors**, never retyped. A
  moved anchor fails loudly instead of silently testing an empty string.
- **Nothing is hand-copied and left lying about.** `falsify-triage-leg.bash` derives its
  guard-free copy from the current `triage.bash` on every run; the hand-made one it used
  before was four review rounds stale and its green result described a script that no
  longer existed.
- **Paths come from `_paths.inc.bash`**, resolved by walking up from the harness's own
  location to `ansible.cfg`. `git rev-parse` answers about the CWD, not the script.
- **Mutants are written where the script under test can still resolve the repo root** —
  `scripts/` for the drift gate, the plan folder for `triage.bash` — and removed on exit,
  including on Ctrl-C. A copy in `/tmp` dies in its own bootstrap, and the harness would
  then be reporting on that rather than on the thing it names.

## Running them

Each is standalone and takes no arguments:

```bash
bash CLAUDE/Plan/00099-rclone-rc-auth-broke-unmigrated-clients/falsification/falsify-round6-rcaddr.bash
```

They stub only system probes (`findmnt`, `pgrep`, `/proc` argv via a real background
process) and run the real library and the real gate blocks. Several start a `sleep`-backed
process to carry a realistic argv, so a full sweep takes a couple of minutes.
