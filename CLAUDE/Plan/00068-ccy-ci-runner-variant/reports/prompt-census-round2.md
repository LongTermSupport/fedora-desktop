# Plan 00068 — Prompt census, Round 2 (Task 7.2, per review finding C3)

Round 1 counted interactive prompts with a pattern matching `read -rp`. C3 said that pattern
misses `read -r -p` — flags written separately — and predicted the true count was nearer 46.

**C3 is confirmed, and the miscount is the least interesting part of it.**

## The numbers

Measured on `files/var/local/claude-yolo/` at branch `plan-00066-ccy-ci-runner`.

| File                          | Round-1 pattern `read[[:space:]]+-rp` | Corrected pattern (`-p` in any flag order) | Missed |
| ----------------------------- | ------------------------------------: | -----------------------------------------: | -----: |
| `claude-yolo`                 |                                    20 |                                         20 |      0 |
| `lib/token-management.bash`   |                                 **0** |                                      **9** |  **9** |
| `lib/docker-health.bash`      |                                     5 |                                          5 |      0 |
| `lib/dockerfile-custom.bash`  |                                     5 |                                          5 |      0 |
| `lib/network-management.bash` |                                     4 |                                          4 |      0 |
| `lib/ssh-handling.bash`       |                                     3 |                                          3 |      0 |
| `lib/common.bash`             |                                     0 |                                          0 |      0 |
| `entrypoint.sh`               |                                     0 |                                          0 |      0 |
| **Total**                     |                                **37** |                                     **46** |  **9** |

46 matches C3's predicted ~46.

Reproduce:

```bash
cd files/var/local/claude-yolo
# Round-1 pattern
grep -rcE 'read[[:space:]]+-rp' claude-yolo entrypoint.sh lib/*.bash
# corrected
grep -rcE 'read([[:space:]]+-[a-zA-Z]+)*[[:space:]]+-[a-zA-Z]*p' claude-yolo entrypoint.sh lib/*.bash
```

(A third pattern — every `read -<flag>` call, prompt-bearing or not — totals 74. Those extra 28
are pipe/heredoc reads, a different class, and are not blockers. Counted only to confirm the 46
is not itself an undercount.)

## Why the miss matters more than the count

**Every one of the 9 missed prompts is in `token-management.bash`, and that file was 100%
invisible to the Round-1 census.** Not undercounted — unseen. It uses `read -r -p` exclusively,
so a pattern requiring `-rp` found nothing there and reported a clean file.

That is the failure mode this repo keeps naming: *a control that silently becomes a no-op*. The
census did not report "token-management: not scanned"; it reported a count, and the count was
zero, which reads as "no prompts here". A file with nine blocking prompts on the default path
looked identical to a file with none.

| Line | Function                      | Prompt                        |
| ---- | ----------------------------- | ----------------------------- |
| 171  | `create_token()`              | `Enter a name for this token` |
| 212  | `create_token()`              | `Overwrite? (y/N)`            |
| 324  | `create_token()`              | `Token:`                      |
| 331  | `create_token()`              | `Try again? (Y/n)`            |
| 346  | `create_token()`              | `Try again? (Y/n)`            |
| 364  | `create_token()`              | `Try again? (Y/n)`            |
| 407  | `create_token()`              | `Try again? (Y/n)`            |
| 611  | `select_token()`              | `Select token [...]`          |
| 826  | `export_tokens_interactive()` | `Select tokens to export`     |

## The named sub-problem: `create_token` / `select_token` are the earliest blocker

C3 asked for these to be treated as their own sub-problem. They are, for two reasons.

**They sit on the default path.** `select_token()` is what runs when `ccy` is invoked without an
explicit token; `create_token()` is what runs when there is no token to select. An unattended
runner with no pre-provisioned token reaches `create_token` before it reaches anything this plan
was originally scoped to fix.

**One of them spins forever rather than failing.** `token-management.bash:322` opens a
`while true`, and inside it:

```bash
while true; do
    echo "Please manually paste the token (starts with sk-ant-oat01-):"
    read -r -p "Token: " manual_token
    if [ -z "$manual_token" ]; then
        print_error "Token cannot be empty"
        read -r -p "Try again? (Y/n): " retry
        if [ "$retry" = "n" ] || [ "$retry" = "N" ]; then ... return 1; fi
        continue
    fi
    ...
```

On EOF (no TTY — every CI runner): `read` returns non-zero and leaves `manual_token` empty →
the empty branch fires → the `Try again?` read also EOFs → `retry` is empty → empty is neither
`n` nor `N` → `continue` → **loop forever, emitting output the whole time**. The `while true`
ancestor also suspends `errexit`, so `set -e` cannot rescue it.

Note the shape of the bug: the loop's *only* exit paths are a valid token or an explicit `n`.
EOF is neither, and EOF is the one thing guaranteed on a runner. A hang is worse than a crash —
it burns a JIT runner slot and the job dies on timeout with no diagnosable cause.

## Consequences for the plan

1. **The headline "35 prompts" is superseded by 46.** Any task text quoting 35 needs updating.
2. **Task 7.3's spin-vs-abort classification must cover `token-management.bash`,** which the
   Round-1 classification never examined. At least the 322 loop spins; 611 and 826 need
   classifying.
3. **`create_token` is an irreducibly human OAuth flow** (already noted as C5). It cannot be made
   unattended — which makes "accept `CLAUDE_CODE_OAUTH_TOKEN` by value, bypassing the token-file
   subsystem" (Task 7.4) not a convenience but the *only* way the default path is survivable
   unattended. It should be treated as a prerequisite, not one capability among six.
4. **The census pattern itself is now a known-fragile control.** Whatever re-runs it should be
   asserted against a file known to contain `read -r -p`, so a pattern that silently matches
   nothing cannot report a clean sweep again.

## What this report does not claim

Only that these prompts *exist* and that the `:322` loop spins on EOF by inspection of its
control flow. Nothing here was executed — `ccy` needs a real engine, and the nested-podman attempt
already failed for an unrelated userns reason. Confirming the spin empirically belongs to the
HOST triage run.
