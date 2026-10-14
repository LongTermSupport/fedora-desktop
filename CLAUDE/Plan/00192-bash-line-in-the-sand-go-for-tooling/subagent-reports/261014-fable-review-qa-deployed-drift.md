# Adversarial review: `go-port/qa-deployed-drift` against `integration/261014`

Reviewer: Fable. Subject: commit `f2ff3bb4` (one commit over the integration branch), the Go port of
`scripts/qa-deployed-drift.bash` (`src-go/qa/deployeddrift`, `src-go/qa/cmd/qa-deployed-drift`,
`SPEC-qa-deployed-drift.md` E1-E16). Method: read the spec, the code, the tests and the old bash;
ran `go test` and `go vet`; then tried to break it live with a fake HOME holding planted drift,
on a clean clone of the live tree (a main checkout, so the host half ran), and ran the old bash
over the same clone and the same HOME. `qa-all` was not run.

## Verdict

**Merge after two small fixes (F1, F2); the rest are risks to record, not blockers.**

The comparison itself cannot be fooled by anything I planted: an appended line, a stripped
trailing newline, CRLF, random binary bytes, and a template-shaped edit all fail; a deployed
symlink to the repo copy passes as equal (correct); a `.j2` is never compared. On the live tree
with the same fake HOME the Go and the bash agree exactly: `3 of 6 deployed script(s) differ; 90 not installed; 2 template(s)`, the same three files, the same plays named (tier 1 for
`ftp-camera` and `reclaim`, tier 2 for `lan-scan` giving `play-network-tools.yml`). The two
misfires found are in how the **root** and **HOME** strings are handled, not in the comparison.

## Findings, ranked

### F1 (medium): the container skip keys on the exact string `/workspace`, so any other spelling of the same directory compares the repo against the container's own `/root/.local/bin`

`src-go/qa/deployeddrift/run.go:217` (`if root == containerRoot`) and `parseArgs`
(`run.go:118-137`), which stores `--root`/`--scripts` as typed, never `filepath.Abs` or
`filepath.Clean`. The container has a populated `/root/.local/bin` (pipx shims), so E9 does
not rescue it: the host half runs and prints a green `✓`.

Failing inputs, all run in the ccy container with `HOME=/root`:

| input                                                  | printed                                                                                                                        | expected                            |
| ------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------- |
| `qa-deployed-drift --root /workspace/`                 | `✓ deployed-drift: 0 deployed script(s) match the repo; 96 ...`                                                                | `⚠ ... skipped (CCY container ...)` |
| `cd /workspace && qa-deployed-drift --root .`          | same `✓`                                                                                                                       | same `⚠`                            |
| `cd /workspace && qa-deployed-drift --scripts scripts` | same `✓`                                                                                                                       | same `⚠`                            |
| `qa-deployed-drift --root <symlink to /workspace>`     | same `✓`                                                                                                                       | same `⚠`                            |
| `qa-deployed-drift --scripts /workspace/scripts/`      | `ERROR: source directory not found: /workspace/scripts/files/...` (`filepath.Dir` of a trailing-slash path is the path itself) | the `⚠` skip                        |

The production path is safe today only because the wrapper passes `--scripts "$SCRIPT_DIR"`
where `SCRIPT_DIR` is `cd ... && pwd`-resolved, and `Cwd` comes from `os.Getwd`. E1 says
`--root DIR names the repository root` with no normalisation rule, so the spec is also silent.
The bash was immune because `REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"`.

Fix: in `parseArgs`, `filepath.Abs` then `filepath.Clean` the root (and `filepath.Clean` the
`--scripts` value before `filepath.Dir`); consider `filepath.EvalSymlinks` so a symlinked
`/workspace` is also recognised. Add to E1: "the root is made absolute and cleaned", with a
test for `--root /workspace/` and `--root .` with `Cwd=/workspace` both producing the E7 skip.
A relative root also leaks into the report (`repo:     ./files/home/.local/bin/ftp-camera`)
and a trailing slash doubles (`.../clone//files/...`), both of which the same fix removes.

### F2 (medium): `HOME` with a trailing slash is exit 2 with a misleading message

`run.go:230-233`: `deployedBin := env.Home + "/.local/bin"` then `fs.Stat(env.Host, hostPath(deployedBin))`. `hostPath` only trims the leading `/`, so `HOME=/home/user/` yields the
`fs.FS` name `home/user//.local/bin`, which `fs.ValidPath` rejects. The result is
`ERROR: /home/user//.local/bin: stat home/user//.local/bin: invalid argument` and exit 2, i.e.
the whole QA run fails on an environment detail the bash tolerated (`[ -d "$HOME/.local/bin" ]`
is fine with `//`). `HOME` with a trailing slash is unusual but real (`su -`, some display
managers, hand-exported in a shell rc).

Failing input: `HOME=/home/user/ qa-deployed-drift --root <main checkout>`.

Fix: `filepath.Clean(env.Home)` once in `run` (and reject a relative or empty result as the
existing `HOME is not set` error does); have `hostPath` assert `fs.ValidPath` and return a
clear error otherwise. Add a test.

### F3 (low): a dangling symlink or any unstat-able entry in `files/home/.local/bin` (or a pair directory) is exit 2 for the whole gate, undocumented

`run.go:290-293` (`regularFiles`) and `run.go:347-350` (`scanPairs`) return the `fs.Stat` error.
A symlink in the repo tree pointing nowhere gives `ERROR: files/home/.local/bin/zz-dangling: stat files/home/.local/bin/zz-dangling: no such file or directory` and exit 2; the bash skipped the
entry with `[ -f ]`. Fail-loud is the right instinct (a dangling symlink in the repo is a defect),
but neither the spec nor the message says so; the message reads like an internal fault. The live
tree has no symlinks or subdirectories under the bin dir, so nothing fires today.

Failing input: `ln -s /nonexistent files/home/.local/bin/x` then run.

Fix: either state it in E5/E10 ("an entry that cannot be stat-ed, such as a dangling symlink, is
exit 2: `ERROR: <path>: not a regular file or a readable symlink`") with a test, or `Lstat`
first and skip broken links with a count. The first is preferable here.

### F4 (risk, inherited from the bash; decide, do not merge-block): "exists but is not a regular file" is counted as "not installed"

`run.go:402-405`: a deployed path that is a **directory** or a **dangling symlink** falls into
`notInstalled` and is silent. Planted live: `mkdir ~/.local/bin/rdt` and
`ln -s /nonexistent ~/.local/bin/wsi` both vanished into the `90 not installed` count. A play
cannot have produced either state; both are a wrecked deploy that this gate was built to see.
The bash did the same (`[ ! -f "$dep" ]`), so this is not a regression, but E11 writes the hole
into the spec as intended behaviour ("a regular file, symlinks followed").

Suggested follow-up: split the stat outcome three ways: not present (not installed), regular
file (compare), anything else (drift, "deployed path is a <kind>, not a file"). One extra test.

### F5 (risk, inherited; state it in the spec): mode and ownership are never compared

Only bytes are compared (`sameBytes`, `run.go:425`). Planted live: `chmod 600 ~/.local/bin/nord`
with identical content passes as "match". Every `copy:`/`template:` into `~/.local/bin` sets
`mode:`, so a script that lost its execute bit, or was deployed before a play's `mode:` was
changed, is drift the gate cannot see. The bash had the same limit (`cmp -s`). Comparing mode
needs the play's declared `mode:` (the repo file's own mode is not authoritative; most of the
tree is `-rw-------` in this checkout), so it is a design extension, not a fix. Record it in E12
as an explicit non-goal so a reader does not infer "match" means "deployed as the play says".

### F6 (risk, inherited; acceptable, with the reason written down): "not installed" hides a file a play *should* have deployed

A file **added** to a play that was last run before the addition is, on that host, "not
installed" and never a failure. That is exactly the class Plan 00094 had, for new files rather
than edited ones. The bash made the same choice for a defensible reason (a machine that never
installed a feature is not nagged), and the gate cannot tell "feature not installed" from
"feature installed, play stale" without a per-play signal. A cheap one exists: if **any** file of
a play's set is installed, the play has run here, and a sibling that is absent is drift. The
`pairs` table already groups files by play; the bin files would need `owningPlays` per file.
Worth a follow-up task, not this branch. E11 should at least name the hole.

### F7 (low, spec/doc): the pair table is code and the spec repeats it

`run.go:150-181` (`pairs`) is a Go literal; `SPEC-qa-deployed-drift.md` E10 repeats the twelve
rows as a markdown table; `CLAUDE/QA.md` names the Go table as the authority. Three places say
the set. The spec table is already lossy (it elides three repo globs to `...`). Keep the spec's
table or the Go's, not both: the spec can say "the `pairs` table in `run.go`, currently N rows,
covering the VM lab (play X) and the agent bus (play Y)" and let E6's tests be the proof. Not a
correctness problem: E6 (`TestE6PairMatchingNothingIsExitTwo`) does make a stale row loud,
which is the thing that matters.

Note the consequence of E6 running in the source-tree half: renaming any agent-bus or vmtest
file without updating `pairs` turns **every** QA run red everywhere (container, CI, worktrees),
not only on the deployed host. That is intended and right, but it is a new failure mode the
PLAN.md departures list does not mention.

### F8 (low, spec wording): E9's HOME sentence contradicts the code's order

E9: "HOME unset or empty is exit 2 ..., checked before E7/E8 are decided only in that it is never
silently treated as no directory" is unreadable and, read literally, wrong: the code checks HOME
**after** E7 and E8 (`run.go:226`), so in the container or a worktree an unset HOME is a skip,
not an error. The code's order is the sensible one (the skips need no HOME). Rewrite the sentence
to say that.

## What held up

- Byte comparison: appended line, stripped final newline, CRLF, random binary, a rendered
  template planted under its stripped name next to the `.j2` (not compared, as E16 says), a
  symlink deploy to the repo copy (equal), an extra-table file (CRLF in
  `.local/share/vmtest/guest-cleanup.bash` found, named with `play-vm-test-lab.yml`).
- Parity with the bash on the live clone: identical verdict, identical counts (6 checked, 90
  not installed, 2 templates), identical plays. Counted independently: 44 bin files + 52
  extra-table files = 96; `request.py` excluded from `helpers/vmtest` only.
- The improvements the PLAN.md lists are real and tested: git missing from PATH is
  `ERROR: cannot tell whether <root> is a linked git worktree ...` exit 2 (the bash would have
  compared); a linked worktree of the clone skips with the worktree reason; the real wrapper in
  this review worktree skips; the wrapper in `/workspace` skips with the container reason; the
  E5 `dest:` anchoring, tier-1 `src:` anchoring and tier-2 whole-word test (`lan` vs `lan-scan`)
  all pass; drift goes to stderr only and the pass line to stdout only (E3), which is what
  `gates.driftLine` (`src-go/qa/gates/readers.go:112`) parses: `^[✓⚠] deployed-drift: (.*)$`
  matches both the pass and the three skip lines.
- Skip conditions: `~/.local/bin` present but empty is a run (`0 match; 96 not installed`), not
  a skip, which is correct (the directory is the signal that plays have run here); `~/.local/bin`
  as a symlink to a directory is followed; a real host with its checkout at `/workspace` is
  skipped, same as the bash and documented in E7.
- Exit codes 0/1/2 match E2 and the wrapper (`scripts/lib/go-tool.bash`) passes them through;
  `go test`, `go vet` clean; Go 1.26 so the `p` capture in `scanPairs` closures is per-iteration.
- Ratchet: the `scripts/qa-deployed-drift.bash` row is removed from `scripts/bash-limits.allowlist`
  and the wrapper is 6 code lines; `qa-gates.tsv` row untouched; QA.md and `src-go/CLAUDE.md`
  updated; no install-specific names in the diff (tests use `/home/user`, `/repo`).
- Unreadable deployed copy: the unit test (`TestE12ByteDifferenceIsDriftAndUnreadableIsExitTwo`)
  proves exit 2 through the `Open` seam; it could not be shown live because the container runs
  as root (chmod 000 does not deny root), so the bash and Go both read the file and reported
  drift there. Not a defect; noting the limit of the live check.

## Reproduction

The probe scripts are under `untracked/scratch/` (not tracked): they build the command, clone
the tree, create a fake HOME with the planted states above, and run the old bash and the Go side
by side. Everything in this report was observed from those runs or from `go test`.
