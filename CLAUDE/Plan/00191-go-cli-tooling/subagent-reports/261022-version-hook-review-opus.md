VERDICT: FIX-FIRST

I tested this in a scratch worktree at 839de16e, which I have since removed. The probe commit there was never on any branch.

1. **BLOCKING: the hook lets a front change through, with no message, when the commit is made from a symlinked path to the checkout.**

   - `scripts/git-hooks/lib/ccy-staged.bash:22,28` strips `root=$(pwd -P)`, the physical path, from the directories `go list` prints. But `go list` runs after `cd src-go`, and Go takes its working directory from `$PWD`, which is the symlinked path. The strip does nothing, nothing in the closure matches, and the script prints nothing and exits 0.
   - Evidence from a real `git -c core.hooksPath=… commit` with `src-go/internal/gitx/gitx.go` staged and no bump:
     - from the physical path: rc=1, "COMMIT REJECTED: CCY version bump required";
     - from a symlink to the same checkout: rc=0 and the commit went in.
     - Running the lib directly from the symlinked directory also printed nothing (rc 0) for `gitx.go`.
   - This is the silent pass that fail-fast forbids, and a host checkout reached through a symlinked projects directory is a normal setup.
   - Fix: compare paths that cannot disagree. Use `{{.ImportPath}}` mapped onto the module path or `vendor/`, or run `cd "$(pwd -P)/src-go"` before `go list`. Add a test case that runs the hook from a symlinked repo root.

2. **Red/green proof: done.**

   - Old hook plus the new test cases: 108 passed, 6 failed (`tokens.go`, `leaf.go` and `go.mod` without a bump, each "rejected" and "says to bump").
   - New hook: 114 of 114 passed.
   - So the test does exercise the change.

3. **Cost and failure behaviour: correct, apart from finding 1.**

   - A docs-only commit takes 0.003s and needs no `go`. `go list` is only asked when a `src-go/` file outside the front's own paths is staged.
   - A `src-go` file in the closure takes about 0.06s with a warm cache.
   - Missing `go`: "COMMIT REJECTED: go list could not name…", exit 1.
   - A `go.mod` asking for go 1.99 is refused loudly with `GOTOOLCHAIN=local`.
   - A hostile `GOFLAGS=-mod=mod GOTOOLCHAIN=go1.99.0` in the environment is overridden.
   - The vendor tree is tracked, so every worktree has it.
   - Classification is right: flagged are vendored `x/sys/unix`, `gitx`, `go.mod`; not flagged are `watchlist`, `vendor/modules.txt`, `tokens/testdata`.

4. **Not blocking, gaps in the closure check:**

   - (a) Embed assets in a subdirectory of a closure package (`//go:embed dir/*`) would be missed. Today all four embeds sit in `ccylaunch`, which matches by path anyway.
   - (b) The closure is computed from the working tree, not the index. This is harmless in practice.
   - (c) `--name-only` sees only the new name of a rename. That predates this change.

5. **The awk cut is clean.** The pre-commit diff touches only the old `CCY_STAGED` loop, replaced by the lib call plus `mapfile`. The allowlist line went from 277 to 273, so it only shrank. `qa-bash-limits` reports BASH-LIMITS-OK. The new lib has 2 functions, no associative arrays and no parsing, and names nothing install-specific.

6. **The other review findings are fixed as claimed.**

   - meta-deploy says CCY 3.99.14.11 and container 2.78.14 in both places, lists all six flags, and the after-run check compares `--version`, `--help`, `--list-tokens` and `--token-usage` against `CCY_LEGACY=1`.
   - `main.go` and `play-claude-yolo.yml` list the six flags; `CCY_VERSION` is 3.99.14.11 with a changelog entry; `ContainerRules.md` describes the gate.
   - PLAN.md: 4.3 and 4.4 ticked with named follow-ups, duplicate bullet gone, count 82 of 82, Task 4.6 added.
   - Not blocking: `--prevent` and `--export-token` are absent from the after-run check (they change state, so this is defensible). Two edited comment lines run past the wrap width (`main.go:3`, `meta-deploy.list:102`).
