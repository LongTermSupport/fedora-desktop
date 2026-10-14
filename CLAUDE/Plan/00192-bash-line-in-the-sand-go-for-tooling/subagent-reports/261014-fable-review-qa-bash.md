# Adversarial review: `go-port/qa-bash` against `integration/261014`

Reviewer: Fable (sub-agent), branch `review/qa-bash` cut from the port branch. Method:
[src-go/CLAUDE.md](../../../../src-go/CLAUDE.md) "How a port is built", applied as an attack: the
real binary (`go build ./cmd/qa-bash`) was run over a fixture work tree of planted defects and over
the live tree; every shellcheck configuration route was set to hide a finding; thirteen mutations
were applied to the source and the test suite run against each. No `qa-all` run. Spec under review:
[SPEC-qa-bash.md](../SPEC-qa-bash.md) (S1-S15, B1-B12); qa-changed change: SPEC-qa-changed B35.

## Verdict

**Mergeable with two follow-ups to record, one of them a spec gap.** The port catches everything
the bash caught and closes the rc-file and environment holes the bash had. The pins are proved by
tests: eleven of thirteen mutations are killed, and the two survivors are an equivalent mutant and an
untested-but-safe path. The findings below are not regressions against the bash; they are holes the
bash had too, which this port's own spec (S8, "no file beside a script can change the rules") now
makes visible, plus one behaviour change the spec does not name.

## Findings, ranked

### 1. A `# shellcheck disable=` directive hides a gating finding, and nothing in the gate notices

**Where:** `src-go/qa/bash/shellcheck.go:31` (`shellcheckArgs`) builds the only invocation; there
is no directive scan anywhere in `src-go/qa/bash/`. QA.md:998 says suppression comments "are blocked
by the hooks daemon"; QA.md:1208 says "never add a suppression directive".

**Failing input** (fixture `scripts/disable-line.bash` and `scripts/disable-all.bash`):

```bash
#!/usr/bin/bash
# shellcheck disable=all
f() {
  local x=$(date)   # SC2155, the repository's own defect class
  echo "$x"
}
f
exit 300            # SC2242 error
```

Result: the file is reported `pass`, zero diagnostics, while the same content without the directive
fails with one warning and one error. Both the line form and the file-wide `disable=all` form hide
everything.

**Is the ban enforced, or discouraged?** Discouraged only. The one enforcement point is the hooks
daemon's `qa_suppression` handler (`.claude/hooks-daemon.yaml:108`), which runs at `Write`/`Edit`
time in a Claude session. It does not see a Bash heredoc or redirect (CLAUDE.md states this), a human
editor, a merge, or a file that already exists. Proof it is not a gate: the `shell` discovery list
holds **21 live `# shellcheck disable=` directives** today (`.claude/init.sh` x5, `run.bash` x2,
`files/var/local/claude-yolo/lib/token-management.bash` x4, `files/var/local/claude-yolo/lib/common.bash`,
`files/etc/profile.d/zz_lts-fedora-desktop.bash`, `files/home/.local/bin/rdt`,
`scripts/test-ccy-teams.bash`, and five in plan scripts). Nothing fails on them.

**Should this gate enforce the ban the way qa-python uses `--ignore-noqa`?** Yes, and it is the only
place that can: shellcheck has no `--ignore-directives` flag (checked against 0.11.0), so the pin
`--norc` + empty `SHELLCHECK_OPTS` closes every route *except* the one written into the script
itself. S8's claim that "no file beside a script can change the rules" is literally true and
practically incomplete: the script itself can. The port is the right moment because the Go already
owns "how the engines are invoked"; a directive scan is twenty lines against the discovered files.
Proposed shape, for the plan owner to decide: a line matching `^\s*#\s*shellcheck\s+disable=` in a
discovered file is a gating finding (reported like a diagnostic, level `error`, code 0 or a
synthetic marker), with a ratchet allowlist for the 21 existing sites that may shrink and never
grow, exactly as `bash-limits.allowlist` works. qa-changed's per-file shellcheck would need the same
scan, or the two verdicts differ (the reason S9 exists). This is not the bash's regression; it is a
spec gap: **S8 should state the limit** ("a directive inside the script is not pinned; see Sx") or
the gate should close it.

### 2. `# shellcheck shell=bash` on a `#!/bin/sh` file hides the POSIX-compliance warnings

**Where:** same invocation, `src-go/qa/bash/shellcheck.go:31`; same class as finding 1 with a
different directive.

**Failing input** (`scripts/posix-hidden.sh`):

```sh
#!/bin/sh
# shellcheck shell=bash
f() {
  local x=1          # SC3043 warning under sh: hidden
  [[ $x = 1 ]] && echo y   # SC3010 warning under sh: hidden
}
f
```

`shellcheck --norc --external-sources` exits 0 on this; without the directive the port gates it with
two warnings (fixture `scripts/posix.sh`). The file still runs under `/bin/sh` and still breaks
there. The tree has 16 `shell=` directives; 15 are `shell=bash` on extensionless or `.inc.bash`
libraries with no shebang (legitimate: it names the dialect of a sourced file) and one is `shell=sh`
on `files/etc/profile.d/zz-github-ssh-443.sh` (stricter, fine). `files/etc/profile.d/gh-multi-profile.sh`
is a `.sh` declared `shell=bash`; it is sourced by bash login shells so that is arguably right, but
it is the pattern. A directive scan for finding 1 should treat `shell=` **that contradicts a shebang**
as the same defect; `shell=` on a file with no shebang is correct use.

### 3. `--external-sources` changes a verdict the spec does not mention: an unparseable sourced file in an excluded tree is now a gating warning

**Where:** `src-go/qa/bash/shellcheck.go:31` adds `--external-sources` (B3, S9). S9 says the flag
is "so a sourced library's definitions are seen" and the port report says the only live-tree change
is 5 fewer SC1091 info findings. Both are true today, and neither names the other direction.

**Failing input** (`scripts/src-broken.bash` sourcing a file under the excluded `roles/vendor/`):

```bash
# roles/vendor/broken.sh (excluded tree, never checked on its own)
#!/usr/bin/bash
if [ 1 ]; then
# scripts/src-broken.bash
#!/usr/bin/bash
source roles/vendor/broken.sh
echo ok
```

Old invocation: `SC1091 note: Not following` (info, advisory). Port invocation:
`SC1094 warning: Parsing of sourced file failed. Ignoring it.` -- **gating**. So a vendored or
daemon-owned tree (`roles/vendor`, `.claude/hooks-daemon`, `.claude/ccy`), deliberately excluded by
`qa-trees.tsv` because this repo does not own its findings, can now fail the gate through any script
in the tree that sources it by a root-relative or `# shellcheck source=` path. On the live tree
nothing does yet (both runs exit 0), so this is a latent behaviour, and defensible: a script that
sources an unparseable file is broken. But it is a change the spec should own in S9 or as a B-row,
because the exclusion table's promise ("must not gate on") is now conditional. Also worth knowing:
shellcheck resolves a plain `source ./x` **relative to the working directory (the root), not the
script's directory** (verified: `sub/a.bash` sourcing `./sibling.bash` gets SC1091), so the flag
only follows root-relative paths and `# shellcheck source=`/`source-path=` directives. The five
SC1091 that vanished were exactly those.

Findings inside a followed file are **not** reported by shellcheck (verified with a warning planted
in `roles/vendor/lib.sh`: zero diagnostics on it, exit 0), so S10's "a diagnostic on a file that was
not given is exit 2" is unreachable with shellcheck 0.11.0. It is correct as a defence; it is not a
behaviour anyone will see.

### 4. The shellcheck time limit is reachable by one file, and turns a slow lint into "the run could not complete"

**Where:** `src-go/qa/bash/run.go:46` (`lintTimeout = 15 * time.Minute`), one process for every
file (S8). The bash had no limit of its own; qa-all's 30-minute stage cap was the only one.

**Input:** a generated 5.4 MB, 300k-line script (`scripts/huge.bash`: 100k times
`v=...; echo "$v"; [ -n "$v" ] && echo yes`). shellcheck alone on the real tree's biggest file
(`files/var/local/claude-yolo/claude-yolo`, 204 KB; `.claude/init.sh` at 166 KB takes 0.94 s) is
quick; the whole live tree takes 83 s. The 5.4 MB file: see the result below. Whatever the number,
the design point stands: one pathological file does not fail *as a file*, it aborts the whole stage
with exit 2 because every file shares one process and one clock. That is fail-fast and correct for
a QA tool; it is just not what S7's per-file `bash -n` treatment would lead a reader to expect, and
S8/S10 should say "or the 15-minute limit" where they say "a timeout". A per-file fallback is not
suggested (the bash never had one either).

Result: `ERROR: shellcheck did not finish within 15m0s`, exit 2 after 15m0.5s of CPU, no report
written, the other 15 files' verdicts lost with it. The timeout path works as S10 says and the
message is the right one (the `TimedOut` branch, which finding 5 notes no test drives). The old bash
on the same input would have run until qa-all's 30-minute cap, so this is strictly better; it is
recorded so nobody reads a future exit 2 on this message as a shellcheck crash.

### 5. Two mutations survive the test suite

Run against `go test ./bash/... ./cmd/qa-bash/...` with the mutation applied and the source restored
after each (`untracked/scratch/rev/mutate.bash`, not committed):

| mutation                                                               | result                                                                                                                                                                                                                                                                                                                                |
| ---------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| gate on `error` only (drop `warning`)                                  | killed (3 tests)                                                                                                                                                                                                                                                                                                                      |
| drop `--norc`                                                          | killed (`TestShellcheckIsInvokedWithItsRulesPinned`, real `TestRealNestedRcFile...`)                                                                                                                                                                                                                                                  |
| drop `SHELLCHECK_OPTS=`                                                | killed (same two)                                                                                                                                                                                                                                                                                                                     |
| drop `--external-sources`                                              | killed (argv test, real `TestRealSourcedLibraryIsFollowed`)                                                                                                                                                                                                                                                                           |
| coverage guard off (`len(missed) > 1000`)                              | killed (`TestATrackedScriptNotPublishedIsExitTwoAndListed`)                                                                                                                                                                                                                                                                           |
| syntax error recorded nowhere                                          | killed (3 tests)                                                                                                                                                                                                                                                                                                                      |
| "reported on a file not given" check off                               | killed (`TestShellcheckStatusAndOutputAreChecked`)                                                                                                                                                                                                                                                                                    |
| shellcheck status >= 2 tolerated                                       | killed (same)                                                                                                                                                                                                                                                                                                                         |
| report status stuck at `pass`                                          | killed (4 tests)                                                                                                                                                                                                                                                                                                                      |
| exit 0 on a failed file                                                | killed (4 tests)                                                                                                                                                                                                                                                                                                                      |
| unknown level accepted                                                 | killed (`TestShellcheckStatusAndOutputAreChecked`)                                                                                                                                                                                                                                                                                    |
| **`[` prefix check on shellcheck stdout removed** (`shellcheck.go:44`) | **survived**: equivalent mutant; `json.Unmarshal` into `[]json.RawMessage` rejects the same inputs, including empty stdout. The check is redundant, not a hole.                                                                                                                                                                       |
| **`if res.TimedOut` branch removed** (`run.go:191`)                    | **survived**: no test drives a timeout through the fake runner. Safe today because a killed process also returns a non-0/1 status that `shellcheck()` and `syntax()` already treat as exit 2, but the message would then be "invocation failed (status -1)" instead of "did not finish within 15m0s". One fake-runner case closes it. |

The journal's two mutation claims (drop `--norc`, drop `--external-sources`) are confirmed.

### 6. Minor

- `src-go/qa/cmd/qa-bash/main.go:56` writes the report `0o600`; the bash wrote it with the umask
  (`0644`). qa-all reads it as the same user, so nothing breaks, but a human `jq` from another
  account on a shared machine would not. Match qa-python's choice, whichever it is.
- `src-go/qa/bash/discover.go:70` strips `g.root+"/"` from discovery's output; if discovery ever
  printed a path not under the root (it is spec'd not to), the absolute path would be passed to
  `bash -n`/shellcheck from the root and `relative()` would keep it, so paths would be inconsistent
  rather than wrong. Not reachable through `qa/discovery` as it stands.

## What held up

Run on the real binary over the fixture (16 shell files, every shellcheck config route armed:
`.shellcheckrc` at the root and beside the scripts, `$XDG_CONFIG_HOME/shellcheckrc`,
`$HOME/.shellcheckrc`, `SHELLCHECK_OPTS="-e SC2155 -e SC2242"`; control: the same shellcheck
without `--norc` from the same shell exits 0 on the planted warning and error):

- **Config pin holds on every route.** `--norc` covers the nested, root, XDG and home rc files;
  `proc.Cmd.Env` is appended after `os.Environ()` (`internal/proc/proc.go:68`) so `SHELLCHECK_OPTS=`
  wins over the caller's value. 17 gating findings reported; all 9 planted files failed.
- **Everything the bash caught is caught**, and more precisely: syntax error (`bash -n` exit 2,
  message relative to the root), SC2242 error, SC2155 warning (the repository's class), extensionless
  `bin/tool` by shebang, `#!/bin/sh` (SC3043/SC3010 under the sh dialect), `#!/usr/bin/env bash -e`
  (discovered, then SC2096 error on the shebang itself), CRLF (SC1017 error on every line; `bash -n`
  alone passes a CRLF file, so shellcheck is load-bearing here, as it was before). Info (SC2086) and
  style (SC2006) are counted in the advisory line and gate nothing. A warning in a sourced library
  inside the set (`lib/inside.bash`) is reported once, as its own file, not twice.
- **Failure accounting is now per file** (B7/B8): `9/16 files failed`, `failures` carries each
  file's codes (`SC2096:1 (error) ...; SC2155:3 (warning) ...`), `summary` adds up; the bash wrote
  `failures: []` and counted "shellcheck" as one failure. qa-all's reader (`qa/all/report.go:47`)
  takes `summary` and `failures` and will now list the files.
- **Exit codes and fail-fast:** unknown argument exit 2 with `ERROR:` and usage on stderr, nothing
  on stdout; no shellcheck on `PATH` exit 2 naming `play-devtools.yml`; closed stdout (`>&-`) exit 2
  while `>/dev/null` is allowed; unwritable `QA_JSON_OUT` exit 2 with no stdout at all (S3: no
  verdict without a report); empty `QA_JSON_OUT` falls back to `/tmp/qa-bash-results.json`; a root
  with no shell file, and a root that is not a git repository, both exit 2 through discovery's
  message; nothing is written on any exit 2.
- **Root normalisation:** `--root ../` from a subdirectory, `--scripts scripts/` relative, and an
  absolute `--scripts .../scripts/` with a trailing slash all produce root-relative paths in stdout
  and in the report (`"file": "scripts/..."` 38 times in the relative-root run).
- **Live tree:** 433 files, exit 0, 162 advisory findings, 83 s; matches the port report. The
  in-repo `files/var/local/claude-yolo/.shellcheckrc` the bash used to read holds comments only, so
  `--norc` changes nothing there today (the journal says the same).
- **B35 is argv and env only.** All 20 golden files under `qa/changed/testdata/deviation/` changed
  by exactly one line each, the shellcheck argv (`--norc` inserted); `git diff -U0 | grep -v 'shellcheck --'` over the goldens is empty. `deviations.json` adds B35 to the reason text. The
  qa-changed `bash` check (`checks.go:70`) runs the whole gate on the tool, discovery, wrapper and
  `qa-trees.tsv` paths (`qa-routing.tsv` rows) and not on an ordinary script
  (`TestTheShellGateRunsWhenItsToolDiscoveryWrapperOrTableChange`). The drift test
  (`realtree_test.go`) now reads the Go gate for the bar and `--external-sources`.
- **Ratchet:** `scripts/qa-bash.bash` row removed from `bash-limits.allowlist`; the wrapper is 7
  code lines; `qa-bash-limits.bash` passes (`449 bash files: 267 exempt, 182 checked`).
- **Docs:** QA.md gate row names `--norc --external-sources` and the empty `SHELLCHECK_OPTS`;
  `src-go/CLAUDE.md` has the package paragraph; `docs/` has no behavioural description to drift
  (one historical mention in `ccy-changelog.md`). `go vet` clean; `go test` green.

## Recommendation to the plan owner

Merge the port. Open two items on Plan 00192 rather than blocking: (a) decide whether the shell gate
scans for `# shellcheck disable=` and shebang-contradicting `shell=` with a ratchet allowlist of the
21 + 1 existing sites, or amend S8 to state the directive limit (finding 1 and 2); (b) add the
SC1094 promotion to S9/B-table and a fake-runner timeout case (findings 3, 5).
