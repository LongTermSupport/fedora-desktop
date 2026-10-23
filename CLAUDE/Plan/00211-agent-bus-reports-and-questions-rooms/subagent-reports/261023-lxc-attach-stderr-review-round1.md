# Review round 1: lxc-attach-file-stream gate (fd7f80cf4) and the in_guest fix (b1a4157fa)

Range reviewed: `git diff e43feeda4..b1a4157fa`. Adversarial review. Nothing was edited and
nothing was committed.

## What was run

- `cd src-go/qa && go test ./lxcattach/ ./cmd/qa-lxc-attach-streams/ ./changed/ ./all/ ./gates/`:
  all ok.
- `bash scripts/qa-lxc-attach-streams.bash`: `LXC-ATTACH-STREAMS-OK: 394 shell script(s) examined`.
- `acceptance.bash --fake --stage 2`: all legs OK, the three new LXC-coordinator legs included.
- Scratch experiments (removed afterwards). Each one compared the old `in_guest` with the new
  one, run against a fake lxc-attach that refuses a file on fd 0, 1 or 2:
  - **Originating shape** (`out="$(printf q | f ... 2>"$err")" || rc=$?`): the old one gives
    rc 97 and an empty capture, so the reuse and new-question legs really are red on the old
    code. The new one gives rc 9 (the command's own), `out=q`, and the err file is complete and
    readable as soon as the call returns.
  - **Exit codes**:
    - signal death: 143
    - `if` context: false, rc 1
    - stdout redirected to a file: works, rc 0
  - **`set -euo pipefail`, called bare** (as a statement, in `$(...)`, and piped into `jq`):
    the shell exits with the command's status (7), as the old one did. No `set -e` loss.
  - **A consumer that closes early** (`| head -n1`): the new one returns 1 with "copying the
    stdout ... failed". The old one returned the guest's status. Both are non-zero.

## Findings

### Sound

- **The new `in_guest`**:
  - It has no deadlock. Both cats write to different sinks, and nothing writes back into the
    pipeline.
  - fd 3 is closed in every child that does not need it.
  - stdin is unchanged. It still passes straight to lxc-attach, so the rule's summary for
    `in_guest` is exactly {stdin}, which is correct.
  - `inner` is unlocalised but lives in a subshell, so it does not leak.
  - When both lxc-attach and the stdout cat fail, the subshell exits (under an active `set -e`)
    with pipefail's rightmost non-zero status before the message is printed. The result is still
    non-zero.
- **Callers**: none uses `<file` with `in_guest`. Line 182 (`| jq`) and every `$(...)` or
  `|| rc=$?` caller propagates as before.
- **The rule**:
  - Pipe and redirection order is correct.
  - Dups, groups, function bodies with their own redirections, `$(...)`, process substitution,
    runners and cross-file summaries (a union, monotone, reaching a fixed point) are all
    handled.
  - The fixture and the unit tests cover the shapes the docs claim.
- **The gate wiring**:
  - `qa-gates.tsv`, the `qa-routing.tsv` also, first and post rows, and `perfile-gate` are in
    place, as are `checks.go`, B52 in SPEC-qa-changed with every deviation reason updated,
    `addedgates_test`, and the wording testdata.
  - The detail pattern matches the actual output.
- **Install-specific names**: none. The fixtures use the placeholders `m1`, `agent`,
  `/srv/member-home`, `c`.

### Required changes

1. **`CLAUDE/QA.md:1127-1136`**: the "How to fix a finding" snippet is unsafe to copy.
   - It records `piped=("${PIPESTATUS[@]}")` and then never checks or returns it. Pasted into a
     wrapper as shown, the function's status becomes the assignment's 0. Under the very caller
     shape that started this (`... || rc=$?`, where `set -e` is suspended), every lxc-attach
     failure is then swallowed. That is a fail-fast violation in the canonical remedy.
   - Its `[[ "${inner[1]}" -eq 0 ]] || exit 1` also fails with no message.
   - Fix: show the whole tail, as `in_guest` at `_acceptance-stage2.inc.bash:563-586` does:
     - the stdout-copy check with its message;
     - `if [[ "${piped[1]}" -ne 0 ]]; then printf ... >&2; return 1; fi`;
     - `return "${piped[0]}"`.
2. **`src-go/qa/lxcattach/rule.go:487-489`** (`fileTarget`): every `/dev/...` path is treated as
   safe. `/dev/shm/x` (and `/dev/mqueue`, and any regular file under `/dev`) is a regular file,
   and lxc-attach chowns it, so this is a false negative.
   - The QA.md text at `:1109` says only `/dev/null` (plus here-docs and here-strings) is clear,
     so the code and the docs disagree.
   - Fix: clear only the device nodes (`/dev/null`, `/dev/zero`, `/dev/tty`, `/dev/full`,
     `/dev/urandom`, `/dev/random`) besides the `/dev/std*` and `/dev/fd/N` aliases. Treat any
     other `/dev/` path as a file. Add a `2>/dev/shm/x` red case to `rule_test.go`, and the
     fixture too if you like. Alternatively, keep the code and make the docs say "a /dev path"
     and list `/dev/shm` under "What it does not see". The code fix is preferred: it is two
     lines, and the docs say "Do not loosen the check".

### Non-blocking (recommended)

- **The PLAN.md Task 2.13 line and the journal's "red before"**: the third LXC leg (`limited`,
  a `reject_leg`) cannot go red on the old `in_guest`, because exit 97 is rejected too. Only
  reuse and the new question are red-first. Reword the claim, or drop "another rate limit"
  from it.
- **`|&`**: `rule.go:305` makes stderr safe before the statement's own redirections. Bash
  performs the implicit `2>&1` of `|&` after them, so `lxc-attach 2>f |& cat` is reported but
  is clear: a false positive. It is rare, but either the order or a unit case would settle it.
- **Undocumented blind spots**: `bash -c '...lxc-attach...'`, `sh -c` and `ssh` strings, and
  `exec 3>file` followed later by `2>&3` (the same class as the documented bare `exec 2>file`).
  Consider adding them to "What it does not see".
- **Function and variable names are unioned across all 394 scripts.** A future generic
  `run() { lxc-attach ...; }` in one plan would flag every `run ... 2>f` in the others. That is
  only a false positive, and acceptable, but worth a sentence in the package doc.
- **`in_guest`'s stdout-copy message** reads "copying ... failed" when the consumer merely
  stopped reading (SIGPIPE on the cat). No caller does this today.

## Verdict

CHANGES

1. `CLAUDE/QA.md:1127-1136`: complete the remedy snippet. Check `piped[1]` with a message,
   `return "${piped[0]}"`, and give the stdout-copy failure a message. As written, it returns 0
   and swallows lxc-attach failures in `|| rc=$?` callers.
2. `src-go/qa/lxcattach/rule.go:487-489`: stop clearing every `/dev/` path; `/dev/shm` files are
   regular files lxc-attach chowns. Clear the device nodes only, add a red unit case, or
   document the gap (code fix preferred; it also matches `QA.md:1109`).
