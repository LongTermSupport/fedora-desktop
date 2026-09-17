# QA Review — Plan 00099, closing round 2

Scope: `17819cda`, plus `65a557fe` and `c3554f2e` which landed on F44 during the review.

**Verdict: BLOCK.** Plan 00099 must not be marked Complete. Two blocking defects, both
introduced or left by this commit, both reproduced. Separately, Task 5.9 is unticked, so the
plan has open work regardless.

## Blocking

### B1 — `ftp-camera --copy` is broken by this commit: the new discovery is given a path the match can never find

`files/home/.local/bin/ftp-camera:1084,1100` and `files/home/.local/bin/rclone-rc-auth.bash:157`

`copy_to_mount` passes `dest_path` — the output of `find_mount_path` — to
`rclone_rc_addr_for_mount`. `find_mount_path` returns the mount target **plus the remote's
path offset**; its own header comment says so (`ftp-camera:1002-1004`).
`rclone_rc_addr_for_mount` matches `*" $mountpoint "*` against the rclone process's cmdline,
where the mountpoint is the **last argument** (`play-rclone.yml:388`) and carries no offset.
The two can only agree when the configured remote has no path — which is not the configured
case and defeats the point of the tool.

Reproduced, running the repo's real `find_mount_path` against a stubbed `findmnt` shaped like
`rclone_mounts[0]`, and the real library against a process carrying the unit's exact argv:

```
dest_path that copy_to_mount passes:                 <mnt>/PHOTO/LIBRARY
A: called with the MOUNTPOINT (triage.bash)       -> localhost:5572
B: called with find_mount_path output (ftp-camera) -> FAILED rc=1
   rclone_rc_addr_for_mount: no rclone mount process serves <mnt>/PHOTO/LIBRARY
```

So `--copy` now aborts at `ERROR: could not determine the RC address for …` on every run.
The bug it replaced was **latent**: the camera's remote is mount index 0, so
`rc_port_base + 0` = 5572 and the hardcoded address was correct on this host. This one is live.
S3's premise ("would have bitten a user") is right about the direction and wrong about which
build bites.

Worse, the gate still cannot see it. Check [6] (`acceptance.bash:298-319`) probes **its own**
`RC_ADDR`, discovered in check [0] from the mount **target**, and only greps `ftp-camera` for
the string `rclone_rc_available`. The new comment at `acceptance.bash:291-297` — "ftp-camera
now discovers the address from the mount's own process too … so 'same function, same library'
is once again 'same call'" — is false in a new way: the two now discover from **different
inputs**, and the client's input resolves to nothing. This is the S3 class recurring a third
time, inside the fix for S3.

**Fix**: normalise inside the library so every caller gets it right in one place — resolve the
argument to its mount root (`findmnt -n -o TARGET --target "$mountpoint"`) before the cmdline
walk. And make check [6] call `rclone_rc_addr_for_mount "$(find_mount_path)"` so the gate
exercises the client's own resolution rather than a parallel one.

### B2 — `triage.bash` dies on its own last line, every run

`CLAUDE/Plan/00099-rclone-rc-auth-broke-unmigrated-clients/triage.bash:278`

```
./CLAUDE/Plan/00099-…/triage.bash: line 278: LOG: unbound variable
EXIT=1
```

`LOG=` was defined at the pre-conversion `triage.bash:49` and removed by the `_planlib`
conversion; its single consumer at `:278` was not. Under `set -euo pipefail` the run ends with
an unbound-variable error and the line that tells the operator where the report is never
prints. `./scripts/qa-all.bash` is green (943 files) and `shellcheck -x triage.bash` is CLEAN
— neither can see it.

**Fix**: replace `echo "Full report: $LOG"` with `plan_finish`, which prints `PLAN_RUN_LOG` and
the failed-leg summary and is the contract this script is otherwise missing (see S-c).

## Should fix

### S-a — `triage.bash` has no `plan_require_host` and no `# STANDARD-EXCEPTION(R2):`

It probes `findmnt`, `systemctl --user`, `~/.local/bin`, `/etc/ftp-camera` — pure host state,
`PlanScriptStandards.md` R2's exact subject, and the R2 skeleton includes the guard.
`deploy.bash` got it in the same commit; `triage.bash` did not. Ran here to demonstrate the
harm — it completed and reported, as facts:

```
### RC credential file shape        NOT READABLE (or absent)
### core/stats WITHOUT credentials  rclone: command not found
### helper matrix                   rclone-cache-status  ABSENT
                                    ftp-camera           ABSENT   (all five)
```

A complete, confident, entirely wrong picture of the host, with nothing in the output saying it
is not the host. That is R2's original incident verbatim.

### S-b — `triage.bash` never states the RC address it used, and proceeds with an empty one

`:84-93` sets `RC_ADDR=""` on discovery failure and every probe below then calls
`--url="http://"`. The comment claims "every probe below reports rather than papering over",
but no probe reports the **discovery**; they report a connection error, which a reader chasing
F1 ("unauthenticated `core/stats` returns 401") reads as evidence about the RC. Even on success
the address is never printed, so the whole RC section's meaning rests on a value the report
omits. Add an explicit first probe: `RC address: <addr>` / `NOT DISCOVERED — every RC probe
below is void`.

### S-c — `plan_mode gather` is declared and then not used

`triage.bash:72` declares the mode correctly (read-only, must not gate — R8) but the script
uses **no** `plan_gather_leg` and never calls `plan_finish`. R7/R9's contract — "records every
failure by name and drives a non-zero final exit" — therefore never operates: `probe()`
(`:96-103`) returns 0 unconditionally by design, so the exit status carries no information
about whether the fact-finding was complete. Its only non-zero exit today is B2. Either route
the probes through `plan_gather_leg`, or say on the line why `probe()` replaces it and still
call `plan_finish`.

### S-d — `acceptance.bash` check [0] passes on an empty population

`:125-132`. `findmnt -n -t fuse.rclone` exits **0** with zero rows on this system, so the
`if !` ABORT does not fire:

```
[0] precondition: an rclone mount is present and publishes an RC address
  PASS  0 rclone mount(s) present
```

`ok "$(… | wc -l) rclone mount(s) present"` asserts nothing about the count. This is exactly
S2's class — a blind check and a clean check printing alike — in the check whose own comment
(`:119-123`) says it exists to stop checks 4 and 5 passing on an empty population. The run is
saved only by the second ABORT on an empty `RC_ADDR`; and with `rc_mount` empty the matcher at
`:143` degenerates to `*"  "*`, which matches any cmdline containing two consecutive spaces.
Assert the count `-gt 0`.

## Minor — the previous round's m-a…m-g, none resolved and none recorded

The journal's 17:25 entry covers S1–S5 and says nothing about the minors, so these are open
*and* undocumented rather than consciously deferred.

- **m-a** — "this plan's four files" still at `PLAN.md:170` and `:213` (five were migrated,
  the library included).
- **m-b** — `scripts/qa-deployed-drift.bash:264`: the failing summary still prints
  `$DRIFTED of $CHECKED` and omits `NOT_DEPLOYED`, which the passing path states always, on
  purpose.
- **m-c** — `triage.bash:146` still `length($2)`, truncating the reported credential length at
  the second `=`. Its sibling 26 lines down (`:172-173`) was fixed with a comment explaining
  why. Same file, same parser, same argument.
- **m-d** — `acceptance.bash:26-35` is still a third hand-maintained check list (currently
  consistent with `EXPECTED_CHECKS:74`, tied to it by nothing).
- **m-e** — COVERAGE is still one-directional: a check run without being in `EXPECTED_CHECKS`
  gives `10 of 9` and still ACCEPTED (`:389-398`).
- **m-f** — `acceptance.bash:141` is still an unguarded
  `tr '\0' ' ' < "/proc/$rc_pid/cmdline"`. **The new library function added exactly that
  guard** (`rclone-rc-auth.bash:150-155`), with a comment explaining why. The lesson was
  applied at the site being written and not to the identical loop the same commit was editing.
- **m-g** — checks 4 and 5 (`:255`, `:276`) still grep only
  `rc unreachable\|rejected credentials\|credential missing`; the clients'
  `rc helper library missing` arm (`rclone-tail:155,166`, `rclone-cache-status:206`) is still
  absent, so that failure degrades to the weaker "printed no cache figures" message.

## Nits

- `rclone-rc-auth.bash:144` uses `pgrep -f 'rclone mount'` while `acceptance.bash:140`, in the
  same change, uses the bracketed `'rclone [m]ount'`. The bracketed form is the repo's rule
  (R-PGREP-SELF-MATCH) and it blocked one of my own probes during this review.
- `rclone-rc-auth.bash:158` omits the `| head -n1` that all three sibling implementations
  carry.
- There are now **five** copies of the cmdline walk: the library, `acceptance.bash:140-148`,
  `rclone-cache-status:163`, `rclone-tail:113`, `rclone-cache-warm:99`. The library's comment
  (`:128-130`) names only two of them as knowing duplication.
- `PLAN.md:170` — stray space still present: `files/home/.local/lib/ freeze/freeze-common.bash`.
- `PLAN.md:169-174` — Task 5.7's nested unticked box, which is never to be ticked, is still a
  task rather than a **Known, Out of Scope** entry.
- `PLAN.md:157` — Task 5.9 is listed **above** Task 5.7 (the sequence reads 5.8, 5.9, 5.7).
- `CLAUDE/Plan/00099-…/logs/rclone-rc-clients-triage.log` (0 bytes) is still on disk.
  `.gitignore:78` covers it so it will not commit, but `git mv` into `Completed/` moves only
  tracked files, leaving an orphan directory at the old path.
- Success criteria 1 and 3 (`PLAN.md:207`, `:211`) remain ticked while Task 5.9 states the host
  no longer runs these builds. Criterion 1 — "ftp-camera's copy preflight authenticates" — is
  additionally false of the **repo** code now, per B1.
- The index row (`CLAUDE/Plan/README.md:151`) now ends "Deployed; see PLAN.md…", which S4
  fixed, but "Deployed" is stale again for the same reason Task 5.9 gives.

## Checked and clean

- **S1, verified against the real gate.** Driving `acceptance.bash`'s check [7] block over real
  `qa-deployed-drift.bash` output: `FAIL the drift gate SKIPPED and compared nothing — this is
  not a pass`.
- **S1's converse is safe.** Every output path of the drift gate read: `skipped` appears only in
  the three exit-0 skip messages (`:76`, `:92`, `:99`). The passing path (`:277-287`) prints
  "match the repo / not installed on this host / template(s) not byte-comparable" and the
  per-template lines — no occurrence. The `exit 2` paths print a cross and exit non-zero, so
  they take check [7]'s `else` arm. A genuine PASS cannot be wrongly failed.
- **S2, verified.** Empty population gives `FAIL no deployed client was scanned`. Real
  population gives `PASS … (4 scanned)` from 5 glob matches: `scanned=$((scanned+1))` sits
  **after** both the `-f` and the `RC_LIB` continues, so the count cannot be inflated by the
  skipped library.
- **S3's matching, verified.** Prefix: asking for `<b>/a` while only `<b>/ab` is mounted
  correctly does not match (the trailing space in `*" $mountpoint "*` is load-bearing). A
  mountpoint containing a space matches correctly, because `tr` flattens the NULs. `pgrep`
  no-match gives a clean `return 1`, caller alive. A mount with no `--rc-addr` gives the
  intended diagnostic, under both `set -e` and `set -euo pipefail` (the substitution sits in an
  `if` condition, so errexit is suspended). No caller receives an empty address and proceeds:
  `ftp-camera:1100` gates on the exit status, `triage.bash:90` on the same.
- **`rclone-cache-status` / `rclone-tail` / `rclone-cache-warm` are untouched** and keep their
  own `findmnt`-target-driven walks, which match the mount root — they still work.
- **`deploy.bash` against R1–R14: clean.** R1 bootstrap verbatim; R2 `plan_require_host`
  (exercised — refuses in the container, naming `/run/.containerenv`); R3 `plan_prime_sudo`
  before `plan_start_log`; R4 no hand trap; R5/R8 `plan_gate_change` before the first mutating
  leg; R6 `plan_ansible_playbook` (the cd that S5 was about); R7 `plan_mode deploy` with both
  `plan_deploy_leg`s bare top-level; R12 exec bit set; `plan_finish` present. `--help` works and
  exits 0 before the host guard.
- **The `acceptance.bash` EXIT trap conflicts with nothing.** It is the only `trap` in the file,
  armed at `:112` before any `mktemp`; both `mktemp`s register on the immediately following line
  (`:191`, `:311`); the inline `rm -f`s at `:197`/`:318` make the trap's second removal a no-op.
  `TEMP_FILES=()` sits after the `--help` exit, which creates no temp files.
- **S4 fixed** — the index row no longer claims "ACCEPTED 10/10 on the host".
- **Public-repo safety: clean.** No added line across `17819cda`, `65a557fe`, `c3554f2e`
  contains a checkout path, an absolute home path, a username, an email or an IP. The host
  values worked from are in an **untracked** file — confirmed with
  `git ls-files --error-unmatch`. The one remote name flagged as a nit last round remains a
  pre-existing repo-wide string (8 tracked files) that this commit discusses rather than
  introduces; still not in `CLAUDE/ExampleValues.md`, still an owner decision, still not
  00099's to close.
- **Version bumps: none owed.** Nothing under `files/var/local/claude-yolo/`, `lib/*.bash` or
  the Dockerfile is touched.
- **Plan Commit Rule: satisfied.** Task 5.9 was dangling unstaged at the start of this review;
  it landed in `65a557fe` during it, and `git status` is now clean.

## Mechanical gates

| Gate | Result |
| --- | --- |
| `./scripts/qa-all.bash` | **PASS**, exit 0 — 943 files. The deployed-drift, shellcheck and patterns warnings are the known advisories. Green over both blocking defects. |
| `hooks-daemon plan-qa --sweep` | exit 1, **7 findings, 0 blocking, none concerning Plan 00099** (path-existence in 00046; journal ordering in 00063/00109/00119) |
| `shellcheck -x` | CLEAN on `deploy.bash`, `triage.bash`, `acceptance.bash`, `rclone-rc-auth.bash`, `ftp-camera` — including `triage.bash`, which errors at runtime |
| `ansible-playbook --syntax-check` | **not triggered** — no playbook in the diff. `qa-ansible-syntax` inside `qa-all.bash` covered 82 playbooks green. |
| `acceptance.bash`, live | ABORTs at check [0] in the container (no mount), exit 1 — the path that exposed S-d |
| `triage.bash`, live | ran to completion, exit 1 on B2 |

**Conditional gates**, per `CLAUDE/QA.md` — checked whether the diff triggers them:
`qa-helper-tests.bash` **not triggered** (no `helpers/` or `tests/helpers/` change; ran inside
`qa-all.bash` anyway — 1562 tests, 66 modules); `helpers.gnome.check_extension_compat`
**not triggered** (no `metadata.json`; ran inside `qa-all.bash` — 5 extensions OK); `eslint` in
`extensions/` **not triggered** (no extension JS; 12 files OK). No required gate skipped.

## What could not be verified from the container

Any live RC call, and `deploy.bash` past its host guard. B1 is proven against a simulated
process carrying the unit template's exact argv and the repo's real `find_mount_path`, not
against a live mount.

Scratch left behind, both gitignored: `untracked/scratch/qa99/` (captures) and one
`untracked/plan-runs/00099-…/triage/` run directory from executing `triage.bash`. No tracked
file was changed by this review.

## Bottom line

The last unticked success criterion is not met. B1 and B2 need fixing and re-verifying, then
Task 5.9's host re-deploy and a fresh `acceptance.bash` run — and that run is what will confirm
the B1 fix, since check [6] does not currently exercise the client's own address resolution.
