# Adversarial review: `go-port/qa-nokill-containerwatch` against F44

Reviewer: Fable 5.1 subagent. Branch under review: `origin/go-port/qa-nokill-containerwatch`
(one commit, `7e36aa14`). Method: read the bash the branch retires and the Go that replaces it,
built the real binary, ran it and the old bash over the live tree and over a planted probe
repository (54 candidate call sites in a Python file, 24 in a containment.py, a nested module,
8 in a JavaScript target), ran the unit tests, `qa-go.bash` and `qa-bash-limits.bash`. qa-all
was not run.

## Verdict

**Mergeable, with one correctness fix recommended before merge (F1) and one rule gap that
contradicts the spec's own claim (F2).** Nothing the bash caught is lost: on the probe file the
bash's eleven patterns flag lines 19 20 35 39 42 43 44 45 48 51 52 54 and the Go gate flags every
one of those plus eight more (5 13 23 32 40 41 46 47). The live tree passes both (Go counts 7
files, bash 6: containment.py is counted now, B10, as the PLAN says). Fail-fast holds: a missing
target, no Python, a dead git, an unreadable file and an unwritable stream are all exit 2 and
none prints a pass. The qa-changed change does not weaken the count gates and cannot turn a
silent detail gate into a pass; it does judge a multi-reader gate by one reader only (F1).

## Findings, ranked

### F1. qa-changed keeps only the LAST detail reader of a row (medium, correctness)

`src-go/qa/changed/suites.go:39-43`:

```go
for _, r := range g.Readers {
    if r.Detail != nil {
        readers[name] = r.Detail
    }
}
```

qa-all requires every reader of a row to find its line (`src-go/qa/all/gate.go:70-84`,
`summarise`). The row `vmtest-manifest` in `scripts/qa-gates.tsv` has three `detail:` readers.
qa-changed now judges a changed `scripts/qa-vmtest-manifest.bash` by the third alone.

Failing input: edit `scripts/qa-vmtest-manifest.bash` so it exits 0 and prints only
`3 scenario fixture(s) run before a declared reboot` (the first two summary lines gone).
qa-changed: `✓ suite qa-vmtest-manifest.bash: 3 scenario fixture(s) ...`. qa-all: fails,
`ran, but its summary line was not found`. The pre-commit gate passes work the push gate rejects,
which is the drift this change set out to remove. A row mixing `count` and `detail` (none today)
would lose its failed-cases check the same way.

Fix: carry `[]gates.Reader` on `pickedSuite` and reuse `summarise`'s loop (or move that loop into
`qa/gates` so both callers share it). Test: a two-reader row whose script prints one line must be
`cannotRun`.

### F2. A parenthesised `from os import (...)` is not seen, so the bare `kill(pid, 9)` after it passes (medium, rule gap)

`src-go/qa/nokill/rules.go:52`: `os-import-kill` is `(?m)^[ \t]*from[ \t]+(?:os|signal)[ \t]+import\b[^\n#]*\b(?:kill|...)\b`;
`[^\n#]*` stops at the line end. Spec E9 says this rule exists because "the bare `kill(pid, 9)`
that follows has no `os.` to key on", and section 2 says rules are matched over the whole file
"so a call written across lines is seen". A formatter wraps a long import into exactly this shape.

Failing input (probe `core.py:8-12`, not flagged):

```python
from os import (
    getpid,
    kill,
)
kill(pid, 9)
```

Fix: a second pattern `(?s)^[ \t]*from[ \t]+(?:os|signal)[ \t]+import[ \t]*\([^)]*\b(?:kill|killpg|abort|pthread_kill|raise_signal|SIG[A-Z_0-9]*)\b`
with `(?m)`; test with the input above.

### F3. containment.py may build a stop that is a kill: `--time 0` (medium, exception too wide)

`rules.go:72-76` forbids `--force`, `-f`, `--signal`, and the verbs. `podman stop --time 0`,
`-t 0` and `--time=0` deliver SIGKILL at once, which is the harm `containment-signal` names
("it turns a graceful stop into a kill"). Probe `containment.py:5,7,9` all pass:

```python
return [engine, "stop", "--time", "0", cid]
return [engine, "stop", "-t", "0", cid]
return [engine, "stop", "--time=0", cid]
```

Fix: `containment-timeout`: `["']-t["']|--time(?:=0\b|["']\s*,\s*["']0["'])`, or forbid `-t`
and `--time=` outright and require the long form with a positive literal.

### F4. Engine global flags between program and verb evade the lifecycle rules (low-medium)

`rules.go:62-66`: `engine-literal` and `engine-variable` allow only an optional `"container"`
between the program and the verb. Probe `core.py:34` passes:

```python
subprocess.run(["podman", "--remote", "stop", cid])
```

`--remote`, `--connection NAME`, `--root DIR`, `--url`, `docker --context X`, `docker -H host`
are ordinary ways to address an engine. Fix: allow `(?:["']-[^"'\n]*["']\s*,\s*(?:["'][^"'\n]*["']\s*,\s*)?)*`
(flag, optional value) before the optional `"container"`.

### F5. `[ENGINE, "stop", cid]` passes: the variable rule is case-sensitive (low)

`rules.go:65`: `(?:\w+\.)*\w*engine`. Probe `core.py:33` passes. A module constant is the
likeliest spelling for a configured engine. Fix: `(?i)` on the name group only, or `[Ee][Nn]...`;
E10 spec text would say "ends in `engine`, any case".

### F6. Ending a unit is not ending a container, to the gate (low, scope)

The watchdog's subject is transient systemd units; `["systemctl", "stop", unit]`,
`["systemctl", "kill", unit]` is caught by `argv-kill` by luck of the word, but
`["machinectl", "terminate", m]` and `["systemctl", "stop", unit]` pass (probe `core.py:22,24`,
`containment.py:24`). The live watchdog runs no `systemctl` at all (grep is empty), so a rule
`["']systemctl["']\s*,\s*(?:["']--user["']\s*,\s*)?["'](?:stop|kill|restart)["']` costs nothing
today and closes the one termination route in the watchdog's own domain. Inherited from the
bash; stated so a decision is recorded, not as a regression.

### F7. Shell strings without a dash (`"kill %d" % pid`, `f"kill {pid}"`, a JS template) pass (info, inherited)

`shell-kill` needs `kill -`. Probe `core.py:17,18`, `containerReport.js:2` pass; the bash had the
same hole and the spec's E9 says `kill <pid>` prose is unmatched by design. The two cannot be
told apart by text; left as a stated limit. `killall5 -9` also passes (`core.py:21`), trivially
added to `killNames`.

### F8. Trailing comments defeat the multi-line argv match (info)

`["podman",   # comment\n "stop", cid]` passes (probe `core.py:30-31`) because `\s*` cannot cross
the comment. The no-comment case is tested (`TestE9E10MultiLineCallsAreSeen`) and works.

### F9. qa-changed does not run this gate for a watchdog edit (observation, pre-existing)

`scripts/qa-routing.tsv` and `qa-trees.tsv` name neither `nokill` nor `containerwatch`; a change
to `helpers/containerwatch/*.py` runs the Python checks and never the no-kill gate before commit.
Only a change to the gate's own script picks it as a suite. Not this branch's doing; the gate is
cheap and `--root` makes it routable.

### F10. The same qa-changed defect class remains for `drift` and `helpers` rows (observation)

`gates.Population` admits every row whose command is a `scripts/*.bash` (`gates.go:177-187`),
so `qa-deployed-drift.bash` and `qa-helper-tests.bash`, picked as suites, are still judged by
`judgeCount` and fail as "proved nothing" — the defect this branch fixed for `detail` rows. Worth
a row-kind-aware judge while F1 is being fixed.

## What held up

- **Superset of the bash on every probe**, including the forms the brief asked about: `getattr(os, "kill")` (caught by `argv-kill`'s `, "kill")` shape), an aliased `import os as o; o.kill(` (caught by the new `proc-kill` method rule, which closes the alias hole for `os.kill`), `["sudo", "kill", pid]`, `["env", "kill", pid]`, `asyncio.create_subprocess_exec("kill", ...)`, `psutil.Process(pid).kill()`, a list comprehension `.terminate()`, `proc.send_signal(s.SIGTERM)`, `os.kill (pid, 9)` with a space, `sp.getoutput("kill -9 ...")`, a multi-line `["kill",` argv, a nested `sub/nested.py`, `["podman", "container", "kill", cid]`, `--signal=KILL` in containment.py (B9), `cmd.extend(["podman", "stop"])`, a conditional-expression argv.
- **Prose stays free**: the `exec_hint` line and the `podman stop <container>` advice pass (`core.py:49-50`).
- **Scope** is right: what git would publish, recursive under `helpers/containerwatch/`, two named JS files, missing or gitignored target is exit 2, no Python is exit 2, gitignored kill file never read (`TestE3AGitignoredFileIsNeverRead`, `TestEndToEndOnARealWorkTree` on a real repo).
- **Exit codes and streams**: 0/1/2 as specified; every exit-2 line is `ERROR:` on stderr; stdout write failure is exit 2; findings and guidance on stderr, the pass line on stdout where the qa-gates.tsv detail reader `[0-9]+ container-watch file[(]s[)] clean` finds it (row unchanged).
- **Fail-fast in the wrapper**: `set -euo pipefail`, `run_go_tool` returns the tool's status and exit 2 on no go / no cache / build failure.
- **The new rules flag nothing legitimate in scope**: the live watchdog and both JS targets are clean under `.kill(`, `.terminate(`, `killall`; the scope is the watchdog alone, so "elsewhere in the tree" is unaffected by construction.
- **qa-changed**: a detail gate that exits 0 with no matching line is `cannotRun`, not a pass (`TestADetailGateWhoseReaderFindsNothingCouldNotRun`); count gates are untouched (`judgeCount` unchanged); `qa/all` and `qa/gates` are untouched, so qa-all's behaviour is identical.
- **Ratchet row** `scripts/qa-nokill-containerwatch.bash 276 5 248 -` removed legitimately: the wrapper is 6 code lines; `qa-bash-limits.bash` is green (`BASH-LIMITS-OK 449 bash files`).
- **`qa-go.bash` green**: fmt, vet, race tests, 94.6% coverage, golangci-lint, project rules; `go test` over `nokill`, the command, `changed` and `gates` passes.
- **Tests assert behaviour**: one or more per expectation E1-E15, named by number; red-then-green is recorded in the journal; a fake FS and a fake `Published` keep the package pure; the command's tests use a real `git init` tree.
- **PLAN, journal, QA.md, src-go/CLAUDE.md claims checked true**: live-tree 7 vs 6 count, "ported, not merged", B1-B15 each demonstrated by a test or by this probe; the spec's "known limit" paragraph honestly states the text-not-syntax boundary (F7 falls inside it; F2-F5 do not, they are regex shapes the spec promises to see).

## Reproduction

The probe tree is a throwaway `git init` repository under `untracked/scratch/` with the files
named above; run the built binary with `--root <probe>` and the F44 bash from a `scripts/`
directory beside symlinked `helpers/` and `extensions/`. Line numbers in this report are the
probe's.
