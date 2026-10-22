VERDICT: MERGE
SIMPLIFY

The branch is correct for real tokens and real settings. The production clock is clean and the secret handling is sound. None of the findings is blocking. The cost is in the rules for when Go hands a run to bash: some are broad enough that Go rarely answers `--list-tokens`, and the rest will be deleted once the launch path itself is in Go.

**What I ran:** `go test` without tags over `ccylaunch/...`, `execx/...` and `cmd/claude-yolo` passed. `go test -tags difftest` over `ccylaunch/...` and `cmd/claude-yolo` passed. `ccylaunch` coverage is 93.6%. `qa-changed --base integration/261021-membership` failed on a single test, `tests.speech_to_text.test_claude_process` with `value='1'` (a 1-second limit). It passes when run alone and this branch does not touch it, so it is a timing flake under load. All the ccy suites passed, including test-ccy-info-flags 90/90 and test-ccy-token-handoff.

**Findings (none blocking)**

1. **The production clock is clean (attack 1 holds).** `go list -deps ./cmd/claude-yolo` contains no `pinclock` or `difftest` package; only `-tags difftest` pulls `pinclock` in. The only build-tagged files are `cmd/claude-yolo/clock.go` (`!difftest`, the system clock) and `clock_difftest.go`. `pinclock.FromEnv` returns an error when `CCY_DIFFTEST_SANDBOX` is unset (pinclock.go:80), and `main.go:33` exits 1 on it. `tasks/build-go-command.yml:103` sets `GOFLAGS` explicitly, so a stray `-tags` in the environment cannot reach the play's build. The production code reads no environment variable for the time.

2. **Secrets are handled correctly (attack 2 holds).**
   - curl gets the token through `--config -`, as a `header = "Authorization: Bearer …"` line on stdin, byte for byte what the bash's `printf` pipes (token-management.bash:441).
   - The token is not in argv or the environment; `secret_test.go` reads `/proc/$$/cmdline` and `${CCY_TEST_TOKEN-unset}` to prove it, with and without `NewSession`.
   - `checkSecrets` also covers the fed secret (secret.go:104-108). `Exec`, `Foreground` and `Fake.Foreground` refuse a Feed. `fed()` runs after the argv check and refuses a Feed together with a Stdin.
   - `%v`, `%+v` and `%#v` of a Cmd with a Feed show only a pointer, and the test checks it. No error message carries the value.
   - Weakness, the same in bash: a token containing `"`, `\` or a newline can rewrite curl's config. It is the user's own file, so this is not a regression.

3. **A short token fails instead of handing over** (tokenusage.go:303-307, 452-453). `readToken` only checks the token against the settings and TMPDIR. curl's fixed arguments (`--connect-timeout`, `user`, `content`, `POST`, the temp names) are not checked. A junk token file such as `user` or `on` makes `Run` return `ErrSecretInArgv`, and Go exits 1 where bash prints the table. Real `sk-ant-…` tokens never hit this. Fix: check the whole argv in `readToken`, or treat `ErrSecretInArgv` as a hand-over.

4. **`answer()` claims "decided before anything is written" (front.go), which is false for `--token-usage`.** Two hand-overs come after the fetch: an unreadable rate-limit header (tokenusage.go:464) and a reset time with a leading nought or past year 9999 (667-672). By then Go has printed the progress line and written cache entries. Bash then prints the progress line a second time, and asks the API again for any entry Go did not write. This is rare and harmless, but the doc comment and F11 should say so.

5. **`Front.Dir` is a dead field** (front.go:135). Nothing in production or tests uses it. Delete it.

6. **The collation rule is too broad** (bashread.go:93-99, listtokens.go:35). With any locale other than C, which is the normal Fedora desktop (`LANG=en_*.UTF-8`), Go hands over unless every name matches `^[a-z]+\.date\.token$`. Names allow digits, `-`, `_` and capitals, so a pool holding a name like `work-2` always goes to bash and the `--list-tokens` port then does nothing. The rule matters only for display order. For `--token-usage` it cannot change which file a name launches with: two files of one name differ only in their date digits, which every locale sorts the same, and undated files are handed over anyway.

7. **Shell variables the bash takes from the environment are not all covered.** `shellEnv` (front.go:107) does not include `GLOBSORT`, which bash 5.3 added and which reorders globs. Unverified: the container has bash 5.2.15, so I could not test it, and the host may run 5.3. I tested `GLOBIGNORE` from the environment on 5.2 and it had no effect, so that one is not a gap.

8. **The other hand-over rules are correct and narrow.**
   - The DST rule only fires when a clock change moves the bash's truncated day count across a colour boundary: 15 days becoming 14, or 1 becoming 0.
   - The zone rule matches how Go and glibc each read `TZ`, `TZDIR` and `ZONEINFO`. The skipped-midnight and invalid-date cases hand over.
   - On a typical host (`TZ` unset, ASCII `HOME`, no `TMPDIR`), Go does answer `--token-usage`.

9. **The parity tests are real.** The goldens and scenarios are unchanged by the branch, so they are the bash's own recordings. `TestFrontAnswersItself` breaks the bash's `list_tokens` and `usage_report` lines and the 15 scenarios still match, so Go answered them. `token-usage-not-first` stays with bash. Exit codes 0, 1 and 64 match `usage_report`. The cache entry format (file name, status, record; 0600 in a 0700 directory), the freshness test (`age >= 0 && age < TTL`) and the stale test (line 779) match the bash.

10. **Docs, version and the python-heredoc edit are clean.**
    - The `front.go` hunk in 320e44b4 has only the F11 doc, `flagTokenUsage`, `flagTools` and the `ctx` threading. It is gofmt-clean with no CRs.
    - `CCY_VERSION` is 3.99.14.6, the changelog has entries for 3.99.14.5 and 3.99.14.6, and docs/ccy.md, src-go/CLAUDE.md (F1-F11) and PLAN.md agree with the code.
    - No names specific to any install appear in the diff.
    - I did not re-check the commit message's "82/82 against `LAUNCHER=` a `-tags difftest` build".

**Why SIMPLIFY rather than KEEP or REVERT:**
- **What to keep:** the parts that must survive the full port. These are the JSON document, which `wsi-claude-token` parses; the cache format, which a bash launch reads too; the fetch through Feed; and the table. They are also the shared contracts that src-go/CLAUDE.md says an oracle should pin.
- **What to cut:** about a third of `tokenusage.go` and most of `bashread.go` exists only to copy the bash byte for byte, and all of it goes when bash retires:
  - mktemp-style temp names in `TMPDIR`
  - the awk `%.4f` round trip
  - the octal and year-9999 guards
  - the collation, zone and DST hand-overs
  - colour whatever stdout is
- **How to cut it:** replace the cosmetic parts with declared departures — byte-order sort, calendar-day colouring, Go's temp files — and keep hand-overs only where the meaning could differ (an undated file, an unreadable token or cache entry).
- **Why not revert:** the goldens run in a C locale with a pinned clock, so they do not exercise these rules. Dropping them costs no parity and makes Go answer for typical users.

The code is at `/workspace/.claude/worktrees/agent-ae205a4af87ad9866-9d6b4cf9/src-go/internal/ccylaunch/` (`tokenusage.go`, `bashread.go`, `listtokens.go`, `front.go`) and `src-go/internal/execx/secret.go`.