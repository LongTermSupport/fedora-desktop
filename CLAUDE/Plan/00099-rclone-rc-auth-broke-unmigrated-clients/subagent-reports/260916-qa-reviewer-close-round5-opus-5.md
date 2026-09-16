# QA Review — Plan 00099 round 5, commit `113fe2e1`

**Verdict**: FIX-BEFORE-MERGE

Round 5 is the first round where check [6] is not defeated by a normalisation — running the
client instead of approximating it is the right structural answer, and I could not construct
an input-shape mutant against it. But the round-4 fix introduced a **new** defect of the same
family it was written to close: check [6] gained a branch that has never been executed by
anything, and that branch calls a function that does not exist.

## Blocking

### 1. `note` is called and never defined — the gate dies with exit 127 and no verdict

`CLAUDE/Plan/00099-rclone-rc-auth-broke-unmigrated-clients/acceptance.bash:374`

```bash
note "ftp-camera resolved $client_addr; check [0] probes $RC_ADDR (different mounts, or one of them is wrong)"
```

`acceptance.bash` defines `check`, `ok` and `bad` (lines 99–113) and sources only
`rclone-rc-auth.bash`, which defines `rclone_rc_load_credentials`, `rclone_rc`,
`rclone_rc_available`, `rclone_rc_addr_for_mount`. There is no `note` anywhere in the plan
folder or the library:

```
$ grep -rn 'note()' CLAUDE/Plan/00099-rclone-rc-auth-broke-unmigrated-clients/
NONE ANYWHERE IN PLAN DIR
```

The gate runs under `set -euo pipefail` (line 16), so `note: command not found` returns 127
and errexit kills the script:

```
$ bash -c 'set -euo pipefail; ...; note "..."; ok "..."; echo "REACHED THE COVERAGE BLOCK"'
bash: line 5: note: command not found
EXIT=127
```

Consequences, in order of how bad they are:

- The run ends **mid-check [6]**. No `COVERAGE` line, no `ACCEPTED`/`REJECTED`, no `NOT RUN`
  list. The entire coverage apparatus this plan spent four rounds hardening never executes.
  An operator sees a handful of `PASS` lines and a `command not found`.
- The branch is reachable **by design on the host this plan targets**. `acceptance.bash:82–88`
  states check [0] deliberately takes the *first* `fuse.rclone` mount and that multi-mount
  hosts are the normal case (`rclone_rc_port_base + mount_index`); lines 371–373 say so in the
  branch's own comment. This is not a corner nobody will hit — it is the exact scenario the
  branch was added for.
- It is the plan's own named defect class. The branch was written in round 4 precisely so the
  gate could distinguish "different mount" from "wrong address". It cannot distinguish
  anything; it crashes.
- Nothing caught it. `qa-all.bash` is green (943 files), shellcheck is clean on the file
  (exit 0) — shellcheck treats an undefined function as an external command and cannot know
  better. **No harness executes check [6].** `falsify-round4-fixes.bash` asserts on check [6]
  with five `grep`s of the gate's *text* (lines 109–123) and never runs it; when it does
  invoke the gate (finding 3, lines 166/174), `$BIN/ftp-camera` is absent in the container so
  check [6] takes the `bad "ftp-camera is not deployed"` arm at line 333 and the new `else`
  block is never entered. The round-4 fix was verified by grep and shipped unrun — which is
  the thing this plan's last four rounds were about.

Fix: define `note()` beside `ok`/`bad` (stderr, no `PASS`/`FAIL` counter change), and add an
execution test that reaches line 374 rather than another text assertion.

## Should fix

### 2. `--copy-preflight` re-execs under `systemd-inhibit`

`files/home/.local/bin/ftp-camera:493`

```bash
if [ "$PASS_ONLY" = true ] || [ "$SORT_ONLY" = true ] || [ "$PRUNE_ONLY" = true ]; then
    _skip_inhibit=true
fi
```

`PREFLIGHT_ONLY` is absent, so `--copy-preflight` falls through to the
`exec systemd-inhibit --what=idle:sleep --mode=block -- "$0" "${ORIG_ARGS[@]}"` at line 503.
That contradicts the help text at line 110 (*"Read-only: copies nothing, starts nothing"*),
the block comment at 2284–2285 (*"it starts nothing … and changes nothing"*), and the block's
own criterion at 489–490 (*"Cheap/interactive modes (anything that exits in seconds) skip the
wrap"*). A preflight is the definition of a mode that exits in seconds.

The practical cost is a misdiagnosis in check [6]. Where logind's bus is unreachable — a plan
gate run over SSH without a session bus, from a service, from cron:

```
$ systemd-inhibit --what=idle:sleep --who=test --why=test --mode=block -- /bin/echo HELLO
Failed to connect to bus: No such file or directory
EXIT=1
```

`exec` replaces the shell, so `--copy-preflight` exits 1 with empty stdout and that text on
stderr, and check [6] prints `FAIL ftp-camera's own copy preflight failed — --copy aborts
here / Failed to connect to bus`. The preflight never ran. That is exactly the "cannot tell
the check failed from the check did not run" case that the new `--copy-preflight`-absent arm
at line 337 was added to close, reintroduced one layer up. Add `PREFLIGHT_ONLY` to
`_skip_inhibit`.

### 3. The extraction changed `copy_preflight`'s failure semantics between its two callers

`files/home/.local/bin/ftp-camera:1098` and `:1146`

The commit message asserts "`copy_to_mount` calls the identical function so the two cannot
drift". The function is identical; its behaviour is not, because `copy_to_mount` calls it
inside a `||` list, which suspends `set -e` (line 2 of the script) for the whole call, while
the `--copy-preflight` dispatch calls it as a simple command with errexit live. The only
unguarded command in `copy_preflight` is line 1098:

```bash
DEST_PATH=$(find_mount_path)
```

Demonstrated:

```
--- via copy_to_mount (|| context) ---
ERROR: No rclone mount found for remote
REACHED the -d test with DEST_PATH=[]
ERROR: Mount destination not found:
copy_to_mount returned 1
--- via direct call (--copy-preflight path) ---
ERROR: No rclone mount found for remote
OUTER EXIT=1
```

So `--copy-preflight` aborts on `find_mount_path`'s own message, while `--copy` swallows the
failure signal, continues, and emits a second misleading error with an empty path after the
colon. Before the extraction, `copy_to_mount` was called with errexit live and this could not
happen. Both paths still fail, so this is not a false green — but it is a discarded failure
signal introduced by a refactor claimed to be behaviour-preserving, and it falsifies the
"cannot drift" invariant the round-5 design rests on. Fix:
`DEST_PATH=$(find_mount_path) || return 1`.

### 4. `copy_preflight` is not "everything `--copy` does before it moves a byte"

`files/home/.local/bin/ftp-camera:1062` vs `:1151`

The header says *"Everything `--copy` must establish before it moves a single byte, and
NOTHING else."* The `UPLOAD_DIR` existence check was moved **out** of the preflight and left
in `copy_to_mount` at line 1151, before the `cp -r` at 1179. It is a pre-copy check, and it is
not in the preflight. So `--copy-preflight` can succeed, check [6] can pass, and
`ftp-camera --copy` can still abort on every run with `ERROR: Upload directory not found` — a
smaller version of the exact gap rounds 2–4 were spent closing. Either move the check into
`copy_preflight`, or amend the header and check [6]'s comment (`acceptance.bash:352–354`) to
name the one check the preflight deliberately excludes and why. Do not leave the stronger
claim standing.

The reordering also changed `--copy`'s error precedence: an absent upload directory used to be
reported before anything touched the mount; it is now reported after the mount and RC
diagnostics. Harmless, but undocumented in a commit that presents the extraction as a no-op.

### 5. `PREFLIGHT_ONLY` is exempt from every mode-conflict check

`files/home/.local/bin/ftp-camera:416`, `:430`, `:444`, `:457`

`--copy-preflight` is documented at line 110 under *"Primary modes (pick at most one …)"*, but
it is in none of the four validation loops:

```bash
for _mode in SORT_ONLY PASS_ONLY PUSH_ONLY COPY_ONLY PRUNE_ONLY ASYNC_MODE; do   # :416
```

`ftp-camera --copy --copy-preflight` therefore counts one mode, passes validation, and the
dispatch at 2289 runs *before* the `--copy` dispatch at 2296 — the user asked for a copy, got
a printed address, and the script exits 0. Same silent override for
`--copy-preflight --async`, and `--view` / `--hotspot` / `--debug-ftp` are accepted alongside
it and ignored. Add `PREFLIGHT_ONLY` to all four loops (and to the "Pick one of:" list at
line 423).

### 6. The new count-mismatch verdict is first in the cascade, not last

`CLAUDE/Plan/00099-rclone-rc-auth-broke-unmigrated-clients/acceptance.bash:528`

The comment at 511–513 correctly describes the direct count comparison as the *unnamed
fallback*: *"the four named conditions above each describe a KNOWN way they can disagree; this
one holds whether or not the cause has a name yet"*. It is then checked **first** in the
verdict cascade. Simulating the cascade for each canonical single-cause mutant:

```
canonical duplicate-run mutant (check 7 emitted twice: ran=10 exp=9, dup=1):  -> REJECTED: counts disagree
canonical undeclared mutant (new check ran, not declared: ran=10 exp=9):      -> REJECTED: counts disagree
canonical missing mutant (a check deleted: ran=8 exp=9):                      -> REJECTED: counts disagree
canonical catalogue-dup mutant (ran=9 exp=10):                                -> REJECTED: counts disagree
```

Lines 530, 532 and 535 — including the duplicate-REJECTED wording round 4 explicitly confirmed
as exercisable — are now reachable only when the two counts coincide by accident (e.g. one
check skipped *and* another duplicated). The detail lines above (`RAN MORE THAN ONCE`,
`DECLARED MORE THAN ONCE`, `NOT RUN`) still print, so no information is lost from the full
output, but the headline the operator reads has regressed from specific to generic in every
case those lines were written for. Move the count comparison to the **end** of the cascade,
where its own comment says it belongs.

Note this is also why `falsify-round4-fixes.bash` stayed green: it asserts on
`DECLARED MORE THAN ONCE: 7` (the detail line), not the verdict.

## Nits

7. **Two harnesses now vouch for a gate input that no longer exists.**
   `untracked/scratch/falsify-check6-input2.bash` still prints
   `FIXED — the new input kills all three` about `find -mindepth 1 -maxdepth 1` on the mount
   root, which round 5 deleted; `falsify-check6-input.bash` does the same for round 2's
   `$rc_mount/.`. Both re-run green and both now establish nothing about the shipped gate.
   Retire them, or re-head them as historical. `falsify-b1.bash` and
   `falsify-round3-fixes.bash` both still exercise live library code (re-run green:
   `MUTANT KILLED` on both counts) and should stay.

8. **`--copy-preflight` leaks a temp file.** `START_MARKER=$(mktemp)` at line 43 runs
   unconditionally at load; the preflight block clears the EXIT trap at 2290 so
   `rm -f "$START_MARKER"` (line 2156) never runs. Pre-existing and shared with
   `--sort`/`--push`/`--copy`/`--prune`, but check [6] now invokes `ftp-camera` on every
   acceptance run, so it is newly on the gate's path.

9. **Delivery section names a hash it can, and a phrase where it could name another.**
   `PLAN.md:278–283` lists `17819cda`, `510dbbf4` and "this round's follow-up" — but
   `68c8bbfa` also changed a deployed file (`rclone-rc-auth.bash`) and is nameable. Task 5.9's
   scope rests on this list.

10. **`play-ftp-camera.yml:445–470`'s command list omits `--copy-preflight`** while listing
    the equally diagnosis-only `--debug-ftp`. Low confidence this matters — it is a
    gate-facing flag — but the omission is asymmetric.

## Checked and clean

- **Check [6] is genuinely no longer a stand-in.** `acceptance.bash:364` invokes
  `"$BIN/ftp-camera" --copy-preflight`; `falsify-round4-fixes.bash` confirms no stand-in
  resolution remains in the gate and that `copy_to_mount` calls the same function. I could not
  construct a `find_mount_path` normalisation mutant that the gate now passes — there is no
  proxy left to satisfy. The structural answer is right; findings 1–4 are about its
  implementation, not its shape.
- **The `--copy-preflight`-absent arm is correct and necessary** (`acceptance.bash:337`). An
  unknown flag exits 2 at lines 404–406, which is genuinely indistinguishable from a failed
  preflight; asserting the flag by name before invoking it is the right order. The redundant
  `[ -z "$client_addr" ]` arm at 367 is belt-and-braces, not a defect.
- **The preflight is read-only with respect to data.** `validate_library_remote` (684–697),
  `find_mount_path`, `rclone_rc_addr_for_mount` and `rclone_rc_available` (107–115) are all
  reads; the RC probe is `core/stats`. Nothing copies, writes or restarts. All four are also
  stdout-clean, so `client_addr` captures only the address — I checked this specifically,
  because stdout noise would make the `!= RC_ADDR` branch fire unconditionally and turn
  finding 1 from conditional into certain.
- **`DEST_PATH` and `RC_ADDR` collide with nothing.** Grepped both names across `ftp-camera`
  (2,540 lines) and `rclone-rc-auth.bash`: the only occurrences are lines 1073–1149 of the new
  code. The library defines four `rclone_rc_*` functions and no globals by those names.
  `copy_preflight` is called in the current shell from both callers, so the globals propagate
  as intended.
- **Trap handling is right.** `trap - EXIT INT TERM` at 2290 matches every other short-lived
  mode; the preflight starts no background process, so `cleanup` has nothing to do.
- **Item 2 of the task — the `--rc-addr` runtime message.** `rclone-rc-auth.bash:212–214` no
  longer asserts rclone's semantics; it separates the verifiable half (sibling-helper
  convention) from the unestablished half and states that no address is returned. No stale
  copy of the old wording survives outside the journal and the review reports (grepped
  repo-wide).
- **Item 4 — the 00109 bundle.** `113fe2e1` touches six files, all under the 00099 plan folder
  or `files/home/.local/bin/`. Plan 00109's work is in `6922ef6a` with no 00099 paths. The
  originally bundled artefact is already in `68c8bbfa`'s history and is now referenced from
  00109's journal, which is the only remedy short of a rewrite.
- **Item 5 — PLAN.md.** Task 5.10 now covers rounds 2, 3 and 4 in one entry; Task 5.9 remains
  correctly unticked, as do success criteria 1, 3 and 5.
- **The two corrections the round-4 report earned are both recorded and both verifiable.**
  Journal 18:44, lines 539–542. `c3554f2e` touches exactly one file:
  `CLAUDE/Plan/Completed/README.md` — no deployed file, so dropping it from the Delivery list
  was right.
- **All five round-4 should-fix items are actioned** (report headings at lines 21, 74, 90,
  125, 140 of the round-4 report).
- **Placement, naming, IaC graph, secrets.** No playbook changed, so no play-ownership or
  ordering question arises; `--copy-preflight` correctly lives in the client it vouches for
  rather than in a new script. No jargon naming. No real usernames, paths, hostnames or IPs in
  the diff. No secrets, no `failed_when`/`ignore_errors`, no `creates:`-on-empty-file guard.
- **Docs.** `docs/playbooks.md:877–882` describes `play-ftp-camera.yml` without enumerating
  flags, so no docs drift from the new flag.

## Mechanical gates

- `scripts/qa-all.bash`: **PASS** — `QA passed: 943 files checked`, exit 0. It does not and
  cannot see finding 1.
- `hooks-daemon plan-qa --sweep`: **7 advisories, 0 block**, exit 1. None concern Plan 00099
  (journal ordering in 00063/00109/00119, a path reference in 00046, and journal freshness on
  eleven unrelated plans).
- `ansible-playbook --syntax-check`: **not triggered** — the commit changes no `.yml`.
- `scripts/qa-helper-tests.bash`: **not triggered** — no `helpers/` or `tests/helpers/` change.
- `check_extension_compat` / extension ESLint: **not triggered** — no `extensions/` change.
- `acceptance.bash`: **cannot run here** — ABORTs at check [0], no `fuse.rclone` mount in the
  container, as stated.
- Harnesses re-run: `falsify-round4-fixes.bash` **exit 0**, `falsify-round3-fixes.bash`
  **exit 0**, `falsify-check6-input2.bash` **exit 0** (vouching for a retired input — see
  nit 7).

## What stands between this plan and Complete

**No — Task 5.9's host re-deploy is not the only thing left.** Finding 1 must be fixed and
committed *before* the deploy, not after, for two reasons: the `note` crash is in
`acceptance.bash` itself, so Task 5.9's post-deploy gate run cannot render a verdict on a
multi-mount host; and findings 2–5 change `files/home/.local/bin/ftp-camera`, a deployed file,
so deploying now would immediately re-stale the host and re-open check [7] — the precise
failure Task 5.9 exists to correct.

Sequence: fix findings 1–5, commit with the plan update, then Task 5.9 (`deploy.bash` then
`acceptance.bash`), then success criteria 1, 3 and 5. Success criterion "`qa-reviewer` returns
PASS" is not met by this round.
