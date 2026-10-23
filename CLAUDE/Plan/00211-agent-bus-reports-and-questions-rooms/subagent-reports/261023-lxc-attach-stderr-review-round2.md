# Review round 2: lxc-attach-file-stream gate and the in_guest fix

Range reviewed: `git diff e43feeda4..2095b97e0`, covering fd7f80cf4 (the gate), b1a4157fa (the
fix and its `--fake` legs) and 2095b97e0 (the round-1 fixes). This is an adversarial review.
Nothing in the tree was edited and nothing was committed. The scratch probes live under
`untracked/scratch/lxcprobe/`.

## What was run

- `cd src-go/qa && go test ./lxcattach/ ./cmd/qa-lxc-attach-streams/ ./changed/ ./all/`: all
  ok.
- `bash scripts/qa-lxc-attach-streams.bash`: `LXC-ATTACH-STREAMS-OK: 394 shell script(s) examined`.
- `acceptance.bash --fake --stage 2`: all legs OK, including the three LXC-coordinator legs.
  The reuse leg reads `Q3` back from the err file. The new-question leg goes through
  `as agent in LXC container m1`.
- A scratch Go probe, `untracked/scratch/lxcprobe/probe_test.go` with
  `cases/a.bash`, calls `lxcattach.Analyze` on 30 new shapes. Each result is listed below.

## Round-1 required items

1. **The QA.md remedy snippet: resolved.** It now checks `inner[1]` and `piped[1]`, each with
   a message, and does `return "${piped[0]}"`. It matches `in_guest`, at
   `_acceptance-stage2.inc.bash:563-586`, line for line.
2. **`/dev/` paths: resolved as asked.** Only the listed device nodes are clear. `/dev/shm`
   and `/dev/mqueue` are red unit cases. However, the list round 1 proposed is itself wrong:
   see Required 1 below.

The round-1 non-blocking items were taken as well:

- `|&` now joins stderr after the command's own redirections. The probe confirms it:
  `lxc-attach >f |& cat` reports stdout and stderr, and `{ ...; } 2>e |& cat` is clear.
- The blind spots and the pooled names are documented.
- PLAN.md's "rejected either way" wording is in.

## Fresh findings

### Sound

- **`in_guest`**:
  - stdin is still inherited by the first element of each pipeline.
  - fd 3 is closed in every child.
  - Both copy failures carry a message and return 1, and otherwise the command's status is
    returned.
  - Without `inherit_errexit`, the `$(...) || rc=$?` caller gets the real status. A bare
    statement under `set -e` still stops the shell, as before.
  - Caller line 182 (`</dev/null | jq`) and line 514 (`</dev/null`) are unaffected.
- **The `--fake` legs**:
  - The fake lxc-attach refuses any regular file on fd 0, 1 or 2 with exit 97. The old
    `in_guest` handed it the err file, so the reuse and new-question legs are genuinely red
    first.
  - `LXC_ATTACH` and `PODMAN` are `local` to `fake_ask`, and the fixture paths are per-kind
    (`ask-run-lxc-*`), so the LXC legs do not collide with the ccy ones.
  - The probe values are placeholders (`m1`, `agent`, `/srv/member-home`).
- **The rule (probe results)**:
  - Correctly reported:
    - `2>&1 >f`
    - `>f |& cat`
    - a wrapper whose summary is {1} after `2>&1 >/dev/null`, with `>e` reported and `2>e`
      clear
    - `2>/dev/fd/0 <in`, as stdin only
    - wrappers that pipe stdout or capture it, judged per descriptor
    - `sudo --preserve-env=X`, `env -i A=b sudo` and `FOO=1` prefixes
    - mutually recursive wrappers, which reach the fixed point correctly
  - Correctly clear:
    - `coproc`
    - `/dev/fd/5`
- **The wiring**:
  - The `qa-gates.tsv` row's detail regex matches the real output line.
  - `qa-routing.tsv` has its rows: the fixture as `covered`, the gate's files, the `first`
    row for `.semgrep`, `post any:@sh` and `perfile-gate`.
  - The check in `checks.go`, B52 and the deviation goldens are in, and the `./changed/` tests
    pass.
  - `addedgates_test` and the wording testdata are in. The `393` in the wording sample is only
    an example line.
  - QA.md has its table row (line 119) and its section. The AgentNotes anchor resolves.
- **Install-specific names**: none in the diff.

### Required

1. **`src-go/qa/lxcattach/rule.go:470-475`** (`devices`), **`CLAUDE/QA.md:1109-1111`**, and
   **`rule_test.go:43`** ("another device") and **`:57`** ("the terminal"): the rule treats
   `/dev/zero`, `/dev/full`, `/dev/tty`, `/dev/random` and `/dev/urandom` as clear. The
   project's own model contradicts that:
   - `QA.md:1114` says lxc-attach makes each stream that is not `/dev/null` root's own.
   - The fixture header says it does so "with a mode only root can read".
   - AgentNotes shows the chown succeeding on an inode the desktop user owns, so it runs with
     host privilege over host inodes.
   - On that model, `lxc-attach ... </dev/urandom` or `>/dev/tty` would chown and restrict a
     **host device node**, which is worse than losing one capture.
   - The code comment's justification ("a chown of the others costs the caller no file") is
     the gap: it costs every other user the device.
   - Only `/dev/null` is known to be skipped (lxc compares `st_rdev` with `/dev/null`).
   - No tracked script uses these devices with lxc-attach, so tightening costs nothing.
   - Fix:
     - Clear only `/dev/null`, besides the `/dev/std*` and `/dev/fd/N` aliases, which already
       resolve to the descriptor they name.
     - Flip the two unit cases to red.
     - Reword QA.md:1109-1111 to "`/dev/null` (which lxc-attach skips), here-documents and
       here-strings are clear; any other path, a device under `/dev` included, is a finding".
   - This is the fail-fast direction, and "Do not loosen the check" applies. This was
     round 1's suggestion, so the error is the reviewer's, not the author's.

### Non-blocking

- **False negatives worth one line in "What it does not see"** (both checked with the probe):
  - The runner is held in a variable: `SUDO=(sudo -n); "${SUDO[@]}" lxc-attach ... 2>e` is
    not reported. `isRunner` flattens the expansion to NUL. A variable whose words name a
    runner could be resolved the way `vars` are.
  - The tool's path comes from a command substitution: `L="$(command -v lxc-attach)"; "$L" ... 2>e` is not reported.
  - There is no instance of either in the tree today.
- **The fixture's `ask_fixed`** (`.semgrep/lxc-attach-streams.bash:35-40`) is commented
  "Cleared" for the `2> >(cat >"${err}")` shape, which QA.md:1150 warns against because a read
  straight after it can be short. The rule is right to clear it. The comment could say
  "clear to the rule, but not the remedy" so nobody copies it.
- **`u23_lxc_attach`** (Plan 00161), which QA.md cites as a model, exits `PIPESTATUS[1]` and
  never checks its stdout `cat`. That code predates this diff, but the citation makes it
  canonical. Either cite `in_guest` alone or note the difference.
- **QA.md:1111** is one overlong unwrapped line, a formatting nit.
- **The AgentNotes heading** says "stdout or stderr", but the rule and the fixture also cover
  stdin. The heading could say "any of its standard streams".

## Verdict

CHANGES

1. `src-go/qa/lxcattach/rule.go:470-475`, `CLAUDE/QA.md:1109-1111` and
   `src-go/qa/lxcattach/rule_test.go:43,57`. Clear only `/dev/null` (plus the `/dev/std*` and
   `/dev/fd/N` aliases). By the project's own model, lxc-attach chowns and restricts every
   other stream, so clearing `/dev/zero`, `/dev/full`, `/dev/tty`, `/dev/random` and
   `/dev/urandom` hides a hazard to host device nodes. Flip the two unit cases to red and
   reword the docs.
