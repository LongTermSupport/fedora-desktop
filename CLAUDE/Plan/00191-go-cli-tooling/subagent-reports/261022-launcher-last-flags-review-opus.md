VERDICT: FIX-FIRST

I changed nothing in the branch. The only files I created are scratch files under `/workspace/untracked/scratch/rv-*`.

**What I ran at f5f7ca0f, all green:**
- `go test -race -count=1` on `./internal/ccylaunch/...` and `./internal/gitx/...`, with and without `-tags difftest`.
- Seven launcher suites: `test-ccy-{info-flags,env-local-dist,gitignore-safety,token-handoff,token-import-cleanup,token-mode,flag-check}.bash`.
- `./scripts/qa-changed.bash` (exit 0).

**Blocking**

1. **The safety gate on tracked files is looser than the bash's when git quotes a path, and the run is not handed over.**
   - `src-go/internal/gitx/gitx.go:97` runs `git ls-files -z`, which gives unquoted names. `src-go/internal/ccylaunch/inrepo.go:147-148` then tests `path.Base` against the safe list.
   - The bash (`lib/common.bash:561,581`) runs plain `git ls-files … 2>&1`. With the default `core.quotePath`, git quotes any path holding non-ASCII, `"`, `\` or control bytes, and `basename` keeps the closing quote.
   - I checked this on a real repo. A tracked `.claude/ccy/café/README.md` lists as `".claude/ccy/caf\303\251/README.md"`, and the bash's basename is `README.md"`. That is not on the safe list, so the bash prints its SECURITY ALERT and exits 1. The Go reads `README.md`, judges it safe, then writes the prelude and answers.
   - Related: the bash's `2>&1` also counts any line git prints on stderr while still exiting 0 as a "tracked file", and refuses.
   - This breaks F12's own promise ("hands over … listing a tracked file under .claude/ccy that the bash refuses") in the one check that exists for security, and it is not declared as a D-item.
   - Fix: hand over when any tracked path would be quoted (any byte outside printable ASCII, or `"` or `\`), or when git wrote anything on stderr. Add a test case for each.

**Not blocking**

2. **`--export-token NAME` writes the token to no file.** The brief assumed it does. `exporttoken.go:109-112` sends the import script, with the token in it, to stdout only, exactly as the bash's `export_token` does (`token-management.bash:1814`).
   - Handed-over reasons and error text name the file, never the token. The token is not put in any argv or environment variable.
   - So umask, O_EXCL, a symlink at the target and a tracked path do not apply to the token. `TrackedUnder` is only the prelude's gate; it plays no part in where the token goes.
   - The only writes are the import script's, on the target machine. They use the bash's body verbatim: 0700 directories, 0600 file, written with `printf >` (no O_EXCL, as before).

3. **The prelude follows symlinks when it writes, as the bash does.**
   - `readText` (`inrepo.go:177-183`) uses Lstat then Stat. So a `.gitignore` or `ccy.env.local.dist` that is a symlink to a regular file elsewhere is overwritten through the link (`inrepo.go:232,287`). The same goes for `chmod` on the token directories.
   - This matches the bash's redirections, so it is not a regression. A future tightening could hand over on any symlink.
   - `allowed-hostnames`, `sessions` and `Dockerfile` hand over on Lstat presence, dangling links included, which is correct.

4. **Version numbers with leading zeros print differently, and this is not declared.** The bash prints the matched digits as they are (`common.bash:742,760`). Go prints them through `strconv.FormatInt` (`inrepo.go:279,298`), so a dist line `version 04` reads "(version 04)" from the bash and "(version 4)" from Go. Hand over on a leading zero, or print the matched text.

5. **"D10" means two things in the same PLAN paragraph.** `PLAN.md:596` uses it for the package doc's colour divergence, and `PLAN.md:605` ("per the D4/D10 default") for Task 4.2's decision gate (`PLAN.md:243`). The owner gate itself is stated correctly. Rename one of them, e.g. "divergence D10".

**Checked, sound**
- Every hand-over is decided before anything is printed or written: `inRepoFlag` runs `readInRepo` and the export check before `apply`.
- D11 (a failed write exits 1) is safe. D10 is narrow: colour only when the stream is a terminal and `NO_COLOR` is unset.
- `colourAllowed` now needs Go output with no colour codes at all, is refused to any bash script (shebang check), and its third scenario `export-token-missing` is named and justified. `TestColourAllowanceIsNotTheBashs` pins all of this.
- The embedded texts test (`inrepo_test.go:19-36`) extracts the text from `lib/common.bash` and `lib/token-management.bash` as they are now. It does not compare a copy with itself.
- `--prevent` matches the bash. It is handed over whenever `allowed-hostnames` exists, so "already disabled" stays the bash's. Otherwise it creates `.claude/ccy`, appends `never` and prints the same text on stdout.
- Parsing matches the bash: `--` and an empty name go to the bash, and a name outside the token grammar is handed over.
- The `ls`/glob order matches D1. The token's trailing newlines are trimmed as `$(cat)` does, and a token holding a NUL is handed over.
- The prelude steps follow the bash's order (`claude-yolo:432-441`, then `1082-1087`), and the token directories match the bash's `mkdir`/`chmod 700`.
- `CCY_MAX_AGE`, `CCY_RELAUNCH_DEADLINE_EPOCH` and `CCY_RESTART_RELAUNCH` are handed over.
- The claim that phase 2 is done is accurate. What remains in bash is `--top`, the interactive `--export-token`, and the launch path (phases 3 to 5, waiting for the owner). PLAN and `docs/ccy.md` say this.
- CCY is bumped to 3.99.14.9 with a changelog entry, and `src-go/CLAUDE.md` now cites F1-F13.
- The diff contains no install-specific names.