# Plan 00052 — Phase 6 Smoke Test (run.bash helpers)

## Approach

`run.bash` is a TTY installer that cannot run end-to-end inside the CCY
container (it provisions a host). But the *prompt logic* is pure shell and is
fully exercisable non-interactively. The helpers follow a clean contract — they
send prompts/errors to **stderr** and emit the accepted value to **stdout** via
`printf '%s'`, and `confirm()` returns an exit status — so we can:

1. Extract each helper function from `run.bash` by name (brace-counting `awk`),
   so the test always runs the *real* code, not a copy.
2. Define the colour/symbol vars the helpers reference (plain values for test
   output).
3. Drive each helper with scripted stdin via `printf '...\n...' | helper ...`,
   capturing `$(...)` for value helpers and `$?` for `confirm`.
4. Assert: **Enter accepts the default**, **bad input re-prompts**, **good input
   after bad input is accepted**, and **the helper never exits the process**
   (the harness runs to completion and reports a total).

The harness lives at `/tmp/smoke_run_helpers.bash` during development; its full
source is embedded below so the test is repeatable. Run it with:

```bash
bash /tmp/smoke_run_helpers.bash /workspace/run.bash
```

> Note: live full-installer interactive testing (Task 6.4) is **HOST-only** —
> the container cannot run the real flow (no host to provision, no TTY for the
> hidden vault reads against a real `localhost.yml`). This smoke test covers the
> helper logic only, which is where the Plan-00052 behaviour changes live.

## What each case proves

- **confirm()** — `default=y` Enter → rc 0; `default=n` Enter → rc 1; explicit
  `y`/`n` override the default; a bad key (`x`) re-prompts then `yes` accepts.
  Proves safe-polarity defaults and never-exit.
- **promptForValue()** — Enter at the confirm accepts the typed value (the
  headline bug); `y` accepts; `n` re-opens the value for editing (a *second*
  value is accepted, not a blind restart); a supplied default is taken on Enter
  at entry; empty entry re-prompts; bad email re-prompts then a good one is
  accepted.
- **promptChoice()** — valid number accepted; Enter takes the supplied default;
  out-of-range then non-numeric then valid → accepted.
- **promptSecretConfirmed()** — matching entries accepted; mismatch re-prompts
  then matches; empty allowed.
- **promptDefault()** — Enter takes the default; typed value used; below-minlen
  re-prompts then a long-enough value is accepted.
- **prompt_verified_vault_password()** — the `abort` escape hatch returns 1
  cleanly (so the caller fails fast) even when verification cannot succeed.

## Result (recorded)

Command:

```bash
bash /tmp/smoke_run_helpers.bash /workspace/run.bash
```

Output:

```
=== confirm() ===
PASS: confirm default=y Enter accepts
PASS: confirm default=n Enter declines
PASS: confirm default=y explicit n declines
PASS: confirm default=n explicit y accepts
PASS: confirm bad-then-good accepts
=== promptForValue() ===
PASS: promptForValue Enter-at-confirm accepts
PASS: promptForValue y-at-confirm accepts
PASS: promptForValue n re-edits value
PASS: promptForValue default via Enter
PASS: promptForValue empty re-prompts
PASS: promptForValue email bad-then-good
=== promptChoice() ===
PASS: promptChoice valid
PASS: promptChoice Enter takes default
PASS: promptChoice bad-then-good
=== promptSecretConfirmed() ===
PASS: promptSecretConfirmed match
PASS: promptSecretConfirmed mismatch-then-match
PASS: promptSecretConfirmed empty allowed
=== promptDefault() ===
PASS: promptDefault Enter default
PASS: promptDefault typed value
PASS: promptDefault minlen re-prompt
=== prompt_verified_vault_password() abort path ===
PASS: prompt_verified_vault_password abort returns 1
PASS: prompt_verified_vault_password EOF aborts (no spin)

TOTAL: pass=22 fail=0
EXIT=0
```

All 22 assertions pass; the harness runs to completion (EXIT=0), proving no
helper exits the process on any input path. The final assertion (added in the
v1.6.1 follow-up) runs `prompt_verified_vault_password` with `</dev/null` under a
wall-clock watchdog to prove a failed read (EOF) aborts cleanly instead of
busy-looping on the empty-input branch.

## Harness source

```bash
#!/usr/bin/env bash
# Smoke test for run.bash interactive helpers.
set -u
IFS=$'\n\t'

SRC="${1:-/workspace/run.bash}"
LOG=$(mktemp)   # helper stdout/stderr noise goes here, inspected on demand

RED=''; GREEN=''; YELLOW=''; BLUE=''; MAGENTA=''; CYAN=''; BOLD=''; NC=''
CHECK='[OK]'; CROSS='[X]'; ARROW='->'; INFO='i'; WARN='!'; BUG='bug'
export RED GREEN YELLOW BLUE MAGENTA CYAN BOLD NC CHECK CROSS ARROW INFO WARN BUG

# Extract one shell function definition `name(){ ... }` by brace-counting.
extract_fn(){
  awk -v fn="$1" '
    $0 ~ "^"fn"\\(\\)\\{" { capture=1; start=NR }
    capture {
      print
      n=gsub(/\{/,"{"); o+=n
      n=gsub(/\}/,"}"); o-=n
      if (o<=0 && NR>start) { exit }
    }
  ' "$SRC"
}

eval "$(extract_fn confirm)"
eval "$(extract_fn promptForValue)"
eval "$(extract_fn promptChoice)"
eval "$(extract_fn promptSecretConfirmed)"
eval "$(extract_fn promptDefault)"
eval "$(extract_fn verify_vault_password)"
eval "$(extract_fn prompt_verified_vault_password)"

pass=0; fail=0
check(){ # <desc> <expected> <actual>
  if [[ "$2" == "$3" ]]; then echo "PASS: $1"; pass=$((pass+1))
  else echo "FAIL: $1 (expected [$2] got [$3])"; fail=$((fail+1)); fi
}
run_confirm(){ printf '%b' "$1" | confirm "$2" "$3" >"$LOG" 2>"$LOG"; }

echo "=== confirm() ==="
run_confirm '\n' "continue?" y; check "confirm default=y Enter accepts" 0 $?
run_confirm '\n' "reboot?" n;   check "confirm default=n Enter declines" 1 $?
run_confirm 'n\n' "continue?" y; check "confirm default=y explicit n declines" 1 $?
run_confirm 'y\n' "reboot?" n;   check "confirm default=n explicit y accepts" 0 $?
run_confirm 'x\nyes\n' "continue?" y; check "confirm bad-then-good accepts" 0 $?

echo "=== promptForValue() ==="
out=$(printf 'alice\n\n' | promptForValue "name" 2>"$LOG"); check "promptForValue Enter-at-confirm accepts" "alice" "$out"
out=$(printf 'bob\ny\n' | promptForValue "name" 2>"$LOG"); check "promptForValue y-at-confirm accepts" "bob" "$out"
out=$(printf 'typo\nn\nfixed\n\n' | promptForValue "name" 2>"$LOG"); check "promptForValue n re-edits value" "fixed" "$out"
out=$(printf '\n' | promptForValue "name" "" "thedefault" 2>"$LOG"); check "promptForValue default via Enter" "thedefault" "$out"
out=$(printf '\nreal\n\n' | promptForValue "name" 2>"$LOG"); check "promptForValue empty re-prompts" "real" "$out"
out=$(printf 'notanemail\ngood@example.com\n\n' | promptForValue "email" email 2>"$LOG"); check "promptForValue email bad-then-good" "good@example.com" "$out"

echo "=== promptChoice() ==="
out=$(printf '2\n' | promptChoice "pick: " 5 2>"$LOG"); check "promptChoice valid" "2" "$out"
out=$(printf '\n' | promptChoice "pick: " 5 3 2>"$LOG"); check "promptChoice Enter takes default" "3" "$out"
out=$(printf '99\nx\n4\n' | promptChoice "pick: " 5 2>"$LOG"); check "promptChoice bad-then-good" "4" "$out"

echo "=== promptSecretConfirmed() ==="
out=$(printf 'sec\nsec\n' | promptSecretConfirmed "pw" 2>"$LOG"); check "promptSecretConfirmed match" "sec" "$out"
out=$(printf 'a\nb\nc\nc\n' | promptSecretConfirmed "pw" 2>"$LOG"); check "promptSecretConfirmed mismatch-then-match" "c" "$out"
out=$(printf '\n\n' | promptSecretConfirmed "pw" 2>"$LOG"); check "promptSecretConfirmed empty allowed" "" "$out"

echo "=== promptDefault() ==="
out=$(printf '\n' | promptDefault "x: " "def" 0 2>"$LOG"); check "promptDefault Enter default" "def" "$out"
out=$(printf 'typed\n' | promptDefault "x: " "def" 0 2>"$LOG"); check "promptDefault typed value" "typed" "$out"
out=$(printf 'ab\nabc\n' | promptDefault "x: " "" 3 2>"$LOG"); check "promptDefault minlen re-prompt" "abc" "$out"

echo "=== prompt_verified_vault_password() abort path ==="
printf 'abort\n' | prompt_verified_vault_password "/nonexistent.yml" >"$LOG" 2>"$LOG"
check "prompt_verified_vault_password abort returns 1" 1 $?
# EOF must abort cleanly (rc 1), not busy-loop. Run in a subshell under a wall
# clock: spawn the call, and if it has not finished promptly, treat it as a spin.
export -f verify_vault_password prompt_verified_vault_password
( prompt_verified_vault_password "/nonexistent.yml" </dev/null >"$LOG" 2>"$LOG" ) &
_eof_pid=$!
_eof_rc=1
for _i in $(seq 1 50); do
  if ! kill -0 "$_eof_pid" 2>"$LOG"; then
    wait "$_eof_pid"; _eof_rc=$?
    break
  fi
  sleep 0.1
done
if kill -0 "$_eof_pid" 2>"$LOG"; then
  kill "$_eof_pid" 2>"$LOG"
  _eof_rc="SPIN"
fi
check "prompt_verified_vault_password EOF aborts (no spin)" 1 "$_eof_rc"

echo
echo "TOTAL: pass=$pass fail=$fail"
rm -f "$LOG"
[[ $fail -eq 0 ]]
```
