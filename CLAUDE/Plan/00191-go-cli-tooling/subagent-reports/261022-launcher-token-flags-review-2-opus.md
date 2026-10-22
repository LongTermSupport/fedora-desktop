VERDICT: MERGE

I changed nothing in the branch. The one file I wrote is the qa-changed log at `/workspace/untracked/scratch/qa-changed-tokenflags-review.log`.

**What I ran, all green:**
- `go test` with and without `-tags difftest`, on `ccylaunch/...`, `tokens`, `wsitoken` and `execx`.
- The retry test with `-race -count=30`, and the whole of `ccylaunch/...` under `-tags difftest` with `-race -count=3`.
- `./scripts/qa-changed.bash --base integration/261021-membership`, which passed: the Go gate, docs, bash-limits and 40 bash suites.
- `ccylaunch` coverage is 94.8%, above the 90% floor.
- `TestFrontParity` passes all 12 token scenarios. `token-usage-json` matches the bash golden byte for byte, with no allowance.

**Findings (none blocking):**

1. **The JSON contract with wsi-claude-token holds.** Its reader, `src-go/internal/wsitoken/usage.go`, takes `schema`, `error`, `name`, `file`, `status`, `reason`, `five_hour_pct`, `seven_day_pct` and `resets.*`. The document is still built by the unchanged `jsonText` (`tokenusage.go:655`).
   - D7: the percentages are still valid JSON numbers, at most 200 (`pct` rejects anything above the limit). Only the last digit can differ, which does not matter against wsitoken's `full` threshold.
   - D8: a reset with a leading nought now comes out as a decimal date. A reset past the year 9999 comes out as a string that is not RFC3339, and `app.go:359-361` already falls back to printing it raw. Either way the JSON stays valid.
   - D9: a header line outside ASCII is skipped. The worst case is a reason saying no figures were sent.

2. **The retry test is weak, not flaky** (`tokenusage_test.go`, `TestTokenUsageRetriesATemporaryNameHoldingTheToken`).
   - `os.CreateTemp`'s random part is a uint32 in decimal, so a name lacks the digit `7` with probability about 0.9^9 ≈ 0.39. Both names of a try lack it about 15% of the time, so roughly one run in seven never reaches the retry.
   - Nothing in the test checks that a retry happened. The run also has no assertion that would notice, because a refused attempt is not recorded in `r.curls()`.
   - The 64-try exhaustion branch (`tokenusage.go:380`, `fetch` at 80% coverage) is never covered.
   - The test also sets `TMPDIR=/tmp`, so it writes real files outside `t.TempDir()`.
   - To make it deterministic: put a temp-name source on `usageReport` or `Front` (a `makeTemp func(dir string) (string, error)`, defaulting to `os.CreateTemp`). The test then hands out one name holding the token followed by clean names, and asserts two attempts, one curl and no token in argv. A second case where every name holds the token pins the exhaustion error.

3. **`colourAllowance` is narrow enough** (`difftest/parity_test.go:116-145`). It covers two named scenarios, applies only to a launcher other than the bash, and strips only colour codes. Real regressions stay caught: colour on a pipe is caught by `TestListTokensColourOnlyOnATerminal`, and the exact colour bytes on a terminal by `TestListTokensIsAnswered:56-58`. Two places to tighten it:
   - (a) It strips colour codes from both sides. Requiring the Go output to hold none (`!sgr.MatchString(got)`) would also refuse wrong colour.
   - (b) `isBash()` counts any set `LAUNCHER` as "not bash". A `LAUNCHER=` that points at the bash launcher therefore gets the allowance, and a bash regression that drops colour would pass in that mode. The default run, with `LAUNCHER` unset, stays strict.

4. **The token stays out of argv, environment and logs.** It goes to curl on stdin (`execx.Feed`). The hand-over reason names only the file (`"a curl argument would hold the token of " + n.file`), is never printed (`front.go:180-192` discards it), and neither is `execx.ErrSecretInArgv` (`secret.go:112`). One corner case: `readToken` builds the name prefix with `filepath.Join(r.tmp, "tmp.")` (`tokenusage.go:278`), which cleans the path, but `os.CreateTemp` builds it as `dir + "/"` without cleaning. With an unclean `TMPDIR` (`/a//b`, `./x`) and a junk token such as `//`, every try fails, so the run exits 1 after 64 tries instead of handing over. Using `r.tmp + "/" + tempPrefix`, or cleaning `r.tmp` once in `prepare`, closes it. Only junk tokens can hit this.

5. **One doc slip in D5** (`front.go`, D5). The `File:` lines are cleaned by `filepath.Join` (`tokens/pool.go:70`), but the `Token storage:` line is printed as built (`pool.go:68`, `p.Dir` from `tokenDir()`, which is string concatenation). So D5 overstates: the storage line still matches the bash. One sentence to fix.

6. **The other divergences are safe for real users.** D1 is only display order; files of one name differ only in date digits, so the token a launch uses is the same. D2 to D4 are display only. D6 still makes 0600 files, removed afterwards. Accepting a TTL of `0` just means always fetch, as the bash does; accepting a leading nought follows D8.

7. **Docs, changelog, PLAN and the version bump match the code.** CCY is 3.99.14.7 in `claude-yolo` and in the changelog. "Used to exit 1" for a junk token is right: the old check never looked at curl's fixed arguments. `GLOBSORT` is added and tested (`front_test.go:193`). `Front.Dir` is gone. There are no install-specific names; `en_GB` and `Europe/London` are generic.