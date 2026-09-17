# QA Review — Plan 00099 round 6, commit `cca0d9e7`

**Verdict**: FIX-BEFORE-MERGE — 1 blocking, 7 should-fix, 7 nits.

All six round-5 findings are genuinely fixed, and I verified each by **execution**, not by
reading. To do that I built a stub host under `untracked/scratch/` (fake `findmnt`/`pgrep`/
`rclone`, a real background process whose argv carries `rclone mount <path> --rc-addr=…`, and
a `$HOME/.local/bin` holding stub clients plus a symlink to the real `rclone-rc-auth.bash`)
and ran `acceptance.bash` end to end. It reached `COVERAGE: 9 of 9` and rendered a verdict.
That harness has been removed; `git status` shows no change from this review.

That same execution found the blocking finding below, which the plan's own round-5 harness
could not have found because it extracts check [6] and never runs check [0].

## Blocking

### 1. `acceptance.bash:182` — the gate dies with exit 1 and NO output when a mount publishes no `--rc-addr`; the ABORT block written for that case is unreachable

```bash
RC_ADDR=$(grep -oE -- '--rc-addr=[^ ]+' <<< "$rc_cmdline" | head -n1 | cut -d= -f2)
```

Under `set -euo pipefail` (line 16) `grep` exits 1 when the cmdline carries no `--rc-addr`,
`pipefail` propagates it, and errexit kills the script. Lines 187–193 — `ABORT  the mount on
$rc_mount publishes no --rc-addr … Re-deploy the mount: ansible-playbook … play-rclone.yml` —
can never run via this path.

Executed, full gate, stub mount started without `--rc-addr`:

```
[0] precondition: an rclone mount is present and publishes an RC address
  PASS  1 rclone mount(s) present
EXIT=1
```

No `ABORT`, no `COVERAGE`, no `ACCEPTED`/`REJECTED`. Isolated to the line:

```
$ bash repro182.bash        # the line verbatim, set -euo pipefail
EXIT=1  -> died before the -z test
$ bash repro182b.bash       # same line, set -eu (no pipefail)
REACHED (no pipefail); RC_ADDR=[]
```

This is round 5's `note` defect exactly: a branch nothing has ever executed, dying mid-check
with no verdict, on the scenario check [0]'s own comment calls the normal case (multi-mount
hosts, where the FIRST `fuse.rclone` mount need not be an RC mount). `bash -n` is OK and
`shellcheck -x` exits 0 on the file.

**It is a four-instance population, and I tested all four.** The identical line is at
`rclone-cache-warm:105`, `rclone-cache-status:169` and `rclone-tail:119`, all three under
`set -euo pipefail`.

- `rclone-cache-warm --fast` is **live** — executed against the no-`--rc-addr` mount:

  ```
  Refreshing directory cache (fast mode)...
  EXIT=1
  ```

  with nothing on stderr; lines 111–113 (`ERROR: no rclone process with --rc-addr found for
  … --fast mode requires the mount to be started with --rc (play-rclone.yml does this)`)
  never print. Control run with `--rc-addr` present: `Directory cache refreshed.  EXIT=0`.
- `rclone-cache-status --no-dir` and `rclone-tail --once` **survive** — both printed
  `remote  (no --rc; cannot query)`, exit 0 — because their copy sits inside a
  `findmnt | while read` pipeline subshell where errexit is suspended. Latent, not live.

Fix: `if ! rc_addr_raw=$(grep -oE -- '--rc-addr=[^ ]+' <<< "$rc_cmdline"); then
rc_addr_raw=""; fi` (or the library's `[ -z "$matches" ]` shape), in all four places. The
library already got this right at `rclone-rc-auth.bash:195` and the lesson was never carried
to its four siblings — the `qa-python.bash`/`qa-bash.bash` pattern in AgentNotes.

## Should fix

### 2. The gate returns ACCEPTED on a host where one of this plan's five migrated clients is not deployed at all

Executed: with `$BIN` containing only `rclone-cache-status`, `rclone-tail`, `ftp-camera` and
`rclone-rc-auth.bash` — **no `rclone-cache-warm`** — and check [7] fed a realistic drift-gate
pass line:

```
[3] no deployed script calls 'rclone rc' directly
  PASS  every deployed client goes through rclone_rc (3 scanned)
[7] no repo-owned script differs from its deployed copy
  PASS  repo and host are in sync
COVERAGE: 9 of 9 checks executed (11 assertion(s) passed, 0 failed)
ACCEPTED — every declared check ran exactly once and every assertion passed.
```

The drift line I supplied said `; 12 not installed on this host`. Two causes, both this
repo's named class:

- `acceptance.bash:277` states a numerator with no denominator — "3 scanned", never "3 of the
  5 this plan owns". `qa-deployed-drift.bash:182–184` counts a never-deployed file as
  `NOT_DEPLOYED`, not drift, so nothing else covers it.
- `acceptance.bash:432–440` reduces the drift gate's entire output to a `*skipped*` substring
  test. Plan 00081 changed that gate to state `$NOT_DEPLOYED` **always, including zero**
  (`qa-deployed-drift.bash:280–284`) precisely so a reader sees it; check [7] discards it and
  prints "repo and host are in sync". That is the S1 laundering pattern recurring on the same
  check.

### 3. Check [6b] is still the stand-in that rounds 2–5 removed from check [6]

`acceptance.bash:409` calls `rclone_rc --url=… vfs/refresh "fs=$REFRESH_FS"` from the gate.
`rclone-cache-warm:116–118` calls it as `("fs=$RCLONE_SOURCE" "recursive=true")` plus
`remote=$REL_PATH`, after its own `--rc-addr` walk (the one in finding 1). The catalogue entry
names the client: `"6b|vfs/refresh (rclone-cache-warm --fast's endpoint) authenticates"`.
Nothing in the gate ever executes `rclone-cache-warm` — grepped the whole file. Four rounds
established that approximating a client's call is not testing it; the adjacent check still
does it.

### 4. Check [6]'s `!= RC_ADDR` branch is an unconditional PASS for any non-empty stdout

`acceptance.bash:379–384`. Executed, two stub clients:

```
# client prints a banner line, then the address
  NOTE  ftp-camera resolved Mount: remote:cam -> /mnt/cam
  PASS  ftp-camera's copy preflight authenticated at its own mount's Mount: remote:cam -> /mnt/cam
ACCEPTED — every declared check ran exactly once and every assertion passed.

# client prints an error string on stdout and exits 0
  PASS  ftp-camera's copy preflight authenticated at its own mount's ERROR: rclone remote control did not answer
ACCEPTED — every declared check ran exactly once and every assertion passed.
```

The deployed client's stdout is clean today — I ran the real
`files/home/.local/bin/ftp-camera --copy-preflight` with a stubbed `getent` and it printed
nothing on stdout before `copy_preflight` — so this is latent, not a live false green. But the
one branch that admits a disagreement admits EVERYTHING, in the check this plan rebuilt four
times. Require a single line matching `host:port` before entering it; anything else is `bad`.

### 5. PLAN.md's Delivery list is wrong, and Task 5.9's scope rests on it

`PLAN.md:280–284` says "**Three** later commits changed files that deploy to the host" and
names `17819cda`, `510dbbf4`, `113fe2e1`. There are **five** after Task 5.7's 15:41 host run:

```
cca0d9e7 09-16 19:13   113fe2e1 09-16 18:47   68c8bbfa 09-16 18:24
510dbbf4 09-16 18:03   17819cda 09-16 17:24
(git log --format='%h %ad %s' 942fb724..HEAD -- files/home/.local/bin/)
```

`68c8bbfa` was named by round-5 nit 9 and not actioned; `cca0d9e7` is the commit under review,
it changed `files/home/.local/bin/ftp-camera`, it edited this very bullet, and it did not add
itself.

### 6. Task 5.10's "Every finding actioned" is an overstatement

`PLAN.md:169–181`. Round-5 nits 8 (`START_MARKER` leak), 9 (the `68c8bbfa` omission) and 10
(`play-ftp-camera.yml`'s command list) are unactioned and unmentioned. Nit 7 (retiring the
stale harnesses) was actioned. Either action them or say which were declined and why.

### 7. Every falsification harness this plan's evidence rests on is gitignored

`untracked/.gitignore:1:*`. `falsify-b1.bash`, `falsify-round3-fixes.bash`,
`falsify-round4-fixes.bash`, `falsify-round5-note.bash`, `falsify-coverage-direction.bash`,
`falsify-drift-summary.bash`, `falsify-triage-leg.bash`, `retired/*` — all under
`untracked/scratch/`, a tree shared with a dozen other plans.
`CLAUDE/Plan/CLAUDE.md` ("Plan-Local Scripts & Artifacts — IN STONE") puts "any other
plan-specific test/scratch script, fixtures" IN THE PLAN FOLDER. None of this travels into
`Completed/`; none exists on another clone; and it is the direct cause of round 5's blocking
finding — the round-4 harness that grepped instead of ran was never in a diff for anyone to
read.

(I re-ran `falsify-round5-note.bash`: exit 0, `MUTANT KILLED`, and it genuinely executes the
block. It is a good harness. It extracts check [6] only, which is why it could not see
finding 1.)

### 8. Mount coverage is counted and then not stated

`acceptance.bash:157–194` prints `PASS  $mount_count rclone mount(s) present` and then
exercises exactly one — `head -n1`. On a two-mount host the gate proves the migration for one
mount and says "every assertion passed". The scope limit is deliberate and argued (comment
168–170), but the gate has a COVERAGE discipline for checks and none for mounts. A
`COVERAGE: 1 of $mount_count mounts exercised (first: $rc_mount)` line would close it.

## Nits

9. **`copy_preflight` does not check `python3`**, which `copy_to_mount` needs at
   `ftp-camera:1242` — AFTER the `cp -r` at 1186. The header at 1063 says "Everything
   `--copy` must establish before it moves a single byte, and NOTHING else." Either add the
   check or amend the header.
10. **`START_MARKER` still leaks** (round-5 nit 8). Measured: `/tmp` entry count 364 → 365
    across one `--copy-preflight`. `ftp-camera:43` `mktemp` runs at load; the dispatch at 2297
    clears the EXIT trap. Check [6] invokes the client every run, so one leak per acceptance
    run.
11. **`play-ftp-camera.yml:445–458` omits `--copy-preflight`** (round-5 nit 10) while listing
    `--debug-ftp`.
12. **The `count_disagrees` fallback (`acceptance.bash:552–554`) is unreachable.** With
    `missing`, `undeclared`, `duplicates` and `catalogue_duplicates` all empty, `RAN_CHECKS`
    and `EXPECTED_CHECKS` are repeat-free sets of the same elements, so their sizes must
    agree. My four mutants confirm it: `COUNT MISMATCH` printed only alongside a named cause,
    never alone. Harmless, but the comment at 520–522 ("the only form of this assertion that a
    fifth cause cannot walk past") claims more than the code does.
13. **`pgrep -f 'rclone mount'` is unbracketed** in `rclone-cache-warm:99`,
    `rclone-cache-status:163`, `rclone-tail:113`, while `rclone-rc-auth.bash:179` and
    `acceptance.bash:173` bracket it and the library's comment (174–177) calls bracketing
    "the repo's rule". I did not construct a live false positive; `pgrep` excludes its own pid.
14. **`triage.bash:104` names `plan_gather_legs`** in a comment; the function is
    `plan_gather_leg` (`_planlib.inc.bash:723`).
15. **`triage.bash:96` sources the REPO library** for address discovery while the plan's whole
    subject is repo-vs-host divergence. Defensible for a fact-gatherer, but unstated on the
    line.

## Round-5 findings — each verified fixed, by execution

| # | Fix | How I established it |
| --- | --- | --- |
| 1 | `note()` defined at `acceptance.bash:120` | Drove all four arms of check [6] against stubs: `NOTE`+`PASS` on the differing branch, `FAIL` on preflight-exit-1, `FAIL` on empty stdout, `PASS` on agreement |
| 2 | `PREFLIGHT_ONLY` in `_skip_inhibit` (`ftp-camera:493–495`) | `--copy-preflight` reaches `copy_preflight` and fails on `/etc/ftp-camera/config`, so it did not re-exec under `systemd-inhibit` |
| 3 | `DEST_PATH=$(find_mount_path) \|\| return 1` (`:1110`) | Read; the only remaining unguarded command in `copy_preflight` is the `. "$CONFIG_FILE"` source |
| 4 | `UPLOAD_DIR` check moved into `copy_preflight` (`:1099–1102`), removed from `copy_to_mount` | Diff + read |
| 5 | `PREFLIGHT_ONLY` in all four mode loops and the "Pick one of:" list | Ran 8 flag combinations with a stubbed `getent`: `--copy --copy-preflight`, `--copy-preflight --async`, `--async-copy`, `--prune` → exit 2 "only one primary mode"; `--view`/`--hotspot`/`--debug-ftp` → exit 2 with their own messages |
| 6 | Verdict cascade reordered, fallback last | Four mutants of a scratch copy plus a control, with check [7] neutralised so `FAIL=0`: control `ACCEPTED`; catalogue-dup → "1 check id(s) are declared more than once"; ran-twice → "ran more than once"; renamed id → "ran that this gate does not declare"; deleted check → "1 declared check(s) never ran" |

## Checked and clean

- **Called-but-never-defined commands.** Extracted every command-position word from
  `acceptance.bash`, `triage.bash`, `deploy.bash` and `rclone-rc-auth.bash` and resolved each
  against the file's own definitions and `_planlib.inc.bash`. All ten `plan_*` symbols used
  are defined; `check`/`ok`/`bad`/`note` are defined; `rclone_rc*` come from the sourced
  library. No second `note`.
- **Argument handling.** `deploy.bash --help` rc=0; `--check` accepted then correctly refused
  by `plan_require_host` in the container (no Ansible ran); `--bogus` rc=64 with usage.
  `acceptance.bash --help` rc=0, `--bogus` rc=1. `triage.bash --help` rc=0.
- **`--copy-preflight` is read-only.** `validate_library_remote` (685–698) is a string `case`;
  `find_mount_path` (1012–1057) is `findmnt` only; `rclone_rc_addr_for_mount` reads `/proc`;
  `rclone_rc_available` does one `core/stats`. Nothing writes, copies or restarts.
  `copy_to_mount` calls the same function with stdout discarded, in the current shell, so
  `DEST_PATH`/`RC_ADDR` propagate.
- **`findmnt -n -t fuse.rclone` exits 0 on zero rows** — checked, because line 157's comment
  depends on it and a non-zero exit there would be a second instance of finding 1.
  `n=0 rc=0`.
- **Journal discipline.** Pure append: `git show cca0d9e7 --numstat` → `61  0` on the journal.
  Times monotonic across all eleven entries (13:40 → 19:09), with the 14:24 entry disclosing
  an earlier inversion as a new entry.
- **Public-repo safety.** Grepped the whole plan folder, all five deployed clients and
  `qa-deployed-drift.bash` for `/home/<user>`, real names, emails and private IPs — no
  matches.
- **Fail-fast annotations.** No `failed_when`/`ignore_errors` (no YAML in the diff). The
  `|| true` occurrences in `rclone-cache-warm`/`-status`/`-tail` are all probe-then-check on
  `pgrep`/`findmnt` with an explicit emptiness test after, which CLAUDE.md permits.
  `ftp-camera`'s `2>/dev/null` uses carry `# FAIL-FAST-OK:` where they suppress a decision.
- **IaC placement.** No playbook changed, so no play-ownership or ordering question arises.
  `--copy-preflight` correctly lives in the client it vouches for rather than in a new script.
  The plan index row at `CLAUDE/Plan/README.md:155` matches PLAN.md's state.

## Is Task 5.9 the only thing left?

No. Finding 1 is in `acceptance.bash` itself, so Task 5.9's post-deploy run cannot render a
verdict on any host whose first `fuse.rclone` mount lacks `--rc-addr`; and findings 1 (the
`rclone-cache-warm` half) and 9 change deployed files, so deploying now re-stales the host and
re-opens check [7] — the exact failure Task 5.9 exists to correct. Sequence: fix 1–8, commit
with the plan update, then `deploy.bash`, then `acceptance.bash`, then success criteria 1, 3
and 5.

## Mechanical gates

- `scripts/qa-all.bash`: **PASS**, exit 0, `✓ QA passed: 947 files checked` (re-run after
  removing the scratch harness). Advisories only: shellcheck 172, semgrep partial parse 15,
  deployed-drift skipped in the container. It cannot see any finding above.
- `hooks-daemon plan-qa --sweep`: exit 1, `Plan QA: 7 findings (0 block, 7 advise)`.
  **None concerns Plan 00099** (grepped).
- `ansible-playbook --syntax-check`: **not triggered** — `cca0d9e7` changes zero `.yml` files
  (`git show --name-only | grep -c '\.yml$'` → 0).
- `scripts/qa-helper-tests.bash`: **not triggered** — no `helpers/` or `tests/helpers/` change.
- `check_extension_compat` / extension ESLint: **not triggered** — no `extensions/` change.
- `bash -n`: OK on all five changed/owned shell files. `shellcheck -x` on `acceptance.bash`,
  `ftp-camera`, `rclone-rc-auth.bash`: exit 0.
- `acceptance.bash` unstubbed: **cannot run here** — this is a CCY container with no
  `fuse.rclone` mount; it ABORTs at check [0]. No Ansible was run at any point.
- `falsify-round5-note.bash`: exit 0, `MUTANT KILLED`.

Unrelated to this plan, visible in `git status` during the review: another session has
`CLAUDE/Plan/032-compression-helpers/` with a modified `PLAN.md` and two untracked scripts.
