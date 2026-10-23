# Review round 3: lxc-attach-file-stream gate and the in_guest fix

Range reviewed: `git diff e43feeda4..f3ae16a22`, covering fd7f80cf4 (the gate), b1a4157fa (the
fix and its `--fake` legs), and 2095b97e0 and f3ae16a22 (the fixes from rounds 1 and 2). This
is a fresh adversarial review. Nothing tracked was edited and nothing was committed. The
scratch probes are in `untracked/scratch/lxcprobe3/`.

## What was run

- `cd src-go/qa && go test ./lxcattach/ ./cmd/qa-lxc-attach-streams/ ./changed/ ./all/`: all
  ok.
- `bash scripts/qa-lxc-attach-streams.bash`: `LXC-ATTACH-STREAMS-OK: 394 shell script(s) examined`.
- `acceptance.bash --fake --stage 2`: exit 0, with no `FAIL` line.
  - The three LXC-coordinator legs pass.
  - The `limited` reject leg is rejected on exit 9 (the rate limit), not on the fake's 97.
- A rule probe (`probe_test.go`) ran 40 new shapes through `lxcattach.Analyze`.
- An `in_guest` probe (`t.bash`, `fake.sh`) sources the real `in_guest` from the plan file and
  runs it against a fake lxc-attach. The fake refuses any regular file on fd 0, 1 or 2.

## Earlier required items

- **Round 1** (the remedy snippet, `/dev/` paths): still resolved. The QA.md snippet matches
  `in_guest` line for line.
- **Round 2** (only `/dev/null` is clear): resolved.
  - `devices` is now `{"/dev/null": true}`.
  - The `/dev/zero` and `/dev/tty` unit cases are findings.
  - QA.md:1109-1111 is reworded.
  - The fixture comment and the u23_lxc_attach caveat were taken. So was the note on the
    variable blind spot.

## Fresh findings

### Sound

- **`in_guest`'s exit status, checked by running it:**

  | Case                                                    | Result                                                                                  |
  | ------------------------------------------------------- | --------------------------------------------------------------------------------------- |
  | `$(in_guest ... 2>err.txt) \|\| rc=$?`, command exits 7 | rc=7. stdout is captured, and the err file holds the stderr. The fake never saw a file. |
  | stdin piped in                                          | It reaches the command.                                                                 |
  | `>out.txt 2>err.txt`, command exits 3                   | rc=3, and the fake was handed no file.                                                  |
  | Bare call under `set -e`, command exits 5               | The shell stops with status 5, as before.                                               |

  - With errexit live, the subshell's pipefail status is lxc-attach's own status, so nothing
    is lost there.
  - With errexit off, the `inner` and `piped` checks run.

- **The rule (the probe):**

  - These are all reported, each on the right stream:
    - the redirections `>>`, `&>>`, `>|` and `<>`
    - `while ...; done <list` (stdin)
    - `for`, `if`, `case`, `( )` and a function body with a redirection on it
    - a nested wrapper `h 2>e`
    - `time`, `!`, `exec`, `command`, `nohup`, `timeout` (with `-k`), `stdbuf`, `doas`,
      `nice`, `setsid` and `runuser`
    - the tool held in a scalar, in a global array or in a `local -a` array
    - a quoted tool name, a redirection before the command word, and `FOO=1 2>e lxc-attach`
    - `2>/dev/fd/1 >f` (stdout only) and `2>/dev/null >"$out"`
  - These are clear: `| tee f`, here-docs, here-strings and an in_guest-shaped wrapper.
  - An in_guest-shaped wrapper called with `<in` is reported (stdin passes through).
  - `exec 2>log` at the top of a script is not seen. That blind spot is documented.
  - A caller's `3>f` reaching lxc-attach as fd 3 is not reported, which is right: lxc-attach
    takes only fds 0 to 2.

- **The wiring:**

  - The `qa-gates.tsv` detail regex matches the real output.
  - `qa-routing.tsv` has its `also`, `first`, `post any:@sh` and `perfile-gate` rows.
  - `checks.go` has its entry and it is in order.
  - The `checks_test`, `run_test` and deviation goldens are updated, B52 is in SPEC-qa-changed,
    and `addedgates_test` has its row.
  - QA.md's table row and section, src-go/CLAUDE.md and the AgentNotes anchor all agree with
    the code.

- **Install-specific names:** none. The only hit is fixture paths in a qa-changed golden.

### Required

None.

### Non-blocking

1. **`in_guest` and SIGPIPE.** If the caller's reader closes early
   (`in_guest ... | head -c 1`), the stdout `cat` gets SIGPIPE. `in_guest` then prints
   "copying the stdout ... failed" and returns 1, where the old code returned lxc-attach's 141.
   No caller is affected today: line 182 pipes to `jq`, which reads all its input. It is only
   a misleading message.
2. **Two stale comments in `src-go/qa/lxcattach/rule.go`.** Lines 62 and 69 still say "a
   device" is safe. Since round 2 the only device that is safe is `/dev/null`, so they could
   say "/dev/null".
3. **Routing for the gate's imports.** The `qa-routing.tsv:121` row omits the gate's
   `src-go/qa/internal/proc/*` and `src-go/qa/internal/rootpath/*` imports. The hidden-bindings
   row omits them too, and the go check covers any `src-go` change, so this is consistency
   only.
4. **The new reject leg is weak on its own.** `fake_ask_lxc limited` would also be rejected
   under the old `in_guest` (the fake's exit 97). The two check legs are the real red-first
   proof, so this is fine. It could assert the exit is the rate limit.
5. **The AgentNotes heading (carried over from round 2).** It still says "stdout or stderr",
   but the rule covers stdin too.

## Verdict

MERGE

Required changes: none.
